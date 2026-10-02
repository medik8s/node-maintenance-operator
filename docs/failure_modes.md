# Node Maintenance Operator (NMO) — Failure modes

## Purpose

Symptom-first notes for NMO. Pair with **`architecture.md`** and **`runbook.md`**.

## Governing constants (selected)

| Name | Meaning |
|------|---------|
| **Finalizer** | `foregroundDeleteNodeMaintenance` |
| **Lease identity** | `node-maintenance` (holder name in **medik8s/common** lease) |
| **Lease duration** | **3600s** in controller |
| **Max lease errors** | **4th** failure path (**`errorOnLeaseCount` > 3**) triggers **Failed** + **uncordon** |
| **Drain requeue** | **5s** after drain error (`waitDurationOnDrainError`) |
| **Drain timeout** | **30s** per **`drain.Helper`** |
| **Exclude label** | `remediation.medik8s.io/exclude-from-remediation` = `true` |
| **Maintenance taints** | `node.kubernetes.io/unschedulable`, `medik8s.io/drain` (**NoSchedule**) |

---

## 1. Admission rejects create

**Symptom:** API **Forbidden** / webhook error on create.

**Common causes:**

- **Node does not exist** — `invalid nodeName, no node with name %s found`
- **Duplicate NodeMaintenance** — another CR already uses that **`nodeName`**
- **OpenShift control plane** — etcd disruption **not** allowed (`ErrorControlPlaneQuorumViolation`)
- **Transient API** — `could not get node ... please try again`

**Ops:** Verify **`kubectl get node`**, list **`nm`** / **`nodemaintenance`**, check **etcd-guard** PDB namespace on OpenShift.

---

## 2. Admission rejects update

**Symptom:** Cannot change **`spec.nodeName`**.

**Behaviour:** Immutable by design (`ErrorNodeNameUpdateForbidden`).

**Ops:** Delete and recreate CR, or create a second CR is **not** allowed for the same node—use one CR per node lifecycle.

---

## 3. Phase **Failed** — node not found

**Symptom:** **FailedMaintenance** event; node name typo or node removed.

**Behaviour:** **NotFound** during reconcile after admission may still happen if node deleted later; **`onReconcileError`** stops requeue for expected not-found string.

**Ops:** Fix **nodeName** (requires new CR after delete) or restore node.

---

## 4. Lease **AlreadyHeld** before drain

**Symptom:** Reconcile errors; logs **lease is held by another entity**; **`drainProgress`** still 0.

**Behaviour:** Returns error from **`RequestLease`** without incrementing **`errorOnLeaseCount`** in that branch (message text in code). Repeated reconcile depends on other actor releasing lease.

**Ops:** Identify other lease holder (e.g. another operator); resolve conflict or wait.

---

## 5. Lease lost / not extended during drain

**Symptom:** **`errorOnLeaseCount`** increases; eventually **Failed** with **LastError** about lease extension.

**Behaviour:** When **`drainProgress` > 0** and **`AlreadyHeldError`**, count increments; above threshold → **`stopNodeMaintenanceImp`**, **Failed**.

**Ops:** Investigate lease contention and API errors; reduce concurrent maintenance or fix RBAC on **leases**.

---

## 6. Drain stuck or slow

**Symptom:** **Running**, **`lastError`** populated, **5s** requeues; **pendingPods** non-empty; **drainProgress** partial.

**Behaviour:** PDBs, pod termination grace, or eviction API failures; **30s** drain timeout may surface as repeated attempts.

**Ops:** **`kubectl get pods -A --field-selector spec.nodeName=...`**, PDBs, forbidden evictions, finalizers.

---

## 7. Node left cordoned / tainted after human delete of CR

**Symptom:** Finalizer blocked or operator down during delete.

**Behaviour:** Normal path runs **uncordon** + taint removal in finalizer. If CR stuck **Terminating**, check operator logs and RBAC.

**Ops:** Ensure operator runs; **`kubectl describe nodemaintenance`** / **`nm`**; manual uncordon only if needed after understanding state.

---

## 8. **InvalidateLease** during cleanup — **AlreadyHeld**

**Symptom:** Logs: lease held by another entity, **skipping invalidation**.

**Behaviour:** **Intentional** — NMO does not steal another holder’s lease; still removes taints / uncordons / label when stopping maintenance.

**Ops:** Confirm the remaining lease owner’s expectations.

---

## Related pieces

- **`architecture.md`** — ordering and constants.
- **`runbook.md`** — commands.
