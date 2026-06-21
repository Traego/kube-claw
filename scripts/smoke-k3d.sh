#!/usr/bin/env bash
# Reproducible local smoke test for kube-claw on k3d.
#
# Stands up an isolated k3d cluster, builds + imports the controller and runner
# images, helm-installs the charts, applies the example Agent, then triggers a
# run and asserts the agent responds end-to-end.
#
# SAFETY: uses a dedicated kubeconfig (KUBECONFIG=/tmp/claw-kubeconfig) and never
# touches your default kubeconfig (which may point at prod). It refuses to run
# kubectl/helm against any context whose name does not start with "k3d-".
#
# Usage:
#   scripts/smoke-k3d.sh           # create (if needed), build, deploy, test
#   scripts/smoke-k3d.sh --clean   # delete the cluster and exit
set -euo pipefail

CLUSTER=claw-dev
export KUBECONFIG=/tmp/claw-kubeconfig
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if [[ "${1:-}" == "--clean" ]]; then
  k3d cluster delete "$CLUSTER"
  exit 0
fi

# 1. Cluster (isolated kubeconfig; do not touch the default/prod one).
if ! k3d cluster list "$CLUSTER" >/dev/null 2>&1; then
  k3d cluster create "$CLUSTER" \
    --kubeconfig-update-default=false --kubeconfig-switch-context=false --wait
fi
k3d kubeconfig get "$CLUSTER" > "$KUBECONFIG"

CTX="$(kubectl config current-context)"
case "$CTX" in
  k3d-*) echo "context OK: $CTX" ;;
  *) echo "REFUSING: context '$CTX' is not a k3d cluster" >&2; exit 1 ;;
esac

# 2. Build + import images.
docker build -q -t claw-controller:dev .
docker build -q -f Dockerfile.runner -t claw-runner:dev .
k3d image import claw-controller:dev claw-runner:dev -c "$CLUSTER"

# 3. Install/upgrade charts.
helm upgrade --install claw-crds ./charts/claw-crds
kubectl create namespace claw-system --dry-run=client -o yaml | kubectl apply -f -
kubectl create namespace claw-agents --dry-run=client -o yaml | kubectl apply -f -
helm upgrade --install claw ./charts/claw -n claw-system \
  --set image.repository=claw-controller --set image.tag=dev --set image.pullPolicy=IfNotPresent
kubectl -n claw-system rollout restart statefulset/claw-controller
kubectl -n claw-system rollout status statefulset/claw-controller --timeout=120s

# 4. Apply the example Agent.
kubectl apply -f examples/gcp-cost-slack/agent.yaml
sleep 3
kubectl -n claw-agents get agent gcp-cost \
  -o jsonpath='{"agent phase="}{.status.phase}{" digest="}{.status.selectedImageDigest}{"\n"}'

# 5. Trigger a run and assert the response.
kubectl -n claw-system port-forward svc/claw-controller 18443:8443 >/tmp/claw-pf.log 2>&1 &
PF=$!; trap 'kill $PF 2>/dev/null || true' EXIT
sleep 4

RID="$(curl -s -X POST http://localhost:18443/v1/runs -H 'content-type: application/json' \
  -d '{"namespace":"claw-agents","agent":"gcp-cost","input":"why did GCP cost spike yesterday?"}' \
  | sed -n 's/.*"id":"\([^"]*\)".*/\1/p')"
echo "run: $RID"

for i in $(seq 1 20); do
  R="$(curl -s "http://localhost:18443/v1/runs/$RID")"
  PHASE="$(echo "$R" | sed -n 's/.*"phase":"\([^"]*\)".*/\1/p')"
  echo "  [$((i*2))s] phase=$PHASE"
  if [[ "$PHASE" == "Succeeded" ]]; then
    echo "SMOKE TEST PASSED"
    echo "$R"
    exit 0
  fi
  sleep 2
done
echo "SMOKE TEST FAILED: run did not reach Succeeded" >&2
exit 1
