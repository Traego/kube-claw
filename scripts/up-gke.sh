#!/usr/bin/env bash
# Stand up kube-claw on GKE Autopilot, end to end and idempotently:
#   enable APIs → create cluster → Artifact Registry → build+push images →
#   CRDs + namespaces → secrets → helm install → register base image + agent.
#
# Re-runnable: each step checks for existing state, so you can run it again to
# roll out a new image tag or finish a partial setup.
#
# Usage:
#   PROJECT=traego-infra ./scripts/up-gke.sh            # port-forward mode (no Ingress)
#   PROJECT=traego-infra INGRESS_HOST=claw.example.com ./scripts/up-gke.sh   # with HTTPS Ingress
#
# Env (with defaults):
#   PROJECT   (required)         GCP project id
#   REGION    =us-central1       cluster + Artifact Registry region
#   CLUSTER   =claw              Autopilot cluster name
#   REPO      =claw              Artifact Registry repo name
#   TAG       =<git short sha>   image tag to build/deploy
#   INGRESS_HOST (optional)      if set, expose the intake UI over HTTPS Ingress
#   SLACK     =0                 1 = create Slack token secret + enable Slack
#
# Credentials are read from the environment or a gitignored .secrets.env:
#   ANTHROPIC_API_KEY (required for the agent loop)
#   SLACK_APP_TOKEN, SLACK_BOT_TOKEN (only if SLACK=1)
set -euo pipefail
cd "$(dirname "$0")/.."

: "${PROJECT:?set PROJECT to your GCP project id}"
REGION="${REGION:-us-central1}"
CLUSTER="${CLUSTER:-claw}"
REPO="${REPO:-claw}"
TAG="${TAG:-$(git rev-parse --short HEAD)}"
SLACK="${SLACK:-0}"
REGISTRY="${REGION}-docker.pkg.dev/${PROJECT}/${REPO}"
[ -f .secrets.env ] && { set -a; . ./.secrets.env; set +a; }

say() { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }

say "1/8 Enable APIs"
gcloud services enable container.googleapis.com compute.googleapis.com artifactregistry.googleapis.com --project "$PROJECT"

say "2/8 Autopilot cluster '$CLUSTER' in $REGION"
if gcloud container clusters describe "$CLUSTER" --region "$REGION" --project "$PROJECT" >/dev/null 2>&1; then
  echo "cluster exists"
else
  gcloud container clusters create-auto "$CLUSTER" --region "$REGION" --project "$PROJECT"
fi
gcloud container clusters get-credentials "$CLUSTER" --region "$REGION" --project "$PROJECT"

say "3/8 Artifact Registry repo '$REPO'"
gcloud artifacts repositories describe "$REPO" --location "$REGION" --project "$PROJECT" >/dev/null 2>&1 \
  || gcloud artifacts repositories create "$REPO" --repository-format=docker --location "$REGION" --project "$PROJECT"
gcloud auth configure-docker "${REGION}-docker.pkg.dev" --quiet

say "4/8 Build + push images (tag $TAG)"
PROJECT="$PROJECT" REGION="$REGION" REPO="$REPO" TAG="$TAG" ./scripts/build-push-gke.sh

say "5/8 CRDs + namespaces"
kubectl apply -f charts/claw-crds/crds/
for ns in claw-system claw-agents; do kubectl get ns "$ns" >/dev/null 2>&1 || kubectl create namespace "$ns"; done

say "6/8 Secrets"
: "${ANTHROPIC_API_KEY:?set ANTHROPIC_API_KEY (env or .secrets.env)}"
for ns in claw-agents claw-system; do
  kubectl -n "$ns" create secret generic claw-anthropic-key --from-literal=api-key="$ANTHROPIC_API_KEY" \
    --dry-run=client -o yaml | kubectl apply -f -
done
if [ "$SLACK" = "1" ]; then
  : "${SLACK_APP_TOKEN:?}" "${SLACK_BOT_TOKEN:?}"
  kubectl -n claw-system create secret generic claw-slack-tokens \
    --from-literal=app-token="$SLACK_APP_TOKEN" --from-literal=bot-token="$SLACK_BOT_TOKEN" \
    --dry-run=client -o yaml | kubectl apply -f -
fi

say "7/8 Helm install"
HELM_ARGS=(upgrade --install claw ./charts/claw -n claw-system -f charts/claw/values-gke.yaml
  --set image.repository="${REGISTRY}/claw-controller" --set image.tag="$TAG"
  --set controller.runnerImage="${REGISTRY}/claw-runner:${TAG}")
if [ -n "${INGRESS_HOST:-}" ]; then
  HELM_ARGS+=(--set controller.uiBaseURL="https://${INGRESS_HOST}" --set ui.ingress.host="$INGRESS_HOST")
else
  # Port-forward mode: no Ingress, UI links point at the forwarded localhost.
  HELM_ARGS+=(--set ui.ingress.enabled=false --set controller.uiBaseURL=http://localhost:8090)
fi
[ "$SLACK" = "1" ] || HELM_ARGS+=(--set slack.enabled=false)
helm "${HELM_ARGS[@]}"
kubectl -n claw-system rollout status statefulset/claw-controller --timeout=180s

say "8/8 Register base image + agent (best-effort)"
kubectl -n claw-system port-forward svc/claw-controller 8443:8443 >/tmp/claw-pf-gke.log 2>&1 &
PF=$!; sleep 4
export CLAW_CONTROLLER_URL=http://localhost:8443
./bin/claw baseimage create gcloud --image "${REGISTRY}/claw-gcloud:${TAG}" \
  --description "Google Cloud SDK (gcloud, bq) — GCP cost/billing queries" || true
./bin/claw agent create gcp-cost --base gcloud \
  --system-prompt "You are a read-only GCP cost assistant. Use gcloud/bq to answer billing/spend questions; request a read-only billing key if you don't have one." || true
kill "$PF" 2>/dev/null || true

cat <<EOF

Done. kube-claw is running on $CLUSTER ($REGION).
  kubectl -n claw-system port-forward svc/claw-controller 8443:8443   # API + admin UI (/ui)
  kubectl -n claw-system port-forward svc/claw-controller 8090:8090   # secret-intake links
EOF
