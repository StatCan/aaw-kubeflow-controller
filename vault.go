package main

import (
	"bytes"
	"fmt"
	"html/template"
	"strings"

	vault "github.com/hashicorp/vault/api"
	"k8s.io/klog"
)

// Create a VaultConfigurerStruct that implements the VaultConfigurer
func NewVaultConfigurer(vc *vault.Client, kubePath, oidcAccessor string, minioInstances []string) *VaultConfigurerStruct {
	return &VaultConfigurerStruct{
		Logical:            &LogicalWrapper{vc.Logical()},
		Mounts:             vc.Sys(),
		KubernetesAuthPath: kubePath,
		OidcAuthAccessor:   oidcAccessor,
		MinioInstances:     minioInstances,
	}
}

type VaultConfigurer interface {
	ConfigVaultForProfile(profileName, ownerName string, users []string) error
}

// Defines a configuration object with the constants used to
// configure the vault instance
type VaultConfigurerStruct struct {
	Logical            VaultLogicalAPI
	Mounts             VaultMountsAPI
	KubernetesAuthPath string
	OidcAuthAccessor   string
	MinioInstances     []string
}

//go:generate moq -out vault_mocks_test.go . VaultLogicalAPI VaultMountsAPI
// Interface to wrap vault functions for easier testing
type VaultLogicalAPI interface {
	Read(path string) (*vault.Secret, error)
	Write(path string, data map[string]interface{}) (*vault.Secret, error)
}

//Wrapper struct to allow easy extension of the Vault Api
type LogicalWrapper struct {
	Logical VaultLogicalAPI
}

func logWarnings(warnings []string) {
	if warnings != nil && len(warnings) > 0 {
		for _, warning := range warnings {
			klog.Warning(warning)
		}
	}
}

// Wraps the Vault API and outputs warnings if there are any
func (l *LogicalWrapper) Read(path string) (*vault.Secret, error) {
	secret, err := l.Logical.Read(path)

	if secret != nil {
		logWarnings(secret.Warnings)
	}

	return secret, err
}

// Wraps the Vault API and outputs warnings if there are any
func (l *LogicalWrapper) Write(path string, data map[string]interface{}) (*vault.Secret, error) {
	secret, err := l.Logical.Write(path, data)

	if secret != nil {
		logWarnings(secret.Warnings)
	}

	return secret, err
}

// Interface to wrap vault functions for easier testing
type VaultMountsAPI interface {
	ListMounts() (map[string]*vault.MountOutput, error)
	Mount(path string, mountInfo *vault.MountInput) error
}

const DEFAULT = "default"

const POLICY_TEMPLATE = `
#
# Policy for Kubeflow profile: {{ .ProfileName }}
# (policy managed by the custom Kubeflow Profiles controller)
#

# Grant full access to the KV created for this profile
path "kv_{{ .ProfileName }}/*" {
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

func generatePolicy(name string, minioInstances []string) (string, error) {

	t, _ := template.New("policy").Parse(POLICY_TEMPLATE)
	w := bytes.NewBufferString("")

	err := t.Execute(w, struct {
		ProfileName    string
		MinioInstances []string
	}{ProfileName: name, MinioInstances: minioInstances})

	if err != nil {
		return "", err
	}

	return w.String(), nil
}

// Writes a policy to Vault
func (vc *VaultConfigurerStruct) doPolicy(name string, mountName string) (string, error) {
	policy, err := generatePolicy(name, vc.MinioInstances)
	if err != nil {
		return name, err
	}

	var policyPath = fmt.Sprintf("/sys/policies/acl/%s", name)
	secret, err := vc.Logical.Read(policyPath)
	if err != nil {
		return name, err
	}

	if secret == nil || secret.Data["policy"].(string) != policy {
		secret, err = vc.Logical.Write(policyPath, map[string]interface{}{
			"policy": policy,
		})
		if err != nil {
			return name, err
		}
	}

	return name, nil
}

func hasMount(mounts map[string]*vault.MountOutput, mountName string) bool {
	for path := range mounts {
		if strings.ReplaceAll(path, "/", "") == mountName {
			return true
		}
	}

	return false
}

// creates a KV secret store for the profile
func (vc *VaultConfigurerStruct) doKVMount(name string) (string, error) {
	mountName := fmt.Sprintf("kv_%s", name)

	mounts, err := vc.Mounts.ListMounts()
	if err != nil {
		return mountName, err
	}

	if !hasMount(mounts, mountName) {
		klog.Infof("creating mount %q", mountName)

		err = vc.Mounts.Mount(mountName, &vault.MountInput{
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

// sets a value for a key if it isn't equal
func setValueIfNotEquals(payload map[string]interface{}, key string, actual []interface{}, expected []string) map[string]interface{} {

	sactual := make([]string, len(actual))
	for i, s := range actual {
		sactual[i] = fmt.Sprint(s)
		i++
	}

	if !StringArrayEquals(sactual, expected) {
		if payload == nil {
			payload = map[string]interface{}{}
		}
		payload[key] = expected
	}

	return payload
}

func (vc *VaultConfigurerStruct) doKubernetesBackendRole(namespace, roleName, policyName string) error {
	rolePath := fmt.Sprintf("/auth/%s/role/%s", vc.KubernetesAuthPath, roleName)

	secret, err := vc.Logical.Read(rolePath)
	if err != nil {
		return err
	}

	var payload map[string]interface{}
	var operation string
	if secret == nil {
		klog.Infof("creating backend role in %q for %q", vc.KubernetesAuthPath, roleName)

		payload = map[string]interface{}{
			"bound_service_account_names":      []string{"*"},
			"bound_service_account_namespaces": []string{namespace},
			"token_policies":                   []string{DEFAULT, policyName},
		}

		operation = "created"
	} else {
		//If not in right state reconfigure the role
		operation = "updated"

		serviceAccounts := []string{"*"}
		key := "bound_service_account_names"
		payload = setValueIfNotEquals(payload, key, secret.Data[key].([]interface{}), serviceAccounts)

		namespaces := []string{namespace}
		key = "bound_service_account_namespaces"
		payload = setValueIfNotEquals(payload, key, secret.Data[key].([]interface{}), namespaces)

		policies := []string{DEFAULT, policyName}
		key = "token_policies"
		payload = setValueIfNotEquals(payload, key, secret.Data[key].([]interface{}), policies)
	}

	if payload != nil {
		secret, err = vc.Logical.Write(rolePath, payload)

		if err != nil {
			return err
		}

		klog.Infof("backend role in %q for %q %s", vc.KubernetesAuthPath, namespace, operation)
	}

	return nil
}

//configures the minio secret stores for the given profile name
func (vc *VaultConfigurerStruct) doMinioRole(authPath, name string) error {
	rolePath := fmt.Sprintf("%s/roles/%s", authPath, name)

	secret, err := vc.Logical.Read(rolePath)
	if err != nil && !strings.Contains(err.Error(), "role not found") {
		return err
	}

	if secret == nil {
		klog.Infof("creating backend role in %q for %q", authPath, name)

		secret, err = vc.Logical.Write(rolePath, map[string]interface{}{
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
func (vc *VaultConfigurerStruct) doEntity(name string) (string, error) {
	entityNamePath := fmt.Sprintf("/identity/entity/name/%s", name)
	entityId := ""
	secret, err := vc.Logical.Read(entityNamePath)
	if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("No value found at %s", entityNamePath)) {
		return entityId, err
	}

	if secret == nil {
		secret, err = vc.Logical.Write(entityNamePath, map[string]interface{}{
			"metadata": map[string]string{
				"created_by": "kubeflow-controller",
			},
		})

		if err != nil {
			return entityId, err
		}
		klog.Infof("created entity %s", name)
	} else {
		klog.Infof("entity already created for %s", name)
	}
	entityId = secret.Data["id"].(string)

	return entityId, err
}

// checks to see if the alias is a part of the supplied array
func hasAlias(aliases []interface{}, mountAccessor, aliasName, canonicalId string) bool {
	if len(aliases) == 0 {
		return false
	}

	for _, alias := range aliases {
		var mapAlias = alias.(map[string]interface{})

		aliasEquals := mapAlias["name"].(string) == aliasName
		mountAccessorEquals := mapAlias["mount_accessor"].(string) == mountAccessor
		canonicalIdEquals := mapAlias["canonical_id"].(string) == canonicalId

		if aliasEquals && mountAccessorEquals && canonicalIdEquals {
			return true
		}
	}

	return false
}

//
// Creates an entity-alias to link an entity to its identity in OIDC if it doesn't exist
// owner_name = <oidc - preferred_name>
//
func (vc *VaultConfigurerStruct) doEntityAlias(entityName string) error {

	entityNamePath := fmt.Sprintf("/identity/entity/name/%s", entityName)

	var secret, err = vc.Logical.Read(entityNamePath)
	if err != nil {
		return err
	}

	var aliases = secret.Data["aliases"].([]interface{})
	var canonicalId = secret.Data["id"].(string)
	if !hasAlias(aliases, vc.OidcAuthAccessor, entityName, secret.Data["id"].(string)) {
		secret, err = vc.Logical.Write("/identity/entity-alias", map[string]interface{}{
			"name":           entityName,
			"mount_accessor": vc.OidcAuthAccessor,
			"canonical_id":   canonicalId,
		})

		if err != nil {
			return err
		}

		klog.Infof("created/update entity-alias for %s", entityName)

		if secret == nil {
			klog.Warning("vault returned an empty response, there may be an issue")
		}
	} else {
		klog.Infof("entity-alias already built for %s", entityName)
	}

	return nil
}

func (vc *VaultConfigurerStruct) doGroup(profileName, policyName string, entityIds []string) error {
	groupPath := fmt.Sprintf("/identity/group/name/%s", profileName)

	secret, err := vc.Logical.Read(groupPath)
	if err != nil {
		return err
	}

	var payload map[string]interface{}
	var operation string
	if secret == nil {
		payload = map[string]interface{}{
			"policies":          []string{policyName},
			"member_entity_ids": entityIds,
			"type":              "internal",
			"metadata": map[string]string{
				"created_by": "kubeflow-controller",
			},
		}

		operation = "created"
	} else {
		operation = "updated"

		policies := []string{policyName, DEFAULT}
		key := "policies"
		payload = setValueIfNotEquals(payload, key, secret.Data[key].([]interface{}), policies)

		key = "member_entity_ids"
		payload = setValueIfNotEquals(payload, key, secret.Data[key].([]interface{}), entityIds)
	}

	if payload != nil {
		secret, err = vc.Logical.Write(groupPath, payload)
		if err != nil {
			return err
		}
	}

	klog.Infof("%s group for %s", operation, profileName)

	return nil
}

func (vc *VaultConfigurerStruct) ConfigVaultForProfile(profileName, ownerName string, users []string) error {

	prefixedProfileName := fmt.Sprintf("profile-%s", profileName)

	//
	// Let's do for a KeyVault mount
	// for the profile.
	//
	var mountName string
	var err error
	if mountName, err = vc.doKVMount(prefixedProfileName); err != nil {
		return err
	}

	klog.Info("done mount")

	//
	// Add the policy to Vault.
	// If the policy already exists,
	// ensure it has the right value.
	//
	var policyName string
	if policyName, err = vc.doPolicy(prefixedProfileName, mountName); err != nil {
		return err
	}

	klog.Info("done policy")

	//
	// Add the Kubernetes backend role
	// to permit authentication from the profile's
	// namespace.
	//
	if err := vc.doKubernetesBackendRole(profileName, prefixedProfileName, policyName); err != nil {
		return err
	}

	klog.Info("done Kubernetes backend roles")

	//
	// Add MinIO role
	//
	//for _, instance := range vc.MinioInstances {
	//	if err := vc.doMinioRole(instance, prefixedProfileName); err != nil {
	//		return err
	//	}
	//}

	klog.Info("done MinIO backend roles")

	//
	// Create the entity associated to the profile
	//
	entityNames := append(users, ownerName)
	entityIds := make([]string, len(entityNames))
	for i, entityName := range entityNames {
		if id, err := vc.doEntity(entityName); err != nil {
			return err
		} else {
			entityIds[i] = id
		}
		if err := vc.doEntityAlias(entityName); err != nil {
			return err
		}
	}

	klog.Infof("done creating entities and aliases")

	err = vc.doGroup(prefixedProfileName, policyName, entityIds)
	if err != nil {
		return err
	}

	klog.Infof("done creating group")

	return nil
}
