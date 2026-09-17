# Node Maintenance Operator (NMO) — Operational runbook

## Purpose

Commands and fields for **NodeMaintenance** day-2 ops. Behaviour details: **`architecture.md`**, **`failure_modes.md`**.

## Components

| Piece | Role |
|-------|------|
| **Operator deployment** | Reconciles **`NodeMaintenance`**, serves **validating webhook**. |
| **CRD** | **`nodemaintenance.medik8s.io/v1beta1`**, kind **`NodeMaintenance`**, short **`nm`**, **cluster-scoped**. |

## Operator process (observability)

| Topic | Note |
|-------|------|
| **Health / readiness** | Default probe bind **`:8081`** (`cmd/main.go`). |
| **Metrics** | **`--metrics-bind-address`** default **`:8443`**; secured (TLS + Kubernetes bearer-token authn/authz via controller-runtime filters) — same pattern as NHC/SNR. |
| **Webhook** | TLS under **`/apiserver.local.config/certificates`** when OLM injects certs; port **9443** default. On OpenShift, TLS settings (both metrics and webhook) also follow the cluster's **`APIServer`** TLS profile, and the process restarts if that profile changes. |
| **Leader election** | **`--leader-elect`** defaults **false** in code; CSV may override. ID **`135b1886.medik8s.io`**. |

## Prerequisites

| Requirement | Note |
|-------------|------|
| **RBAC** | Operator SA can **patch nodes**, **evict pods**, manage **leases**, list **apps** workloads (for drain). |
| **OpenShift** | Control-plane **NodeMaintenance** create requires **etcd** disruption allowed (webhook). |
| **Single CR per node** | Enforced at admission. |

## Quick checks

```bash
kubectl get nodemaintenance
kubectl get nm
kubectl describe nm <name>
kubectl get node <nodeName> -o yaml
kubectl get leases.coordination.k8s.io -A | grep -i maintenance || true
```

Inspect **taints** and **labels** on the node:

```bash
kubectl get node <nodeName> -o jsonpath='{.spec.taints}{"\n"}{.metadata.labels.remediation\.medik8s\.io/exclude-from-remediation}{"\n"}'
```

## What to inspect on **NodeMaintenance**

| Area | Look for |
|------|----------|
| **`spec.nodeName`**, **`reason`** | Target and intent. |
| **`status.phase`** | **Running** → **Succeeded** or **Failed**. |
| **`status.drainProgress`** | Progress during eviction retries. |
| **`status.pendingPods` / `pendingPodsRefs`** | Work still on the node. |
| **`status.lastError`** | Last reconcile error text. |
| **`status.errorOnLeaseCount`** | Approaching **>3** → risk of **Failed** + uncordon. |
| **Events** | **BeginMaintenance**, **SucceedMaintenance**, **FailedMaintenance**, **RemovedMaintenance**, **UncordonNode**. |

## What happens on the **Node** during maintenance

| Effect | Note |
|--------|------|
| **Taints** | **`node.kubernetes.io/unschedulable`** and **`medik8s.io/drain`** (**NoSchedule**). |
| **Cordon** | **`spec.unschedulable`** via drain helper. |
| **Label** | **`remediation.medik8s.io/exclude-from-remediation=true`**. |

## Debugging checklist

1. **Webhook** blocking create? — node exists, no duplicate **nm**, OpenShift **etcd** guard for masters.  
2. **Lease** errors? — **`leases.coordination.k8s.io`**, competing operators.  
3. **Drain** looping? — PDBs, **forbidden** evictions, terminating pods, **30s** timeout.  
4. **Stuck terminating CR?** — Operator logs, finalizer, RBAC **update** on **NodeMaintenance**.  

## Related pieces

- **`code_map.md`** — source file index.
