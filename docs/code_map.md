# Node Maintenance Operator (NMO) — Code map

**Repository:** `github.com/medik8s/node-maintenance-operator`

## Purpose

Reader’s map from verified memory into the repo (not a full tour).

## Layout (first-party)

```
node-maintenance-operator/
├── cmd/
│   └── main.go                          # Manager, webhook server, lease initializer, OpenShift probe
├── api/v1beta1/
│   ├── nodemaintenance_types.go     # CRD types, phases, finalizer constant
│   ├── groupversion_info.go
│   └── zz_generated.deepcopy.go
├── internal/
│   ├── controller/
│   │   ├── nodemaintenance_controller.go # Reconcile, drainer, lease, status
│   │   ├── taint.go                     # Maintenance taints JSON patch
│   │   ├── utils.go                     # Controller-local helpers (string/pod-list helpers)
│   │   └── *_test.go
│   └── webhook/v1beta1/
│       └── nodemaintenance_webhook.go   # Validating webhook (create/update/delete)
├── pkg/
│   └── utils/                       # Events (events.go), OpenShift validator (validation.go)
├── version/
└── config/                          # Manifests, bundle (OLM)
```

## Where to look

| Question | Start here |
|----------|------------|
| End-to-end **Reconcile** | `internal/controller/nodemaintenance_controller.go` |
| **Taints** keys/effects | `internal/controller/taint.go` |
| **Webhook** / etcd quorum | `internal/webhook/v1beta1/nodemaintenance_webhook.go` |
| **CRD** fields & short name **`nm`** | `api/v1beta1/nodemaintenance_types.go` |
| **Events** reasons/messages | `pkg/utils/events.go` |
| **OpenShift** detection | `pkg/utils/validation.go` (`NewOpenshiftValidator`) |
| **Process flags** / TLS / leader ID / secured metrics | `cmd/main.go` |
| **Version** logging | `version/version.go` |

## API short name

| Resource | Short name |
|----------|------------|
| `NodeMaintenance` | `nm` |

## Related pieces

- **`../ARCHITECTURE.md`**
- **`runbook.md`**

## Scope

Does not enumerate every **bundle** RBAC rule or CSV env; use **`config/`** / shipped CSV for deployment truth.
