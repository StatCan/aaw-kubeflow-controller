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

func doVaultConfiguration(vc *vault.Client, profileName string, minioInstances []string, kubernetesAuthPath string) error {
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
	return nil
}
