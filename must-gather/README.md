# Collecting node-maintenance related debug data

You can use the `oc adm must-gather` command to collect information about your cluster.

With the node-maintenance-must-gather image you can collect manifests and logs related to node maintenance:

- Node objects
- Custom Resource Definition
- Node Maintenance Operator pod's logs (dismiss drained pods)
- Custom Resources
- Cluster's resource (CPU, and Memory) usage

To collect this data, you must specify the extra image using the `--image` option.
Example:

```bash
oc adm must-gather --image=quay.io/medik8s/node-maintenance-must-gather:latest
```
