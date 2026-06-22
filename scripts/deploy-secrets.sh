#!/usr/bin/env bash
# Deploy kube-claw with Slack + the agent loop wired up, reading tokens from a
# gitignored secrets file (so they never hit the transcript or git history).
#
# Usage:
#   1. Create .secrets.env (gitignored) in the repo root:
#        SLACK_APP_TOKEN=xapp-...        # Socket Mode app-level token
#        SLACK_BOT_TOKEN=xoxb-...        # bot token (needs chat:write)
#        ANTHROPIC_API_KEY=sk-ant-...    # powers the agent loop
#        SLACK_CHANNEL=C0123ABC          # optional: channel to monitor
#        SLACK_AGENT=assistant           # optional: agent for that channel (default: assistant)
#        SLACK_MENTION=true              # optional: only @mentions trigger (default: true)
#   2. ./scripts/deploy-secrets.sh [path-to-secrets-file]
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="${1:-$ROOT/.secrets.env}"
NS="${NS:-claw-system}"
AGENTS_NS="${AGENTS_NS:-claw-agents}"

[ -f "$ENV_FILE" ] || { echo "secrets file not found: $ENV_FILE" >&2; exit 1; }
set -a; . "$ENV_FILE"; set +a

: "${SLACK_APP_TOKEN:?set SLACK_APP_TOKEN in $ENV_FILE}"
: "${SLACK_BOT_TOKEN:?set SLACK_BOT_TOKEN in $ENV_FILE}"
SLACK_AGENT="${SLACK_AGENT:-assistant}"
SLACK_MENTION="${SLACK_MENTION:-true}"

echo "Creating Slack token Secret in $NS..."
kubectl -n "$NS" create secret generic claw-slack-tokens \
  --from-literal=app-token="$SLACK_APP_TOKEN" --from-literal=bot-token="$SLACK_BOT_TOKEN" \
  --dry-run=client -o yaml | kubectl apply -f -

if [ -n "${ANTHROPIC_API_KEY:-}" ]; then
  echo "Creating Anthropic key Secret in $AGENTS_NS..."
  kubectl -n "$AGENTS_NS" create secret generic claw-anthropic-key \
    --from-literal=api-key="$ANTHROPIC_API_KEY" --dry-run=client -o yaml | kubectl apply -f -
fi

helm_args=(--reuse-values --set slack.enabled=true)
if [ -n "${SLACK_CHANNEL:-}" ]; then
  routes="[{\"channels\":[\"$SLACK_CHANNEL\"],\"mentionRequired\":$SLACK_MENTION,\"agentNamespace\":\"$AGENTS_NS\",\"agentName\":\"$SLACK_AGENT\"}]"
  helm_args+=(--set-json "slack.routes=$routes")
  echo "Routing $SLACK_CHANNEL -> $SLACK_AGENT (mentionRequired=$SLACK_MENTION)"
fi

echo "helm upgrade..."
helm upgrade claw "$ROOT/charts/claw" -n "$NS" "${helm_args[@]}"
kubectl -n "$NS" rollout restart statefulset/claw-controller
kubectl -n "$NS" rollout status statefulset/claw-controller --timeout=120s

echo "Done. Watch the connector:"
echo "  kubectl -n $NS logs -f statefulset/claw-controller | grep -i slack"
