package main

import (
	vault "github.com/hashicorp/vault/api"
	"k8s.io/apimachinery/pkg/labels"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"testing"
	"time"
)

const expectedPolicy = `
#
# Policy for Kubeflow profile: profile-test
# (policy managed by the custom Kubeflow Profiles controller)
#

# Grant full access to the KV created for this profile
path "kv_profile-test/*" {
	capabilities = ["create", "update", "delete", "read", "list"]
}

# Grant access to MinIO keys associated with this profile
path "minio1/keys/profile-test" {
	capabilities = ["read"]
}
path "minio2/keys/profile-test" {
	capabilities = ["read"]
}
`

func TestGeneratePolicy(t *testing.T) {

	policy, err := generatePolicy("profile-test", []string{"minio1", "minio2"})

	if err != nil {
		t.Fatal(err)
	}

	if policy != expectedPolicy {
		println(policy)
		t.Fail()
	}
}

func TestRoleBindings(t *testing.T) {

	cfg, err := clientcmd.BuildConfigFromFlags("https://k8s-cancentral-02-covid-aks-acdbb14c.hcp.canadacentral.azmk8s.io:443", "C:/Users/justin.bertrand/.kube/config")
	if err != nil {
		t.Fatalf("Error building kubeconfig: %s", err.Error())
	}

	kubeClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatalf("Error building kubernetes clientset: %s", err.Error())
	}
	kubeInformerFactory := kubeinformers.NewSharedInformerFactory(kubeClient, time.Second*1)

	roleBindings, err := kubeInformerFactory.Rbac().V1().RoleBindings().Lister().RoleBindings("zachary-seguin").List(labels.Everything())

	for _, role := range roleBindings {
		t.Log(role.RoleRef.Name)
		for _, subject := range role.Subjects {
			t.Log(subject.Name)
		}
	}
}

// Tests updatePolicy when there is already a policy
// defined in Vault
func TestDoPolicy_updatePolicy(t *testing.T) {
	var vc = VaultConfigurerStruct{
		Logical: &VaultLogicalAPIMock{
			ReadFunc: func(path string) (*vault.Secret, error) {

				if path != "/sys/policies/acl/profile-test" {
					t.Log("Wrong path")
					t.Fail()
				}
				return &vault.Secret{
					Renewable: false,
					Data: map[string]interface{}{
						"name":   "profile-test",
						"policy": "This policy is out of data",
					},
				}, nil
			},
			WriteFunc: func(path string, data map[string]interface{}) (*vault.Secret, error) {

				if path != "/sys/policies/acl/profile-test" {
					t.Log("Wrong path")
					t.Fail()
				}

				if data["policy"] != expectedPolicy {
					t.Log("Policy was not generated as expected")
					t.Fail()
				}

				return &vault.Secret{}, nil
			},
		},
		MinioInstances: []string{"minio1", "minio2"},
	}
	policyName, _ := vc.doPolicy("profile-test")

	if policyName != "profile-test" {
		t.Logf("Expected profile-test as policy name, got %s", policyName)
		t.Fail()
	}
}

//func TestDoKVMount_NoMount(t *testing.T) {
//	var vc = VaultConfigurerStruct{
//		Logical: nil,
//		Mounts: &VaultMountsAPIMock{
//			ListMountsFunc: func() (map[string]*vault.MountOutput, error) {
//
//			},
//			MountFunc: func(path string, mountInfo *vault.MountInput) error {
//
//			},
//		},
//		MinioInstances:     []string{"minio1", "minio2"},
//	}
//}
