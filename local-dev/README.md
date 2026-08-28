# Local Development Setup — service-provider-kyverno

## Overview

This document describes how to set up a local development environment for the `service-provider-kyverno` project. The setup runs entirely on your Mac using Docker Desktop and [kind](https://kind.sigs.k8s.io/), with no devcontainer required.

---

## Architecture

The local environment mirrors the production OpenControlPlane topology using three kind clusters running as sibling containers inside Docker Desktop's Linux VM:

```
Mac Host (Apple Silicon / Intel)
└── Docker Desktop Linux VM
    ├── kind: platform            ← hosts the openMCP operator and cluster-provider-kind
    ├── kind: onboarding.*        ← created automatically by the operator; users submit KyvernoService CRs here
    └── kind: mcp-*               ← created on demand per ManagedControlPlane; Kyverno is installed here
```

All three clusters communicate over the `kind` Docker bridge network. The platform cluster has the Mac's Docker socket mounted at `/var/run/host-docker.sock` so that `cluster-provider-kind` can spin up sibling kind clusters.

### Component responsibilities

| Component | Runs on | Purpose |
|-----------|---------|---------|
| `openmcp-operator` | platform | Watches `Cluster`, `ClusterProvider`, `ServiceProvider` CRs; orchestrates the platform |
| `cluster-provider-kind` | platform | Receives `ClusterRequest` CRs; creates/deletes sibling kind clusters via Docker |
| `service-provider-kyverno` (your controller) | local Mac (`make run`) | Watches `KyvernoService` CRs on onboarding cluster; installs Kyverno on `ControlPlane` clusters |
| Kyverno | mcp-* | The actual policy engine installed by the SP |

### Inner dev loop

The service provider runs as a pod on the platform cluster, deployed by the openmcp-operator when a `ServiceProvider` CR is created. There is no `make run` — the SP must be built as an image and loaded into the cluster.

The iteration cycle is:

```
edit code
    ↓
task build:img:build-test        # builds + tags image for local arch
    ↓
./dev.sh setup-sp                # loads image into kind, redeploys SP
    ↓
apply KyvernoService CR to onboarding cluster and observe
```

### `ControlPlane` cluster lifecycle

The `ControlPlane` cluster does not exist at rest — it is created on demand:

```
kubectl apply -f config/samples/kyvernoservice.yaml  (to onboarding cluster)
        ↓
service-provider-kyverno (running on platform cluster) sees the CR
        ↓
requests a ManagedControlPlane from the platform cluster
        ↓
cluster-provider-kind creates a new sibling kind cluster (mcp-*)
        ↓
SP installs Kyverno onto the mcp-* cluster
```

At rest you will always have exactly two clusters:
- `platform` — hosts the operator, cluster-provider-kind, and your SP
- `onboarding.*` — where users submit `KyvernoService` CRs

A third `mcp-*` cluster appears when a `KyvernoService` is created and is deleted when it is removed.

---

## Prerequisites

Install the following on your Mac before running `dev.sh`:

| Tool | Install |
|------|---------|
| Docker Desktop | https://www.docker.com/products/docker-desktop/ |
| kind | `brew install kind` |
| kubectl | `brew install kubectl` |
| Go | `brew install go` or [mise](https://mise.jdx.dev/) |

> **Apple Silicon note:** The setup includes workarounds for a known kind bootstrap issue on Apple Silicon where the API server initialises too slowly for kind's internal taint-removal step. `dev.sh` handles this automatically via `fix_kind_bootstrap`.

---

## Quickstart

```bash
# 1. Spin up the full local environment
./dev.sh deploy

# 2. Build and deploy the SP image
task build:img:build-test
./dev.sh setup-sp

# 3. Apply a KyvernoService CR to the onboarding cluster
kubectl config use-context kind-<onboarding-cluster-name>
kubectl apply -f config/samples/kyvernoservice.yaml

# 4. Watch the ControlPlane cluster get created and Kyverno installed
kind get clusters
kubectl get pods -A --context kind-<mcp-cluster-name>
```

---

## dev.sh reference

```
./dev.sh deploy          spin up platform + onboarding clusters, deploy operator
./dev.sh reset           delete all kind clusters (prompts for confirmation)
./dev.sh reset --force   delete all kind clusters without prompting
./dev.sh setup-sp        load SP image into kind and (re)deploy — run after every image build
./dev.sh status          show cluster and pod health
```

### Iterating on SP code

```bash
task build:img:build-test && ./dev.sh setup-sp
```

`setup-sp` deletes the existing `ServiceProvider` CR and init job so the operator redeploys cleanly with the new image.

---

## What deploy does

`./dev.sh deploy` runs the following steps in order:

1. **Preflight checks** — verifies Docker, kind, kubectl, Go are installed and Docker Desktop is running
2. **setup_kind_cluster** — creates the `platform` kind cluster with the Docker socket mounted; on Apple Silicon runs `fix_kind_bootstrap` to manually apply CNI (kindnet) and storage class since kind's bootstrap step may time out
3. **pull_and_load_images** — pulls `openmcp-operator` and `cluster-provider-kind` images for the correct arch and loads them into the kind node via `docker cp` + `ctr import` (avoids multi-arch manifest issues on Apple Silicon)
4. **setup_openmcp** — deploys namespace, RBAC, ConfigMap, and the `openmcp-operator` Deployment; waits for it to become available
5. **setup_cluster_provider** — creates the `ClusterProvider/kind` CR (tells the operator how to create kind clusters) and the `Cluster/platform` CR
6. **wait_for_onboarding_cluster** — waits for the operator to provision the onboarding cluster and exports its kubeconfig

---

## Apple Silicon specifics

## Networking

All kind clusters are attached to the `kind` Docker bridge network (created by `dev.sh` if it doesn't exist). The platform cluster API server listens on `0.0.0.0:6443` inside its container, and kind maps this to `127.0.0.1:6443` on the Mac host. The kubeconfig is patched to use `127.0.0.1:6443` with TLS verification disabled (the cert SANs don't include `127.0.0.1`).

The `cluster-provider-kind` pod on the platform cluster creates onboarding and `ControlPlane` clusters by talking to Docker via `/var/run/docker.sock`, which is mapped from the Mac host's Docker socket at `/var/run/host-docker.sock` inside the kind node.

---

## Troubleshooting

### Cluster stuck, everything broken
```bash
./dev.sh reset --force
./dev.sh deploy
```

### openmcp-operator pod Pending (taint issue)
```bash
kubectl taint nodes platform-control-plane node-role.kubernetes.io/control-plane- 2>/dev/null || true
kubectl taint nodes platform-control-plane node.kubernetes.io/not-ready- 2>/dev/null || true
```

### cp-kind-init job failing with "hostPath type check failed: not a socket file"
The `ClusterProvider` is using the wrong Docker socket path. Patch it:
```bash
kubectl patch clusterprovider kind --type=merge -p \
  '{"spec":{"extraVolumes":[{"name":"docker-socket","hostPath":{"path":"/var/run/host-docker.sock","type":"Socket"}}]}}'
kubectl delete job cp-kind-init -n openmcp-system
```

### kindnet not initialising (node stays NotReady)
```bash
docker exec platform-control-plane bash -c \
  "sed 's|{{ .PodSubnet }}|10.244.0.0/16|g' /kind/manifests/default-cni.yaml | \
   kubectl --kubeconfig=/etc/kubernetes/admin.conf apply --validate=false -f -"
```

### Check overall health
```bash
./dev.sh status
kubectl get pods -A
kind get clusters
```