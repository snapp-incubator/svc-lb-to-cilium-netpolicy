# svc-lb-to-cilium-netpolicy Operator

## Description
The svc-lb-to-cilium-netpolicy operator addresses a significant challenge in our clusters where NetworkPolicies and CiliumNetworkPolicies are employed to establish strict network segmentation for security purposes.

Based on the current network policies, pods are restricted to receiving traffic only from other pods within the same namespace or specific designated namespaces. However, Some workloads are external to the cluster; These external workloads rely on services of type LoadBalancer to establish connections with cluster pods.<br>The problem arises when these external workloads are eventually migrated into the cluster, often placed in namespaces different from the ones housing the pods they need to communicate with. The existing network policies, initially configured for security, inadvertently disrupt these critical communication paths, causing disruptions and connectivity issues when external workloads are brought into the cluster.

The primary purpose of the svc-lb-to-cilium-netpolicy operator is to enable uninterrupted communication between previously-external workloads, now residing within the Kubernetes cluster, and the LoadBalancer service endpoints. It achieves this by automatically managing CiliumNetworkPolicies to ensure that these workloads can establish connections with LoadBalancer service endpoints, overcoming the limitations posed by existing policies.

## Getting Started
You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.

### Building the helm chart

We use [helmify](https://github.com/arttor/helmify) to generate Helm chart from kustomize rendered manifests. To update
the chart run:

```shell
make helm
```

### Test It Out
1. Compile the code:

```sh
make build
```

2. Run the controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
# Note: The controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).
./bin/manager --config-file-path ./hack/config.yaml
```
