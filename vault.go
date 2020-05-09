package main

import (
	"fmt"
	"strings"

	vault "github.com/hashicorp/vault/api"
	"k8s.io/klog"
)

func NewVaultConfigurer(vc *vault.Client, kubernetesAuthPath, oidcAuthAccess string, minioInstances []string) VaultConfigurer {
	return &VaultConfigurerStruct{
		Logical:            vc.Logical(),
		Mounts:             vc.Sys(),
		KubernetesAuthPath: kubernetesAuthPath,
		OidcAuthAccessor:   oidcAuthAccessor,
		MinioInstances:     minioInstances,
	}
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

//Interface to wrap vault functions for easier testing
type VaultLogicalAPI interface {
	Read(path string) (*vault.Secret, error)
	Write(path string, data map[string]interface{}) (*vault.Secret, error)
	List(path string) (*vault.Secret, error)
}

type VaultMountsAPI interface {
	ListMounts() (map[string]*vault.MountOutput, error)
	Mount(path string, mountInfo *vault.MountInput) error
}

func hasMount(mounts map[string]*vault.MountOutput, mountName string) bool {
	for path := range mounts {
		if strings.ReplaceAll(path, "/", "") == mountName {
			return true
		}
	}

	return false
}

func (vc *VaultConfigurerStruct) doMount(name string) (string, error) {
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

func (vc *VaultConfigurerStruct) doKubernetesBackendRole(authPath, namespace, roleName, policyName string) error {
	rolePath := fmt.Sprintf("/%s/role/%s", authPath, roleName)

	secret, err := vc.Logical.Read(rolePath)
	if err != nil {
		return err
	}

	if secret == nil {
		klog.Infof("creating backend role in %q for %q", authPath, roleName)

		secret, err = vc.Logical.Write(rolePath, map[string]interface{}{
			"bound_service_account_names":      []string{"*"},
			"bound_service_account_namespaces": []string{namespace},
			"policies":                         []string{DEFAULT, policyName},
		})

		if err != nil {
			return err
		}
	} else {
		//TODO: Update role if not correctly configure
		klog.Infof("backend role in %q for %q already exists", authPath, namespace)
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
func (vc *VaultConfigurerStruct) doEntityAlias(mountAccessor string, ownerName string, name string) error {

	entityNamePath := fmt.Sprintf("/identity/entity/name/%s", name)

	var secret, err = vc.Logical.Read(entityNamePath)
	if err != nil {
		return err
	}

	var aliases = secret.Data["aliases"].([]interface{})
	var canonicalId = secret.Data["id"].(string)
	if !hasAlias(aliases, mountAccessor, ownerName, secret.Data["id"].(string)) {
		secret, err = vc.Logical.Write("/identity/entity-alias", map[string]interface{}{
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

type VaultConfigurer interface {
	ConfigVaultForProfile(profileName, ownerName string, users []string) error
}

func (vc *VaultConfigurerStruct) ConfigVaultForProfile(profileName, ownerName string, users []string) error {
	//create mount
	prefixedProfileName := fmt.Sprintf("profile-%s", profileName)

	//
	// Let's do for a KeyVault mount
	// for the profile.
	//
	var mountName string
	var err error
	if mountName, err = vc.doMount(prefixedProfileName); err != nil {
		return err
	}

	klog.Info("done mount")

	//create policy
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

	//create kubernetes role
	//
	// Add the Kubernetes backend role
	// to permit authentication from the profile's
	// namespace.
	//
	if err := vc.doKubernetesBackendRole(kubernetesAuthPath, profileName, prefixedProfileName, policyName); err != nil {
		return err
	}

	klog.Info("done Kubernetes backend roles")

	//
	// Add MinIO role
	//
	for _, instance := range vc.MinioInstances {
		if err := vc.doMinioRole(instance, prefixedProfileName); err != nil {
			return err
		}
	}

	klog.Info("done MinIO backend roles")

	//create entities/alias
	// Create the entity associated to the profile
	//
	entityNames := append(users, ownerName)
	entityIds := make([]string, len(entityNames))
	for i, entityName := range entityNames {
		if id, err := vc.doEntity(entityName, policyName); err != nil {
			return err
		} else {
			entityIds[i] = id
		}
	}
	klog.Infof("done creating entity")

	//
	// Create entity-alias linked to previously created entity
	//
	//if err := doEntityAlias(vc, oidcAuthAccessor, ownerName, prefixedProfileName); err != nil {
	//	return err
	//}

	klog.Infof("done creating entity-alias")

	//create group and add entities, assign policy

	return nil
}
