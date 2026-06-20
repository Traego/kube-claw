# kube-claw TODOs

Deferred work captured during the v0.2 eng review (2026-06-20). Not MVP-blocking.

## T-1 — Configurable runner failure handling / retry
- **What:** Replace the v0 "100% pass-through" exit-code behavior with configurable retry / backoff / dead-letter per agent.
- **Why:** Some runs should retry transient failures instead of failing the AgentRun outright.
- **Pros:** Resilience for flaky downstreams (GCP API hiccups).
- **Cons:** Policy surface + tests; can mask real failures if misconfigured.
- **Context:** Today `claw-bootstrap` forwards the runner's exit code verbatim (§11). Add a policy on the Agent spec.
- **Depends on:** delivery model (done).

## T-2 — Audit + dedupe retention / GC
- **What:** Retention or rollup for the `audit` and `dedupe` tables.
- **Why:** Both grow unbounded; `audit` especially on a long-lived controller. `claw_db_size_bytes` is exported but there's no compaction.
- **Pros:** Bounded PVC growth; predictable DB size.
- **Cons:** Must not break audit tamper-evidence (hash chain) — rollup needs a checkpoint/anchor scheme.
- **Depends on:** SQLite store (Phase 2), hash-chained audit.

## T-3 — Slack-DM the secret-intake link
- **What:** Deliver the one-time intake link (§8.3) to the granter via Slack DM instead of only printing it from `claw secret create`.
- **Why:** Closes the PAM loop in Slack; nicer for non-CLI humans.
- **Pros:** Smoother onboarding of a secret value.
- **Cons:** Links in Slack get fetched by URL-preview bots (read-only GET, no consume) — verify no token consumption on GET.
- **Depends on:** router interactions (Phase 6) + intake UI (§8.3).

## T-4 — Controller HA
- **What:** Remove the single-replica SPOF via leader election + warm standby, or a networked `Store` backend (Postgres/Spanner).
- **Why:** Controller down ⇒ all runs blocked (fail-closed). Acceptable for MVP, not for production uptime.
- **Pros:** No single point of failure.
- **Cons:** Large; networked store is the cleaner path (the §7 `Store` interface already allows it).
- **Depends on:** pluggable `Store` interface (in v0.2 design).

## T-5 — Optional second factor for high-sensitivity secret approval
- **What:** Require a second factor (or "approver must also be in-cluster") for secrets above a sensitivity threshold.
- **Why:** Slack identity is the sole root of release authority; a compromised Slack account = release authority (§8.1 trust note).
- **Pros:** Defense in depth for the most dangerous credentials.
- **Cons:** More approval friction; needs a sensitivity classification on secrets.
- **Depends on:** approval engine (Phase 4).

## T-6 — Granter-bound intake link
- **What:** Bind the one-time intake link (§8.3) to a verified granter so only the intended human can submit the value.
- **Why:** Today the submitter is unauthenticated — an attacker with the link could submit their own credential.
- **Pros:** Closes the "attacker submits their own key" gap.
- **Cons:** Requires identity on the public intake path (OIDC / Slack-verified click) — more surface on a public endpoint.
- **Depends on:** intake UI (§8.3), possibly the identity provider (§9).

## T-7 — External identity providers
- **What:** Implement OIDC / SPIFFE / cloud-IAM `IdentityProvider`s behind the §9 interface (MVP ships only the Kubernetes SA provider).
- **Why:** Lets agents authenticate against external auth sources.
- **Pros:** First-class pluggable identity realized.
- **Cons:** Each provider is its own verification + trust model.
- **Depends on:** identity interface (in v0.2 design).
