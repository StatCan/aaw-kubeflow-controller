package main

import (
	"fmt"
	vault "github.com/hashicorp/vault/api"
	"net/http"
	"testing"
)

// An easy way to do integration testing of the
// of the setup using a vault instance
//
// You can easily setup an vault instance by running
//  docker run --cap-add=IPC_LOCK -d -e VAULT_DEV_ROOT_TOKEN_ID=kubeflow-controller --name=dev-vault -p 8200:8200 vault
//
// TODO: Add a reduced policy for the token instead of root
// TODO: Use the vault-minio image and config it to use the minio plugin

const VAULT_ADDR = "http://localhost:8200"
const VAULT_TOKEN = "kubeflow-controller"
const kubernetesTestPath = "kubernetes"

var minioTestInstances = []string{"minio1", "minio2"}

func shouldRun(t *testing.T) {
	response, err := http.Get(VAULT_ADDR)
	if err != nil {
		t.Fatal(err)
	}

	if response.StatusCode != 200 {
		t.Skipf("Skipping test due to Vault status: %s", response.Status)
	}
}

func setupClient(t *testing.T) (*vault.Client, error) {
	shouldRun(t)
	vc, err := vault.NewClient(&vault.Config{
		Address: VAULT_ADDR,
	})
	if err != nil {
		t.Fatal(err)
	}
	vc.SetToken(VAULT_TOKEN)

	return vc, nil
}

func setupVault(t *testing.T, vaultClient *vault.Client) (string, error) {
	authMounts, err := vaultClient.Sys().ListAuth()
	if err != nil {
		t.Fatal(err)
	}

	if authMounts[fmt.Sprintf("%s/", kubernetesTestPath)] == nil {
		err = vaultClient.Sys().EnableAuthWithOptions(kubernetesTestPath, &vault.EnableAuthOptions{
			Type: kubernetesTestPath,
		})

		if err != nil {
			t.Fatal(err)
		}
	}

	if authMounts["oidc/"] == nil {
		err = vaultClient.Sys().EnableAuthWithOptions("oidc", &vault.EnableAuthOptions{
			Type: "oidc",
		})

		if err != nil {
			t.Fatal(err)
		}
	}

	authMounts, err = vaultClient.Sys().ListAuth()
	if err != nil {
		t.Fatal(err)
	}

	return authMounts["oidc/"].Accessor, err
}

func TestDoEntity(t *testing.T) {
	vaultClient, err := setupClient(t)
	if err != nil {
		t.Fatal(err)
	}

	vaultConfigurer := NewVaultConfigurer(vaultClient, "", "", minioTestInstances)

	id, err := vaultConfigurer.doEntity("jane.doe@test.ca")

	if err != nil {
		t.Fatal(err)
	}

	if id == "" {
		t.Fail()
	}
}

// TODO: Currently has issues with the minio secret backend, requires it to be commented out. :(
func TestConfigureVaultForProfile(t *testing.T) {
	vaultClient, err := setupClient(t)
	if err != nil {
		t.Fatal(err)
	}

	oidcAccessor, err := setupVault(t, vaultClient)
	if err != nil {
		t.Fatal(err)
	}

	vaultConfigurer := NewVaultConfigurer(vaultClient, kubernetesTestPath, oidcAccessor, minioTestInstances)

	err = vaultConfigurer.ConfigVaultForProfile("random-test45", "jeremy.smith@test.ca", []string{"mandy.doe@test.ca"})
	if err != nil {
		t.Fatal(err)
	}
}
