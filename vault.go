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

	var policyPath = fmt.Sprintf("/sys/policies/acl/%s", policyName)
	secret, err := vc.Logical().Read(policyPath)
	if err != nil {
		return policyName, err
	}

	if secret == nil || secret.Data["policy"].(string) != policy {
		secret, err = vc.Logical().Write(policyPath, map[string]interface{}{
			"policy": policy,
		})
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

func doKubernetesBackendRole(vc *vault.Client, authPath, name, roleName string, policies []string) error {
	rolePath := fmt.Sprintf("/%s/role/%s", authPath, roleName)

	secret, err := vc.Logical().Read(rolePath)
	if err != nil {
		return err
	}

	if secret == nil {
		klog.Infof("creating backend role in %q for %q", authPath, roleName)

		secret, err = vc.Logical().Write(rolePath, map[string]interface{}{
			"bound_service_account_names":      []string{"*"},
			"bound_service_account_namespaces": []string{name},
			"policies":                         policies,
		})

		if err != nil {
			return err
		}
	} else {
		klog.Infof("backend role in %q for %q already exists", authPath, name)
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

//
// Creates or updates a Vault entity for the owner of the profile where
// name = <ownerName>
// policies = <policy-name>
//
func doEntity(vc *vault.Client, name string, policyName string) error {

	entityNamePath := fmt.Sprintf("/identity/entity/name/%s", name)

	secret, err := vc.Logical().Read(entityNamePath)
	if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("No value found at %s", entityNamePath)) {
		return err
	}

	if secret == nil {
		secret, err = vc.Logical().Write(entityNamePath, map[string]interface{}{
			"policies": []string{policyName},
		})

		if err != nil {
			return err
		}

		klog.Infof("created entity %s", name)

	} else {
		//Convert to strings for easy matching
		sPolicies := make([]string, 0)
		for _, v := range secret.Data["policies"].([]interface{}) {
			sPolicies = append(sPolicies, fmt.Sprint(v))
		}

		// Check to see if the policy is assigned, if not add
		if !StringArrayContains(sPolicies, policyName) {
			var payload = map[string]interface{}{}
			payload["policies"] = append(secret.Data["policies"].([]interface{}), policyName)
			secret, err = vc.Logical().Write(entityNamePath, payload)

			if err != nil {
				return err
			}
		}
	}
	//klog.Infof("updated entity %s", ownerName)
	return nil
}

// checks to see if the alias is a part of the supplied array
func hasAlias(aliases []interface{}, mountAccessor string, aliasName string, id string) bool {
	if len(aliases) == 0 {
		return false
	}

	for _, alias := range aliases {
		var mapAlias = alias.(map[string]interface{})
		if mapAlias["name"].(string) == aliasName && mapAlias["mount_accessor"].(string) == mountAccessor && mapAlias["canonical_id"].(string) == id {
			return true
		}
	}

	return false
}

//
// Creates an entity-alias to link an entity to its identity in OIDC if it doesn't exist
// owner_name = <oidc - preferred_name>
//
func doEntityAlias(vc *vault.Client, mountAccessor string, ownerName string, name string) error {

	entityNamePath := fmt.Sprintf("/identity/entity/name/%s", name)

	var secret, err = vc.Logical().Read(entityNamePath)
	if err != nil {
		return err
	}

	var aliases = secret.Data["aliases"].([]interface{})
	var canonicalId = secret.Data["id"].(string)
	if !hasAlias(aliases, mountAccessor, ownerName, secret.Data["id"].(string)) {
		secret, err = vc.Logical().Write("/identity/entity-alias", map[string]interface{}{
			"name":           ownerName,
			"mount_accessor": mountAccessor,
			"canonical_id":   canonicalId,
		})

		if err != nil {
			return err
		}

		if secret == nil {
			klog.Info("vault returned an empty response, there may be an issue")
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
	if err := doEntity(vc, name, policyName); err != nil {
		return err
	}

	klog.Infof("done creating entity")

	//
	// Create entity-alias linked to previously created entity
	//
	if err := doEntityAlias(vc, oidcAuthAccessor, ownerName, name); err != nil {
		return err
	}

	klog.Infof("done creating entity-alias")

	return nil
}
