# AGENTS.md — Node Maintenance Operator

> **IMPORTANT — read this first.** Before making any changes in this repository, you MUST
> read the medik8s **common agent guide**, the **OFFICIAL guidance** for all medik8s
> operators: **https://github.com/medik8s/.github/blob/main/AGENTS.md** . It is
> authoritative project guidance and must not be ignored.

## Medik8s context

NMO is a standalone operator in the [medik8s](https://medik8s.io) family. Unlike the remediation operators (SNR, MDR, FAR, SBR), NMO is **not a remediator** — it does not respond to NHC. It is a declarative cordon/drain tool: admins create a `NodeMaintenance` CR to take a node out of service for planned maintenance (upgrades, hardware work, etc.).

NMO was previously developed under [KubeVirt](https://github.com/kubevirt/node-maintenance-operator); this repository is the current version. It coordinates with NHC via Leases to avoid conflicting with automated remediation.

## What NMO does

- **CR created** → cordons the node (marks unschedulable) and drains it (evicts all evictable pods). Mirrors `kubectl drain <node>`.
- **CR deleted** → uncordons the node (marks schedulable again).

A `NodeMaintenance` CR is created by the admin (not by NHC). Its lifecycle is fully manual.

## Build & test

```bash
# Unit tests (also runs go-verify, manifests, generate, fmt, vet, imports)
make test-no-verify

# Unit tests + verify no uncommitted changes
make test

# Build operator binary
make build

# Build container image
make docker-build

# Regenerate CRDs + RBAC after API changes
make manifests generate

# Format + imports
make fmt vet
make fix-imports

# e2e tests (requires a running cluster)
make cluster-functest
```

> `make test` includes `verify-unchanged` — run it before opening a PR.

## Local development & testing

Follow the shared workflow in the
[common agent guide](https://github.com/medik8s/.github/blob/main/AGENTS.md) — it documents
the standardized `dev-*` make targets (`dev-setup`, `dev-deploy`, `dev-redeploy`,
`dev-undeploy`, `dev-describe`, `dev-help`, …) provided by `medik8s/tools` (`dev/dev.mk`).

**To develop and test against a real OpenShift / Kubernetes cluster**:
`export SKIP_KIND=true` before the `dev-*` targets; images are pushed to `ttl.sh`.

Github CI workflow runs on a Kind cluster. NMO is **fully exercisable on any cluster,
including Kind**: create a `NodeMaintenance` CR to cordon and drain a node, and delete it
to uncordon.

## Key design constraints

- NMO is **not a remediator** — do not wire it into NHC remediation templates.
- The operator mimics `kubectl drain` behavior; drain logic edge cases (PodDisruptionBudgets, DaemonSet pods, local storage) apply here as in `kubectl`.
- NMO coordinates with NHC via Leases so maintenance drains and automated remediation don't conflict. Do not remove this coordination.
- `NodeMaintenance` API is `v1beta1` — breaking changes require a migration and deprecation notice.

## Security

- Operator requires cluster-scoped RBAC to cordon nodes and evict pods.
- No privileged pods; runs with standard controller-manager permissions.
