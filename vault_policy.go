package main

import (
	"bytes"
	"fmt"
	"html/template"
)

const DEFAULT = "default"

const POLICY_TEMPLATE = `
#
# Policy for Kubeflow profile: {{ .ProfileName }}
# (policy managed by the custom Kubeflow Profiles controller)
#

# Grant full access to the KV created for this profile
path "kv_profile-{{ .ProfileName }}/*" {
	capabilities = ["create", "update", "delete", "read", "list"]
}

# Grant access to MinIO keys associated with this profile
{{- $profile := .ProfileName }}
{{- range $instance := .MinioInstances }}
path "{{ $instance }}/keys/profile-{{ $profile }}" {
	capabilities = ["read"]
}
{{- end }}
`

func generatePolicy(name, mountName string, minioInstances []string) (string, string) {

	t, _ := template.New("policy").Parse(POLICY_TEMPLATE)
	w := bytes.NewBufferString("")

	t.Execute(w, struct {
		MountName      string
		ProfileName    string
		MinioInstances []string
	}{MountName: mountName, ProfileName: name, MinioInstances: minioInstances})

	return name, w.String()
}

func (vc *VaultConfigurerStruct) doPolicy(name string, mountName string) (string, error) {
	policyName, policy := generatePolicy(name, mountName, vc.MinioInstances)

	var policyPath = fmt.Sprintf("/sys/policies/acl/%s", policyName)
	secret, err := vc.Logical.Read(policyPath)
	if err != nil {
		return policyName, err
	}

	if secret == nil || secret.Data["policy"].(string) != policy {
		secret, err = vc.Logical.Write(policyPath, map[string]interface{}{
			"policy": policy,
		})
		if err != nil {
			return policyName, err
		}
	}

	return policyName, nil
}
