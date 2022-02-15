// This is a generated file. Do not edit directly.

module github.com/StatCan/kubeflow-controller

go 1.13

require (
	github.com/go-ini/ini v1.62.0 // indirect
	github.com/gogo/protobuf v1.3.1
	github.com/hashicorp/vault/api v1.0.4
	github.com/minio/minio-go v6.0.14+incompatible // indirect
	github.com/minio/minio-go/v7 v7.0.5 // indirect
	google.golang.org/genproto v0.0.0-20190819201941-24fa4b261c55
	istio.io/api v0.0.0-20201217173512-1f62aaeb5ee3
	istio.io/client-go v1.8.1
	k8s.io/api v0.0.0-20200403220253-fa879b399cd0
	k8s.io/apimachinery v0.18.1
	k8s.io/client-go v0.18.1
	k8s.io/code-generator v0.0.0-20200403215918-804a58607501
	k8s.io/klog v1.0.0
)

replace (
	golang.org/x/sys => golang.org/x/sys v0.0.0-20190813064441-fde4db37ae7a // pinned to release-branch.go1.13
	golang.org/x/tools => golang.org/x/tools v0.0.0-20190821162956-65e3620a7ae7 // pinned to release-branch.go1.13
	k8s.io/api => k8s.io/api v0.0.0-20200403220253-fa879b399cd0
	k8s.io/apimachinery => k8s.io/apimachinery v0.0.0-20200403220105-fa0d5bf06730
	k8s.io/client-go => k8s.io/client-go v0.0.0-20200403220520-7039b495eb3e
	k8s.io/code-generator => k8s.io/code-generator v0.0.0-20200403215918-804a58607501
)
