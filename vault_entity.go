package main

import (
	"fmt"
	"k8s.io/klog"
	"strings"
)

//
// Creates or updates a Vault entity for the owner of the profile where
// name = <ownerName>
// policies = <policy-name>
//
func (vc *VaultConfigurerStruct) doEntity(name string, policyName string) (string, error) {
	entityNamePath := fmt.Sprintf("/identity/entity/name/%s", name)
	id := ""
	secret, err := vc.Logical.Read(name)
	if err != nil && !strings.Contains(err.Error(), fmt.Sprintf("No value found at %s", entityNamePath)) {
		return id, err
	}

	if secret == nil {
		secret, err = vc.Logical.Write(entityNamePath, map[string]interface{}{
			"policies": []string{policyName},
		})

		if err != nil {
			return id, err
		}
		id = secret.Data["id"].(string)
		klog.Infof("created entity %s", name)
	} else {
		//TODO: this can be more concise (do we want to append the policy or set it to a specific state?)
		//Convert to strings for easy matching
		sPolicies := make([]string, 0)
		for _, v := range secret.Data["policies"].([]interface{}) {
			sPolicies = append(sPolicies, fmt.Sprint(v))
		}

		// Check to see if the policy is assigned, if not add
		if !StringArrayContains(sPolicies, policyName) {
			var payload = map[string]interface{}{}
			payload["policies"] = append(secret.Data["policies"].([]interface{}), policyName)
			secret, err = vc.Logical.Write(entityNamePath, payload)

			if err != nil {
				return id, err
			}

			id = secret.Data["id"].(string)
		}
	}
	klog.Infof("updated entity %s", name)
	return id, nil
}
