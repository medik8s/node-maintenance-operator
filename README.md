# Node Maintenance Operator

The node-maintenance-operator is an operator generated from the [operator-sdk](https://github.com/operator-framework/operator-sdk).
The purpose of this operator is to watch for new or deleted custom resources called NodeMaintenance which indicate that a node in the cluster should either:
  - NodeMaintenance CR created: move node into maintenance, cordon the node - set it as unschedulable and evict the pods (which can be evicted) from that node.
  - NodeMaintenance CR deleted: remove pod from maintenance and uncordon the node - set it as schedulable.

> *Note*:  The current behavior of the operator is to mimic `kubectl drain <node name>`
> as performed in [medik8s - evict all VMs and Pods on a node ](https://medik8s.io/user-guide/docs/latest/administration/node-eviction.html#how-to-evict-all-vms-and-pods-on-a-node)

## Build and run the operator

There are two ways to run the operator:

- Deploy the latest version, which was built from master branch, to a running Openshift/Kubernetes cluster.
- Build and run or deploy from sources to a running or to be created Openshift/Kubernetes cluster.

### Deploy the latest version

After every PR merge to master images were build and pushed to `quay.io`.
For deployment of NMO using these images you need:

- a running Openshift cluster, or a Kubernetes cluster with OLM installed.
- `operator-sdk` binary installed, see https://sdk.operatorframework.io/docs/installation/
- a valid `$KUBECONFIG` configured to access your cluster.

Then run `operator-sdk run bundle quay.io/medik8s/node-maintenance-operator-bundle:latest`

### Build and deploy from sources
Follow the instructions [here](https://sdk.operatorframework.io/docs/building-operators/golang/tutorial/#3-deploy-your-operator-with-olm) for deploying the operator with OLM.
> *Note*: Webhook cannot run using `make deploy`, because the volume mount of the webserver certificate is not found.

## Setting Node Maintenance

### Set Maintenance on - Create a NodeMaintenance CR

To set maintenance on a node a `NodeMaintenance` CustomResource should be created.
The `NodeMaintenance` CR spec contains:
- nodeName: The name of the node which will be put into maintenance.
- reason: the reason for the node maintenance.

Create the example `NodeMaintenance` CR found at `config/samples/nodemaintenance_v1beta1_nodemaintenance.yaml`:

```sh
$ cat config/samples/nodemaintenance_v1beta1_nodemaintenance.yaml
apiVersion: nodemaintenance.medik8s.io/v1beta1
kind: NodeMaintenance
metadata:
  name: nodemaintenance-sample
spec:
  nodeName: node02
  reason: "Test node maintenance"

$ kubectl apply -f config/samples/nodemaintenance_v1beta1_nodemaintenance.yaml

$ kubectl logs <nmo-pod-name>
{"level":"info","ts":1551794418.6742408,"logger":"controller_nodemaintenance","msg":"Reconciling NodeMaintenance","Request.Namespace":"default","Request.Name":"node02"}
{"level":"info","ts":1551794418.674294,"logger":"controller_nodemaintenance","msg":"Applying Maintenance mode on Node: node02 with Reason: Test node maintenance","Request.Namespace":"default","Request.Name":"node02"}
{"level":"info","ts":1551783365.7430992,"logger":"controller_nodemaintenance","msg":"WARNING: ignoring DaemonSet-managed Pods: default/local-volume-provisioner-5xft8, medik8s/disks-images-provider-bxpc5, medik8s/virt-handler-52kpr, openshift-monitoring/node-exporter-4c9jt, openshift-node/sync-8w5x8, openshift-sdn/ovs-kvz9w, openshift-sdn/sdn-qnjdz\n"}
{"level":"info","ts":1551783365.7471824,"logger":"controller_nodemaintenance","msg":"evicting pod \"virt-operator-5559b7d86f-2wsnz\"\n"}
{"level":"info","ts":1551783365.7472217,"logger":"controller_nodemaintenance","msg":"evicting pod \"cdi-operator-55b47b74b5-9v25c\"\n"}
{"level":"info","ts":1551783365.747241,"logger":"controller_nodemaintenance","msg":"evicting pod \"virt-api-7fcd86776d-652tv\"\n"}
{"level":"info","ts":1551783365.747243,"logger":"controller_nodemaintenance","msg":"evicting pod \"simple-deployment-1-m5qv9\"\n"}
{"level":"info","ts":1551783365.7472336,"logger":"controller_nodemaintenance","msg":"evicting pod \"virt-controller-8987cffb8-29w26\"\n"}
...
```

### Set Maintenance off - Delete the NodeMaintenance CR

To remove maintenance from a node, delete the corresponding `NodeMaintenance` CR:

```sh
$ kubectl delete nodemaintenance nodemaintenance-sample

$ kubectl logs <nmo-pod-name>
{"level":"info","ts":1551794725.0018933,"logger":"controller_nodemaintenance","msg":"Reconciling NodeMaintenance","Request.Namespace":"default","Request.Name":"node02"}
{"level":"info","ts":1551794725.0021605,"logger":"controller_nodemaintenance","msg":"NodeMaintenance Object: default/node02 Deleted ","Request.Namespace":"default","Request.Name":"node02"}
{"level":"info","ts":1551794725.0022023,"logger":"controller_nodemaintenance","msg":"uncordon Node: node02"}

```

## NodeMaintenance Status

The NodeMaintenance CR can contain the following status fields:

```yaml
apiVersion: nodemaintenance.medik8s.io/v1beta1
kind: NodeMaintenance
metadata:
  name: nodemaintenance-xyz
spec:
  nodeName: node02
  reason: "Test node maintenance"
status:
  phase: "Running"
  lastError: "Last failure message"
  pendingPods: [pod-A,pod-B,pod-C]
  totalPods: 5
  evictionPods: 3

```

`phase` is the representation of the maintenance progress and can hold a string value of: Running|Succeeded.
The phase is updated for each processing attempt on the CR.

`lastError` represents the latest error if any for the latest reconciliation.

`pendingPods` PendingPods is a list of pending pods for eviction.

`totalPods` is the total number of all pods on the node from the start.

`evictionPods` is the total number of pods up for eviction from the start.

## Tests

### Run code checks and unit tests

`make check`

### Run e2e tests

1. Deploy the operator as explained above
2. run `make cluster-functest`

## Releases

### Creating a new release

For new minor releases:

  - create and push the `release-0.y` branch.

For every major / minor / patch release:

  - create and push the `vx.y.z` tag.
  - this should trigger CI to build and push new images
    - if it fails, the manual fallback is `VERSION=x.y.z make container-build container-push`
  - make the git tag a release in the github UI.
