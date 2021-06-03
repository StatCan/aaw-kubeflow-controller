apiVersion: v1
kind: ServiceAccount
metadata:
  name: profile-configurator
  namespace: daaas
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: profile-configurator
  namespace: daaas
  labels:
    apps.kubernetes.io/name: profile-configurator
spec:
  selector:
    matchLabels:
      apps.kubernetes.io/name: profile-configurator
  template:
    metadata:
      labels:
        apps.kubernetes.io/name: profile-configurator
      annotations:
        vault.hashicorp.com/agent-inject: "true"
        vault.hashicorp.com/agent-configmap: "profile-configurator-vault-agent-config"
        vault.hashicorp.com/agent-pre-populate: "false"
        sidecar.istio.io/inject: 'false'
    spec:
      serviceAccountName: profile-configurator
      imagePullSecrets:
        - name: k8scc01covidacr-registry-connection
      containers:
      - name: profile-configurator
        image: k8scc01covidacr.azurecr.io/kubeflow-controller:${IMAGE_SHA}
        resources:
          limits:
            memory: "128Mi"
            cpu: "500m"
        env:
          - name: IMAGE_PULL_SECRET
            valueFrom:
              secretKeyRef:
                name: k8scc01covidacr-registry-connection
                key: .dockerconfigjson
          - name: VAULT_AGENT_ADDR
            value: http://127.0.0.1:8100
          - name: MINIO_INSTANCES
            value: ${MINIO_INSTANCES}
          - name: KUBERNETES_AUTH_PATH
            value: ${VAULT_AUTH_PATH}
          - name: OIDC_AUTH_ACCESSOR
            value: ${OIDC_AUTH_ACCESSOR}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: profile-configurator
rules:
- apiGroups:
    - ''
  resources:
    - 'secrets'
    - 'serviceaccounts'
  verbs:
    - watch
    - list
- apiGroups:
    - ''
  resources:
    - 'secrets'
  verbs:
    - create
- apiGroups:
    - ''
  resources:
    - 'secrets'
  verbs:
    - get
    - create
    - update
    - delete
  resourceNames:
    - image-pull-secret
- apiGroups:
    - ''
  resources:
    - 'serviceaccounts'
  verbs:
    - get
    - update
  resourceNames:
    - default-editor
- apiGroups:
    - 'kubeflow.org'
  resources:
    - 'profiles'
    - 'poddefaults'
  verbs:
    - get
    - list
    - watch
    - create
    - update
- apiGroups:
    - ''
  resources:
    - 'events'
  verbs:
    - create
    - patch
- apiGroups:
    - rbac.authorization.k8s.io
  resources:
    - 'rolebindings'
  verbs:
    - get
    - list
    - watch
    - create
    - update
    - delete
- apiGroups:
    - networking.istio.io
  resources:
    - 'envoyfilters'
  verbs:
    - get
    - list
    - watch
    - create
    - update
    - delete
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: profile-configurator
subjects:
- kind: ServiceAccount
  name: profile-configurator
  namespace: daaas
roleRef:
  kind: ClusterRole
  name: profile-configurator
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: profile-configurator-vault-agent-config
data:
  config.hcl: |
    "auto_auth" = {
      "method" = {
        "config" = {
          "role" = "profile-configurator"
        }
        "type" = "kubernetes"
        "mount_path" = "${VAULT_AUTH_PATH}"
      }
    }

    "exit_after_auth" = false
    "pid_file" = "/home/vault/.pid"

    cache {
      "use_auto_auth_token" = "force"
    }

    listener "tcp" {
      address = "127.0.0.1:8100"
      "tls_disable" = true
    }

    "vault" = {
      "address" = "${VAULT_ADDR}"
    }
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: seldon-user
rules:
- apiGroups:
  - machinelearning.seldon.io
  resources:
  - seldondeployments
  verbs:
  - '*'
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: seldon-profile-configurator
subjects:
- kind: ServiceAccount
  name: profile-configurator
  namespace: daaas
roleRef:
  kind: ClusterRole
  name: seldon-user
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: argo-profile-configurator
subjects:
- kind: ServiceAccount
  name: profile-configurator
  namespace: daaas
roleRef:
  kind: ClusterRole
  name: argo
  apiGroup: rbac.authorization.k8s.io
