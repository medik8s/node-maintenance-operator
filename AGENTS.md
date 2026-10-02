# AGENTS.md — Node Maintenance Operator

## Medik8s context

NMO is a standalone operator in the [medik8s](https://medik8s.io) family. Unlike the remediation operators (SNR, MDR, FAR, SBR), NMO is **not a remediator** — it does not respond to NHC. It is a declarative cordon/drain tool: admins create a `NodeMaintenance` CR to take a node out of service for planned maintenance (upgrades, hardware work, etc.).

NMO was previously developed under [KubeVirt](https://github.com/kubevirt/node-maintenance-operator); this repository is the current version. It coordinates with NHC via Leases to avoid conflicting with automated remediation.

## What NMO does

- **CR created** → cordons the node (marks unschedulable) and drains it (evicts all evictable pods). Mirrors `kubectl drain <node>`.
- **CR deleted** → uncordons the node (marks schedulable again).

A `NodeMaintenance` CR is created by the admin (not by NHC). Its lifecycle is fully manual.

## Repository layout

```
cmd/main.go             Operator entrypoint
api/v1beta1/            NodeMaintenance CRD types (nodemaintenance_types.go; v1beta1 — note: older API version)
internal/controller/    NMO reconciler + taint/utils helpers
internal/webhook/       Admission webhook logic
pkg/                    Shared packages
test/e2e/               Ginkgo e2e suite
must-gather/            must-gather scripts for OCP support diagnostics
hack/                   Dev scripts
config/                 Kustomize bases (operator, rbac, bundle)
bundle/                 Generated OLM bundle (manifests, metadata, tests)
version/                Operator version package
```

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

## Local development & deployment

Deploying to a dev cluster is standardized across all medik8s operators via the
shared dev environment in [`medik8s/tools`](https://github.com/medik8s/tools)
(`dev/dev.mk`). The Makefile pulls these targets in automatically: it uses a
sibling `../tools` checkout if present, otherwise shallow-clones the repo into
`.tools/` on first `make dev-*` use.

```bash
make dev-setup       # Create a Kind cluster (1 control-plane + 3 workers) with deps
make dev-deploy      # Build image, load it, install CRDs, deploy the operator
make dev-describe    # Summarize nodes, pods, CRs, leases, and events
make dev-redeploy    # Rebuild and restart pods (fast iteration)
make dev-undeploy    # Remove the operator
make dev-teardown    # Destroy the Kind cluster
make dev-help        # List all dev-* targets
```

Deploy to an existing cluster (OCP, etc.) with `SKIP_KIND=true`; images are
pushed to the ephemeral `ttl.sh` registry:

```bash
export KUBECONFIG=~/.kube/my-cluster
SKIP_KIND=true make dev-setup dev-deploy
```

Exercise maintenance by creating a `NodeMaintenance` CR (cordons and drains the
node; deleting it uncordons); NMO is fully exercisable on Kind. See
[`dev/README.md`](https://github.com/medik8s/tools/blob/main/dev/README.md) in
`medik8s/tools` for prerequisites, all targets, and per-operator coverage.

## Code style

- Go, Kubebuilder v4, controller-runtime; follows standard medik8s patterns.
- API version is `v1beta1` (not `v1alpha1`) — do not assume it will be promoted without a migration plan.
- Imports must be sorted (`make fix-imports`).
- No direct commits to `main`; open a PR.

## Key design constraints

- NMO is **not a remediator** — do not wire it into NHC remediation templates.
- The operator mimics `kubectl drain` behavior; drain logic edge cases (PodDisruptionBudgets, DaemonSet pods, local storage) apply here as in `kubectl`.
- NMO coordinates with NHC via Leases so maintenance drains and automated remediation don't conflict. Do not remove this coordination.
- `NodeMaintenance` API is `v1beta1` — breaking changes require a migration and deprecation notice.

## Security

- Operator requires cluster-scoped RBAC to cordon nodes and evict pods.
- No privileged pods; runs with standard controller-manager permissions.
- Never widen RBAC beyond the generated `config/rbac/` manifests without review.

## Keeping the docs current

If your changes affect anything described here — build commands, repo layout, drain behavior, API version, NHC coordination — or any other existing documentation (`README.md`, `CONTRIBUTING.md`, anything under `docs/`, inline command or usage references), update all of it so the docs never drift from the code.

## Commit conventions

- One-line commit message; sign off with `-s`.
- Reference the relevant issue or PR number when applicable.
- Use WIP in title when creating draft PRs to save CI resources.
