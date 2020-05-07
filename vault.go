package main

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	vault "github.com/hashicorp/vault/api"
	"k8s.io/klog"
)

func generatePolicy(name, mountName string, minioInstances []string) (string, string) {
	policy := `
#
# Policy for Kubeflow profile: {{ .ProfileName }}
# (policy managed by the custom Kubeflow Profiles controller)
#

# Grant full access to the KV created for this profile
path "{{ .MountName }}/*" {
	capabilities = ["create", "update", "delete", "read", "list"]
}

# Grant access to MinIO keys associated with this profile
{{- $profile := .ProfileName }}
{{- range $instance := .MinioInstances }}
path "{{ $instance }}/keys/{{ $profile }}" {
	capabilities = ["read"]
}
{{- end }}
`
	t, _ := template.New("policy").Parse(policy)
	w := bytes.NewBufferString("")

	t.Execute(w, struct {
		MountName      string
		ProfileName    string
		MinioInstances []string
	}{MountName: mountName, ProfileName: name, MinioInstances: minioInstances})

	return fmt.Sprintf("%s", name), w.String()
}

func cleanName(name string) string {
	return strings.ReplaceAll(name, "_", "-")
}

func doPolicy(vc *vault.Client, name, mountName string, minioInstances []string) (string, error) {
	policyName, policy := generatePolicy(name, mountName, minioInstances)

	policies, err := vc.Sys().ListPolicies()
	if err != nil {
		return policyName, err
	}

	if StringArrayContains(policies, policyName) {
		existingPolicy, err := vc.Sys().GetPolicy(policyName)
		if err != nil {
			return policyName, err
		}

		if policy != existingPolicy {
			klog.Infof("update policy for %q", policyName)
			err := vc.Sys().PutPolicy(policyName, policy)
			if err != nil {
				return policyName, err
			}
		} else {
			klog.Info("policy in sync")
		}
	} else {
		klog.Infof("writing policy for %q", policyName)
		err := vc.Sys().PutPolicy(policyName, policy)
		if err != nil {
			return policyName, err
		}
	}

	return policyName, nil
}

func hasMount(mounts map[string]*vault.MountOutput, mountName string) bool {
	for path := range mounts {
		if strings.ReplaceAll(path, "/", "") == mountName {
			return true
		}
	}

	return false
}

func doMount(vc *vault.Client, name string) (string, error) {
	mountName := fmt.Sprintf("kv_%s", name)

	mounts, err := vc.Sys().ListMounts()
	if err != nil {
		return mountName, err
	}

	if !hasMount(mounts, mountName) {
		klog.Infof("creating mount %q", mountName)

		err = vc.Sys().Mount(mountName, &vault.MountInput{
			Type:        "kv",
			Description: fmt.Sprintf("Kubeflow profile key-value store for %q", name),
			Options: map[string]string{
				"version": "2",
			},
		})
		if err != nil {
			return mountName, err
		}
	} else {
		klog.Infof("mount %q already exists", mountName)
	}

	return mountName, nil
}

func doKubernetesBackendRole(vc *vault.Client, authPath, namespace, roleName string, policies []string) error {
	rolePath := fmt.Sprintf("/%s/role/%s", authPath, roleName)

	secret, err := vc.Logical().Read(rolePath)
	if err != nil {
		return err
	}

	if secret == nil {
		klog.Infof("creating backend role in %q for %q", authPath, roleName)

		secret, err = vc.Logical().Write(rolePath, map[string]interface{}{
			"bound_service_account_names":      []string{"*"},
			"bound_service_account_namespaces": []string{namespace},
			"policies":                         policies,
		})

		if err != nil {
			return err
		}
	} else {
		klog.Infof("backend role in %q for %q already exists", authPath, namespace)
	}

	return nil
}

func doMinioRole(vc *vault.Client, authPath, name string) error {
	rolePath := fmt.Sprintf("%s/roles/%s", authPath, name)

	secret, err := vc.Logical().Read(rolePath)
	if err != nil && !strings.Contains(err.Error(), "role not found") {
		return err
	}

	if secret == nil {
		klog.Infof("creating backend role in %q for %q", authPath, name)

		secret, err = vc.Logical().Write(rolePath, map[string]interface{}{
			"policy":           "readonly",
			"user_name_prefix": fmt.Sprintf("%s-", name),
		})

		if err != nil {
			return err
		}
	} else {
		klog.Infof("backend role in %q for %q already exists", authPath, name)
	}

	return nil
}

func contains(s []string, e string) bool {
	for _, a := range s {
		if a == e {
			return true
		}
	}
	return false
}

//
// Creates or updates a Vault entity for the owner of the profile where
// name = <ownerName>
// policies = kubeflow-profile
// metadata = {kubeflow_profile=<profileName>}
// then returns the id of the entity.
//
func doEntity(vc *vault.Client, profileName string, ownerName string) (string, error) {

	entityNamePath := fmt.Sprintf("/identity/entity/name/%s", ownerName)

	secret, err := vc.Logical().Read(entityNamePath)
	if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("No value found at %s", entityNamePath)) {
		return "", err
	}

	if secret == nil {
		secret, err = vc.Logical().Write("identity/entity", map[string]interface{}{
			"name":     ownerName,
			"policies": []string{"kubeflow-profile"},
			"metadata": map[string]string{
				"kubeflow_profile": profileName,
			},
		})

		if err != nil {
			return "", err
		}

		klog.Infof("created entity %s", ownerName)
	} else {
		var payload = map[string]interface{}{}
		var shouldUpdate = false

		// Check to see if the kubeflow_profile metadata is present, add if not
		if secret.Data["metadata"].(map[string]string)["kubeflow_profile"] == "" {
			payload["metadata"] = secret.Data["metadata"]
			payload["metadata"].(map[string]string)["kubeflow_profile"] = profileName
			shouldUpdate = true
		}

		// Check to see if the kubeflow-profile policy is assigned, if not add
		if !contains(secret.Data["policies"].([]string), "kubeflow-profile") {
			payload["policies"] = append(secret.Data["policies"].([]string), "kubeflow-profile")
			shouldUpdate = true
		}

		if shouldUpdate {
			var entityIdPath = fmt.Sprintf("identity/entity/id/%s", secret.Data["id"].(string))
			secret, err = vc.Logical().Write(entityIdPath, payload)

			if err == nil {
				klog.Infof("updated entity %s", ownerName)
			} else {
				return "", err
			}
		} else {
			klog.Infof("entity %s already exists", ownerName)
		}
	}

	return secret.Data["id"].(string), nil
}

// checks to see if the alias is a part of the supplied array
func hasAlias(aliases []map[string]interface{}, mountAccessor string, aliasName string) bool {
	for _, alias := range aliases {
		if alias["name"].(string) == aliasName && alias["mount_accessor"].(string) == mountAccessor {
			return true
		}
	}

	return false
}

//
// Creates an entity-alias to link an entity to its identity in OIDC if it doesn't exist
//
func doEntityAlias(vc *vault.Client, mountAccessor string, entityId string, aliasName string) error {

	entityNamePath := fmt.Sprintf("/identity/entity/id/%s", entityId)

	var secret, err = vc.Logical().Read(entityNamePath)
	if err != nil {
		return err
	}

	if !hasAlias(secret.Data["aliases"].([]map[string]interface{}), mountAccessor, aliasName) {
		secret, err = vc.Logical().Write("/identity/entity-alias", map[string]interface{}{
			"name":           aliasName,
			"mount_accessor": mountAccessor,
			"canonical_id":   entityId,
		})

		if err != nil {
			return err
		}
	}

	return nil
}

//
// Configures Vault to allow for access to Minio instances
// and the modification of the mount by its owner.
//
func doVaultConfiguration(vc *vault.Client,
	profileName string,
	ownerName string,
	minioInstances []string,
	kubernetesAuthPath string,
	oidcAuthAccessor string) error {

	name := cleanName(fmt.Sprintf("profile-%s", profileName))

	//
	// Let's do for a KeyVault mount
	// for the profile.
	//
	var mountName string
	var err error
	if mountName, err = doMount(vc, name); err != nil {
		return err
	}

	klog.Info("done mount")

	//
	// Add the policy to Vault.
	// If the policy already exists,
	// ensure it has the right value.
	//
	var policyName string
	if policyName, err = doPolicy(vc, name, mountName, minioInstances); err != nil {
		return err
	}

	klog.Info("done policy")

	//
	// Add the Kubernetes backend role
	// to permit authentication from the profile's
	// namespace.
	//
	if err := doKubernetesBackendRole(vc, kubernetesAuthPath, profileName, name, []string{"default", policyName}); err != nil {
		return err
	}

	klog.Info("done Kubernetes backend roles")

	//
	// Add MinIO role
	//
	for _, instance := range minioInstances {
		if err := doMinioRole(vc, instance, name); err != nil {
			return err
		}
	}

	klog.Info("done MinIO backend roles")

	//
	// Create the entity associated to the profile
	//
	id, err := doEntity(vc, name, ownerName)

	if err != nil {
		return err
	}

	klog.Infof("done creating entity")

	//
	// Create entity-alias linked to previously created entity
	//
	if err := doEntityAlias(vc, oidcAuthAccessor, id, ownerName); err != nil {
		return err
	}

	klog.Infof("done creating entity-alias")

	return nil
}
