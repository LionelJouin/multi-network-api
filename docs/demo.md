# Demo

```
kubectl apply -f ./deployment
```

```yaml
kubectl apply -f - <<EOF
---
apiVersion: multinetwork.networking.x-k8s.io/v1alpha1
kind: NetworkKind
metadata:
  name: devicenetwork-io-devicenetwork
spec:
  implementationType:
    group: devicenetwork.io
    kind: DeviceNetwork
EOF
```

```yaml
kubectl apply -f - <<EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: multi-network-controller-devicenetwork-cluster-role
rules:
- apiGroups: ["devicenetwork.io"]
  resources: ["devicenetworks"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: multi-network-controller-devicenetwork-role-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: multi-network-controller-devicenetwork-cluster-role
subjects:
- kind: ServiceAccount
  name: multi-network-controller-service-account
  namespace: default
EOF
```

```yaml
kubectl apply -f - <<EOF
---
apiVersion: v1
kind: Service
metadata:
  name: service-a
  labels:
    multinetwork.networking.k8s.io/spec.podnetwork.kind: devicenetwork-io-devicenetwork
    multinetwork.networking.k8s.io/spec.podnetwork.name: all-network
spec:
  clusterIP: None
  selector:
    app: demo
    multinetwork.networking.k8s.io/dummy: "true"
EOF
```

```sh
kubectl delete networkkind devicenetwork-io-devicenetwork
```


kubectl apply -f ./deployment

kubectl apply -f examples/example.yaml
kubectl apply -f examples/demo.yaml