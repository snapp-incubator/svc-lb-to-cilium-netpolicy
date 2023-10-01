## Usage

[Helm](https://helm.sh) must be installed to use the charts. Please refer to
Helm's [documentation](https://helm.sh/docs) to get started.

Once Helm has been set up correctly, add the repo as follows:

    helm repo add svc-lb-to-cilium-netpolicy-operator https://snapp-incubator.github.io/svc-lb-to-cilium-netpolicy

If you had already added this repo earlier, run `helm repo update` to retrieve
the latest versions of the packages.  You can then run `helm search repo
svc-lb-to-cilium-netpolicy-operator` to see the charts.

To install the svc-lb-to-cilium-netpolicy-operator chart:

    helm install my-svc-lb-to-cilium-netpolicy-operator svc-lb-to-cilium-netpolicy-operator/svc-lb-to-cilium-netpolicy-operator

To uninstall the chart:

    helm delete my-svc-lb-to-cilium-netpolicy-operator
