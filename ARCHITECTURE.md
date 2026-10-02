# Node Maintenance Operator (NMO) — Architecture

## Purpose

Runtime behaviour of **Node Maintenance Operator**: **`NodeMaintenance`** reconciliation, **webhook** rules, **taints**, **drain**, and **lease** integration. Read **`overview.md`** first.

**Code reference:** `github.com/medik8s/node-maintenance-operator`.

## API scope

- **Group:** `nodemaintenance.medik8s.io`, **`v1beta1`**
- **Kind:** **`NodeMaintenance`** (short **`nm`**)
- **Scope:** **Cluster**
- **Spec:** **`nodeName`** (required), **`reason`** (optional)
- **Status:** **`phase`** — **`Running`**, **`Succeeded`**, **`Failed`**; **`drainProgress`** (0–100 after first error path); **`lastUpdate`**, **`lastError`**; **`pendingPods`**, **`pendingPodsRefs`**; **`totalpods`**, **`evictionPods`**; **`errorOnLeaseCount`**
- **Finalizer:** **`foregroundDeleteNodeMaintenance`** (`NodeMaintenanceFinalizer` constant)

## Validating webhook (`internal/webhook/v1beta1/nodemaintenance_webhook.go`)

- **Create**
  - Node with **`spec.nodeName`** must exist.
  - No other **`NodeMaintenance`** may reference the same **`nodeName`**.
  - On **OpenShift** (`isOpenShift` from **`utils.NewOpenshiftValidator`** in **`cmd/main.go`**), if the node is **control-plane**, **`etcd.IsEtcdDisruptionAllowed`** must allow disruption (uses **etcd** guard PDB logic in **`openshift-etcd`** namespace — **etcd-guard-pdb** / **etcd-quorum-guard** naming in webhook constants).
- **Non-OpenShift:** control-plane **quorum** webhook check is **skipped** (log notes no etcd PDB).
- **Update:** changing **`spec.nodeName`** is **forbidden**.
- **Delete:** validator runs but returns **no error** (logging only).

## Operator startup (`cmd/main.go`)

- **Scheme:** core + **`api/v1beta1`** + OpenShift **`config.openshift.io/v1`** (needed to read the cluster's API server TLS profile)
- **OpenShift TLS profile:** at startup, **`openshifttls.FetchAPIServerTLSProfile`** probes for the OpenShift `APIServer` config; if absent (non-OpenShift, e.g. `NoMatch`/`NotFound`), falls back to default Go TLS settings. When present, the profile's cipher/version settings are applied to **both** the metrics server and the webhook server TLS options, and a **`SecurityProfileWatcher`** runnable is added that **restarts the process** (cancels the manager context) if the profile changes at runtime.
- **Manager:** **Webhook** server (`webhook.NewServer(webhookOpts)`) — cert dir set to **`/apiserver.local.config/certificates`** only when OLM has injected certs there (`configureWebhookOpts`); **HTTP/2** disabled by default (forces `http/1.1`) for CVE mitigation, re-enabled via **`--enable-http2`**.
- **Leader election:** **`--leader-elect`** (default **false** in source), **`LeaderElectionID`:** **`135b1886.medik8s.io`**
- **Runnable:** **`leaseManagerInitializer`** calls **`lease.NewManager`** with **`controller.LeaseHolderIdentity`** (**`node-maintenance`**) after start
- **Probes:** **`--health-probe-bind-address`** (default **`:8081`**)
- **Metrics:** **`--metrics-bind-address`** (default **`:8443`**, flag name unchanged); manager `Metrics` **is** wired via `metrics/server.Options{BindAddress, SecureServing: true, FilterProvider: filters.WithAuthenticationAndAuthorization, TLSOpts}` — same secured-metrics pattern (TLS + Kubernetes bearer-token authn/authz) used by NHC and SNR.
- **Webhook registration:** **`webhookv1beta1.SetupNodeMaintenanceWebhookWithManager(isOpenShift, mgr)`** (package `internal/webhook/v1beta1`, not a method on the CR type)

## NodeMaintenance reconciler (`internal/controller/nodemaintenance_controller.go`)

### High-level order

1. **Get** CR; **not found** → done.
2. Build **`drain.Helper`** via **`createDrainer`** (clientset from **`MgrConfig`**).
3. **Deleting** (`DeletionTimestamp` set):
   - If finalizer present: **`stopNodeMaintenanceOnDeletion`** (fetch node; on node **NotFound** still try **InvalidateLease** on a bare **`Node` name**); else **`stopNodeMaintenanceImp`** — remove taints, **uncordon**, **InvalidateLease** (ignore **AlreadyHeld**), remove **`remediation.medik8s.io/exclude-from-remediation`** label.
   - Remove finalizer, **Update** object, emit **RemovedMaintenance**.
4. **Finalizer:** add **`NodeMaintenanceFinalizer`** if missing, **Update**, **BeginMaintenance** event.
5. **`initMaintenanceStatus`:** when **`status.phase` empty**, set phase **Running**, **`GetPodsForDeletion`**, list all pods on node for **total** / **eviction** counts; **Status().Update** if needed.
6. **Fetch node**; **NotFound** → **Failed** phase, **FailedMaintenance** warning, **`onReconcileError`** (stops requeueing for the expected not-found message).
7. **`setOwnerRefToNode`** — append **Node** **ownerReference** (non-controller).
8. **Lease budget:** if **`errorOnLeaseCount` > 3** → **`stopNodeMaintenanceImp`**, **Failed**, set **LastError**, **Status().Update**, return.
9. **`LeaseManager.RequestLease(ctx, node, LeaseDuration)`** (**3600s**). On error:
   - **`AlreadyHeldError`**: if **`drainProgress` > 0**, increment **`errorOnLeaseCount`** and error-return; else error-return without increment (message distinguishes “extend” vs “obtain”).
   - Other errors: increment **`errorOnLeaseCount`**, return error.
10. On success: clear failure state, set **Running** if needed, **`errorOnLeaseCount` = 0`.
11. **`addExcludeRemediationLabel`** — set **`remediation.medik8s.io/exclude-from-remediation`** to **`true`**.
12. **`AddOrRemoveTaint(..., true)`** then **`drain.RunCordonOrUncordon(..., true)`**.
13. **`drain.RunNodeDrain`**. On error: **`onReconcileErrorWithRequeue`** with **5s**, refresh **pending** pods and **drainProgress**.
14. On success: **Succeeded**, **drainProgress** 100, clear pending, **SucceedMaintenance** event, **Status().Update**.

### Drain helper defaults (`createDrainer`)

- **`Force`:** **true** (pods not owned by RC/RS/Job/DS/STS still evicted — needed for **VMI**-style pods per comments).
- **`DeleteEmptyDirData`:** **true**
- **`IgnoreAllDaemonSets`:** **true**
- **`GracePeriodSeconds`:** **-1** (use pod spec)
- **`Timeout`:** **`DrainerTimeout`** = **30s**
- **`OnPodDeletedOrEvicted`** logs via **klog**

### Taints (`internal/controller/taint.go`)

Maintenance applies **two** **NoSchedule** taints: **`node.kubernetes.io/unschedulable`** and **`medik8s.io/drain`**, using JSON patch **test+add/replace** on **`spec.taints`**.

### Error handling

- **`onReconcileError` / `onReconcileErrorWithRequeue`:** set **`lastError`**, **`lastUpdate`**, refresh **pending** pod lists and **drainProgress** when **`nodeName` set**.
- Special case: error text equals **`nodes "%s" not found`** → return **no error** (no infinite requeue).

## Lease constants (`internal/controller/nodemaintenance_controller.go`)

- **`LeaseHolderIdentity`:** **`node-maintenance`**
- **`LeaseDuration`:** **1 hour**
- **`maxAllowedErrorToUpdateOwnedLease`:** **3**

## RBAC (markers on reconciler)

See **`nodemaintenance_controller.go`** kubebuilder comments: **NodeMaintenance** full verbs; **nodes** patch/update; **pods** list/get/watch; **pods/eviction** create; **leases**; **PDBs**; **apps** list for drain; **namespaces** get/create; **monitoring.coreos.com** ServiceMonitor; **oauth.openshift.io** * (verify bundle vs cluster need).

## Related pieces

- **`failure_modes.md`**, **`runbook.md`**, **`code_map.md`**

## Scope

Does **not** document every **OLM** CSV argument or bundle manifest; confirm **leader-elect** and metrics in your deployment.
