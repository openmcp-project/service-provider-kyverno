#!/bin/bash
set -e -o pipefail

# =============================================================================
# dev.sh — local dev environment for service-provider-kyverno
# Runs directly on Mac. Requires: Docker Desktop, kind, kubectl, Go
# Usage:
#   ./dev.sh deploy       — spin up full local environment
#   ./dev.sh reset        — delete all kind clusters
#   ./dev.sh reset --force
#   ./dev.sh setup-sp     — (re)install Kyverno SP after building image
#   ./dev.sh status       — show cluster and pod status
# =============================================================================

OPENMCP_OPERATOR_VERSION=${OPENMCP_OPERATOR_VERSION:-v0.17.1}
OPENMCP_OPERATOR_IMAGE=${OPENMCP_OPERATOR_IMAGE:-ghcr.io/openmcp-project/images/openmcp-operator:${OPENMCP_OPERATOR_VERSION}}
OPENMCP_ENVIRONMENT=${OPENMCP_ENVIRONMENT:-debug}
OPENMCP_CP_KIND_IMAGE=${OPENMCP_CP_KIND_IMAGE:-ghcr.io/openmcp-project/images/cluster-provider-kind:v0.0.15}
KYVERNO_SP_IMAGE=${KYVERNO_SP_IMAGE:-ghcr.io/openmcp-project/images/service-provider-kyverno:0.0.1}

PLATFORM_CLUSTER="platform"
PLATFORM_NODE="platform-control-plane"
NODE_IMAGE="kindest/node:v1.31.2"

log_info()    { echo "[INFO] $*"; }
log_step()    { echo ""; echo "══════════════════════════════════════════════════════════"; echo "[STEP] $*"; echo "══════════════════════════════════════════════════════════"; }
log_ok()      { echo "[OK]   $*"; }
log_error()   { echo "[ERR]  $*" >&2; }

# =============================================================================
# Preflight checks
# =============================================================================
check_prerequisites() {
  log_step "Checking prerequisites"
  local missing=0

  for cmd in docker kind kubectl go; do
    if command -v "$cmd" &>/dev/null; then
      log_ok "$cmd found: $(command -v $cmd)"
    else
      log_error "$cmd not found. Install instructions:"
      case "$cmd" in
        docker)  echo "       https://www.docker.com/products/docker-desktop/" ;;
        kind)    echo "       brew install kind" ;;
        kubectl) echo "       brew install kubectl" ;;
        go)      echo "       brew install go  OR  https://mise.jdx.dev/" ;;
      esac
      missing=$((missing + 1))
    fi
  done

  if ! docker info &>/dev/null; then
    log_error "Docker Desktop is not running. Please start it."
    missing=$((missing + 1))
  fi

  if [ "$missing" -gt 0 ]; then
    echo ""
    log_error "$missing prerequisite(s) missing. Aborting."
    exit 1
  fi

  log_ok "All prerequisites satisfied"
}

# =============================================================================
# kind cluster setup
# =============================================================================
ensure_kind_network() {
  docker network inspect kind &>/dev/null || docker network create kind
}

setup_kind_cluster() {
  log_step "Setting up kind cluster: ${PLATFORM_CLUSTER}"

  ensure_kind_network

  if kind get clusters 2>/dev/null | grep -q "^${PLATFORM_CLUSTER}$"; then
    log_info "Cluster '${PLATFORM_CLUSTER}' already exists, skipping creation"
  else
    log_info "Creating cluster '${PLATFORM_CLUSTER}'..."
    # --retain keeps node container on bootstrap failure so we can fix CNI manually
    kind create cluster --name "${PLATFORM_CLUSTER}" --retain --config - <<EOF || true
apiVersion: kind.x-k8s.io/v1alpha4
kind: Cluster
networking:
  apiServerAddress: "0.0.0.0"
  apiServerPort: 6443
nodes:
- role: control-plane
  image: ${NODE_IMAGE}
  extraMounts:
  - hostPath: /var/run/docker.sock
    containerPath: /var/run/host-docker.sock
EOF

    if ! docker inspect "${PLATFORM_NODE}" &>/dev/null; then
      log_error "Node container '${PLATFORM_NODE}' not found after create. Aborting."
      exit 1
    fi

    fix_kind_bootstrap
  fi

  export_and_patch_kubeconfig
}

fix_kind_bootstrap() {
  # kind's bootstrap can fail on Apple Silicon because it tries to remove the
  # control-plane taint before the API server finishes initialising.
  # We wait for full API readiness, then manually apply CNI + storage.
  local kc="/etc/kubernetes/admin.conf"

  log_info "Waiting for API server to be fully ready..."
  until docker exec "${PLATFORM_NODE}" kubectl --kubeconfig="${kc}" \
    api-resources &>/dev/null; do
    sleep 2
  done

  log_info "Removing control-plane taint..."
  docker exec "${PLATFORM_NODE}" kubectl --kubeconfig="${kc}" \
    taint nodes --all node-role.kubernetes.io/control-plane- 2>/dev/null || true

  log_info "Applying CNI (kindnet)..."
  # default-cni.yaml contains an unrendered Go template — substitute pod CIDR
  docker exec "${PLATFORM_NODE}" bash -c \
    "sed 's|{{ .PodSubnet }}|10.244.0.0/16|g' /kind/manifests/default-cni.yaml | \
     kubectl --kubeconfig=${kc} apply --validate=false -f -"

  log_info "Applying default storage class..."
  docker exec "${PLATFORM_NODE}" bash -c \
    "kubectl --kubeconfig=${kc} apply --validate=false -f /kind/manifests/default-storage.yaml"

  log_info "Waiting for node to become Ready..."
  until docker exec "${PLATFORM_NODE}" kubectl --kubeconfig="${kc}" \
    get nodes 2>/dev/null | grep -q " Ready"; do
    sleep 3
  done

  # Lifecycle controller may re-add the taint once node is Ready — remove again
  docker exec "${PLATFORM_NODE}" kubectl --kubeconfig="${kc}" \
    taint nodes --all node-role.kubernetes.io/control-plane- 2>/dev/null || true

  log_ok "kind bootstrap complete"
}

export_and_patch_kubeconfig() {
   kind export kubeconfig --name "${PLATFORM_CLUSTER}"
  # kind maps the API server port to localhost on the Mac host
  # the internal Docker IP is not reachable from the Mac directly
  kubectl config set-cluster "kind-${PLATFORM_CLUSTER}" \
    --server="https://127.0.0.1:6443" \
    --insecure-skip-tls-verify=true
  log_ok "kubeconfig set to https://127.0.0.1:6443"
}

# =============================================================================
# Image loading
# =============================================================================
load_image() {
  local image=$1
  local node=$2
  local arch
  arch=$(go env GOARCH)

  log_info "Pulling ${image} (linux/${arch})..."
  docker pull --platform="linux/${arch}" "${image}"

  log_info "Loading ${image} into kind node..."
  local tmpfile
  tmpfile=$(mktemp /tmp/kindimage-XXXXXX.tar)
  docker save "${image}" -o "${tmpfile}"
  docker cp "${tmpfile}" "${node}:/root/kindimage.tar"
  docker exec "${node}" ctr --namespace=k8s.io images import \
    --snapshotter=overlayfs /root/kindimage.tar
  docker exec "${node}" rm -f /root/kindimage.tar
  rm -f "${tmpfile}"
  log_ok "Loaded ${image}"
}

load_local_image() {
  # Like load_image but skips the docker pull — for locally built images
  local image=$1
  local node=$2

  log_info "Loading local image ${image} into kind node..."
  local tmpfile
  tmpfile=$(mktemp /tmp/kindimage-XXXXXX.tar)
  docker save "${image}" -o "${tmpfile}"
  docker cp "${tmpfile}" "${node}:/root/kindimage.tar"
  docker exec "${node}" ctr --namespace=k8s.io images import \
    --snapshotter=overlayfs /root/kindimage.tar
  docker exec "${node}" rm -f /root/kindimage.tar
  rm -f "${tmpfile}"
  log_ok "Loaded ${image}"
}

pull_and_load_images() {
  log_step "Pulling and loading images into kind"
  load_image "${OPENMCP_OPERATOR_IMAGE}" "${PLATFORM_NODE}"
  load_image "${OPENMCP_CP_KIND_IMAGE}"  "${PLATFORM_NODE}"
}

# =============================================================================
# OpenMCP operator
# =============================================================================
setup_openmcp() {
  log_step "Deploying OpenMCP operator"

  kubectl apply -f - <<EOF
apiVersion: v1
kind: Namespace
metadata:
  name: openmcp-system
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: openmcp-operator
  namespace: openmcp-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: openmcp-operator
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin
subjects:
- kind: ServiceAccount
  name: openmcp-operator
  namespace: openmcp-system
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: openmcp-operator
  namespace: openmcp-system
data:
  config: |
    managedControlPlane:
      mcpClusterPurpose: mcp
    scheduler:
      scope: Cluster
      purposeMappings:
        mcp:
          template:
            spec:
              profile: kind
              tenancy: Exclusive
        platform:
          template:
            spec:
              profile: kind
              tenancy: Shared
        onboarding:
          template:
            spec:
              profile: kind
              tenancy: Shared
EOF

  kubectl apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: openmcp-operator
  namespace: openmcp-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: openmcp-operator
  template:
    metadata:
      labels:
        app: openmcp-operator
    spec:
      tolerations:
      - key: node-role.kubernetes.io/control-plane
        operator: Exists
        effect: NoSchedule
      - key: node.kubernetes.io/not-ready
        operator: Exists
        effect: NoSchedule
      serviceAccountName: openmcp-operator
      initContainers:
      - image: ${OPENMCP_OPERATOR_IMAGE}
        name: openmcp-operator-init
        imagePullPolicy: IfNotPresent
        args: [init, --environment, ${OPENMCP_ENVIRONMENT}, --config, /etc/openmcp-operator/config]
        env:
        - name: POD_NAME
          valueFrom: { fieldRef: { fieldPath: metadata.name } }
        - name: POD_NAMESPACE
          valueFrom: { fieldRef: { fieldPath: metadata.namespace } }
        - name: POD_IP
          valueFrom: { fieldRef: { fieldPath: status.podIP } }
        - name: POD_SERVICE_ACCOUNT_NAME
          valueFrom: { fieldRef: { fieldPath: spec.serviceAccountName } }
        volumeMounts:
        - name: config
          mountPath: /etc/openmcp-operator
          readOnly: true
      containers:
      - image: ${OPENMCP_OPERATOR_IMAGE}
        name: openmcp-operator
        imagePullPolicy: IfNotPresent
        args: [run, --environment, ${OPENMCP_ENVIRONMENT}, --config, /etc/openmcp-operator/config]
        env:
        - name: POD_NAME
          valueFrom: { fieldRef: { fieldPath: metadata.name } }
        - name: POD_NAMESPACE
          valueFrom: { fieldRef: { fieldPath: metadata.namespace } }
        - name: POD_IP
          valueFrom: { fieldRef: { fieldPath: status.podIP } }
        - name: POD_SERVICE_ACCOUNT_NAME
          valueFrom: { fieldRef: { fieldPath: spec.serviceAccountName } }
        volumeMounts:
        - name: config
          mountPath: /etc/openmcp-operator
          readOnly: true
      volumes:
      - name: config
        configMap:
          name: openmcp-operator
EOF

  log_info "Waiting for openmcp-operator to be available..."
  kubectl wait --for=condition=available deployment/openmcp-operator \
    -n openmcp-system --timeout=120s
  log_ok "openmcp-operator is running"
}

# =============================================================================
# Cluster provider
# =============================================================================
setup_cluster_provider() {
  log_step "Installing ClusterProvider for kind"

  kubectl wait --for=create \
    customresourcedefinitions.apiextensions.k8s.io/clusterproviders.openmcp.cloud \
    --timeout=60s

  kubectl apply -f - <<EOF
apiVersion: openmcp.cloud/v1alpha1
kind: ClusterProvider
metadata:
  name: kind
spec:
  image: ${OPENMCP_CP_KIND_IMAGE}
  extraVolumes:
  - name: docker-socket
    hostPath:
      path: /var/run/host-docker.sock
      type: Socket
  extraVolumeMounts:
  - name: docker-socket
    mountPath: /var/run/docker.sock
EOF

  kubectl apply -f - <<EOF
apiVersion: clusters.openmcp.cloud/v1alpha1
kind: Cluster
metadata:
  annotations:
    kind.clusters.openmcp.cloud/name: platform
  name: platform
  namespace: openmcp-system
spec:
  kubernetes: {}
  profile: kind
  purposes:
  - platform
  tenancy: Shared
EOF

  log_ok "ClusterProvider installed"
}

# =============================================================================
# Kyverno SP (optional — only when image is built)
# =============================================================================
setup_kyverno_sp() {
  log_step "Installing/updating Kyverno service provider"

  if ! docker image inspect "${KYVERNO_SP_IMAGE}" &>/dev/null; then
    log_error "Kyverno SP image '${KYVERNO_SP_IMAGE}' not found locally."
    log_error "Build it first with: task build:img:build-test"
    exit 1
  fi

  load_local_image "${KYVERNO_SP_IMAGE}" "${PLATFORM_NODE}"

  # Clean up existing SP so operator recreates with new image
  kubectl delete serviceprovider kyverno 2>/dev/null || true
  kubectl delete job sp-kyverno-init -n openmcp-system 2>/dev/null || true
  # Wait for cleanup
  until ! kubectl get serviceprovider kyverno &>/dev/null; do sleep 2; done

  kubectl wait --for=create \
    customresourcedefinitions.apiextensions.k8s.io/serviceproviders.openmcp.cloud \
    --timeout=60s

  kubectl apply -f - <<EOF
apiVersion: openmcp.cloud/v1alpha1
kind: ServiceProvider
metadata:
  name: kyverno
spec:
  image: ${KYVERNO_SP_IMAGE}
EOF

  kubectl wait --for=create -n openmcp-system job/sp-kyverno-init --timeout=120s
  kubectl wait --for=condition=complete -n openmcp-system job/sp-kyverno-init --timeout=120s
  log_ok "Kyverno SP installed"

  # Re-export onboarding kubeconfig in case it wasn't set
  local onboarding_cluster
  onboarding_cluster=$(kind get clusters | grep onboarding | head -1)
  kind export kubeconfig --name "${onboarding_cluster}"
  log_ok "Onboarding kubeconfig exported: kind-${onboarding_cluster}"
}

# =============================================================================
# Onboarding cluster
# =============================================================================
wait_for_onboarding_cluster() {
  log_step "Waiting for onboarding cluster"

  kubectl wait --for=create -n openmcp-system cluster/onboarding --timeout=120s
  kubectl wait \
    --for='jsonpath={.status.conditions[?(@.type=="Ready")].status}=True' \
    -n openmcp-system cluster/onboarding --timeout=180s

  local onboarding_cluster
  onboarding_cluster=$(kind get clusters | grep onboarding | head -1)
  kind export kubeconfig --name "${onboarding_cluster}"
  log_ok "Onboarding cluster ready: ${onboarding_cluster}"
}

# =============================================================================
# Status
# =============================================================================
status() {
  echo ""
  echo "── kind clusters ────────────────────────────────────────"
  kind get clusters 2>/dev/null || echo "(none)"
  echo ""
  echo "── platform nodes ───────────────────────────────────────"
  kubectl get nodes 2>/dev/null || echo "(unavailable)"
  echo ""
  echo "── openmcp-system pods ──────────────────────────────────"
  kubectl get pods -n openmcp-system 2>/dev/null || echo "(unavailable)"
  echo ""
  echo "── kube-system pods ─────────────────────────────────────"
  kubectl get pods -n kube-system 2>/dev/null || echo "(unavailable)"
}

# =============================================================================
# Reset
# =============================================================================
reset_clusters() {
  if [[ "$1" != "--force" ]]; then
    read -rp "Delete ALL kind clusters? (yes/no): " confirm
    [[ "$confirm" != "yes" ]] && { log_info "Cancelled."; exit 0; }
  fi
  kind delete clusters --all
  log_ok "All clusters deleted"
}

# =============================================================================
# Deploy (full setup)
# =============================================================================
deploy() {
  check_prerequisites
  setup_kind_cluster
  pull_and_load_images
  setup_openmcp
  setup_cluster_provider

  if docker image inspect "${KYVERNO_SP_IMAGE}" &>/dev/null; then
    setup_kyverno_sp
  else
    log_info "Kyverno SP image not found — skipping in-cluster install."
    log_info "For local development, run your controller with: make run"
    log_info "To install in-cluster later: task build:img:build && ./dev.sh setup-sp"
  fi

  wait_for_onboarding_cluster

  log_step "Dev environment ready"
  echo ""
  echo "  Platform cluster:   kubectl config use-context kind-platform"
  echo "  Onboarding cluster: kubectl config use-context kind-<onboarding-name>"
  echo "  Run controller:     make run"
  echo "  Status:             ./dev.sh status"
  echo ""
}

# =============================================================================
# Entrypoint
# =============================================================================
case "${1:-}" in
  deploy)    deploy ;;
  reset)     reset_clusters "${2:-}" ;;
  setup-sp)  setup_kyverno_sp ;;
  status)    status ;;
  *)
    echo "Usage: $0 [deploy|reset [--force]|setup-sp|status]"
    exit 1
    ;;
esac