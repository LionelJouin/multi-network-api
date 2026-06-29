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
  name: network-device-hostnetworkdevice-io-hostnetworkdevice
spec:
  implementationType:
    group: network.device.hostnetworkdevice.io
    kind: HostNetworkDevice
EOF
```

```yaml
kubectl apply -f - <<EOF
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: multi-network-controller-hostnetworkdevices-cluster-role
rules:
- apiGroups: ["network.device.hostnetworkdevice.io"]
  resources: ["hostnetworkdevices"]
  verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: multi-network-controller-hostnetworkdevices-role-binding
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: multi-network-controller-hostnetworkdevices-cluster-role
subjects:
- kind: ServiceAccount
  name: multi-network-controller-service-account
  namespace: default
EOF
```

```sh
kubectl delete networkkind network-device-hostnetworkdevice-io-hostnetworkdevice
```