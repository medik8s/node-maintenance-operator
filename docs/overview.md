# Node Maintenance Operator (NMO) — Overview

## Purpose

The **Node Maintenance Operator (NMO)** puts a **single node** into **maintenance**: it **leases** the node (so other controllers can coordinate), **taints** and **cordons** it, **evicts** workload pods (via **`kubectl` drain** semantics), and sets **`remediation.medik8s.io/exclude-from-remediation`** on the node so **health-based remediations** (for example NHC) do not fight the same node during planned work.

NMO is **not** a generic “cluster upgrade” tool; it reconciles one **cluster-scoped** **`NodeMaintenance`** CR per **target node** at a time (enforced at admission).

## How it works

1. You create a **`NodeMaintenance`** with **`spec.nodeName`** (and optional **`reason`**). Admission checks the node exists, that **no other** **`NodeMaintenance`** already targets that node, and (on **OpenShift**) that putting a **control-plane** node into maintenance would not violate **etcd** disruption rules.
2. The reconciler adds a **finalizer**, records **BeginMaintenance**, initializes **status** (pod counts, **Running** phase), then:
   - Requests a **coordination lease** held as identity **`node-maintenance`** (duration **1 hour** in code).
   - Patches the **exclude-from-remediation** label on the node.
   - Applies maintenance **taints** (**`node.kubernetes.io/unschedulable`** and **`medik8s.io/drain`**, **NoSchedule**) and **cordons** the node.
   - Runs **node drain** with **force**, **delete emptyDir data**, **ignore DaemonSets**, and a **30s** per-drain timeout (see **architecture** for details).
3. When drain completes, **phase** becomes **Succeeded** and an event records success.
4. When you **delete** the CR, the finalizer **uncordons**, removes taints and the exclude label, **invalidates** the lease (or skips if the lease is held by someone else), and removes the finalizer.

## Important details

- **API:** **`nodemaintenance.medik8s.io/v1beta1`**, kind **`NodeMaintenance`**, short **`nm`**, **cluster** scope.
- **`spec.nodeName`** cannot be changed on **update** (webhook).
- **KubeVirt / VMI:** drain is configured to allow evicting pods that are not owned by standard workload controllers (**`Force`**) and to delete **emptyDir** data—aligned with **VirtualMachineInstance** workloads in comments in code.
- **Lease conflicts:** If another actor already holds the node lease (**`AlreadyHeldError`**), behaviour differs between “before drain starts” and “during drain”; repeated failures can mark maintenance **Failed** and **uncordon**.

## Related pieces

| Piece | Role |
|--------|------|
| **NHC / other lease users** | May hold the same lease identity pattern; **AlreadyHeld** paths affect whether NMO can proceed or must clean up. |
| **OpenShift etcd guard PDB** | Webhook uses **etcd** disruption checks for **control-plane** nodes. |
| **`k8s.io/kubectl/pkg/drain`** | Implements cordon + eviction behaviour. |

## What this file is not

This overview does **not** spell out every **status** field, webhook error string, or lease counter threshold. See **architecture**, **failure_modes**, and **runbook**.
