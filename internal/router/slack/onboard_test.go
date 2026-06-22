package slack

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/traego/kube-claw/internal/store/sqlite"
)

func TestOnboardingSetsDynamicRoute(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "claw.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	if err := st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	r := &Router{Store: st}

	// No config yet → no route.
	if rt := r.resolveRoute(ctx, "C_NEW", false); rt != nil {
		t.Fatalf("expected no route before onboarding, got %+v", rt)
	}

	// Onboard "Active · threads only" for C_NEW → agent assistant.
	msg := r.HandleOnboard(ctx, onboardValue("C_NEW", "claw-agents", "assistant", false, true))
	if msg == "" {
		t.Fatal("expected a confirmation message")
	}

	// Active (mention not required) → a plain message now routes.
	rt := r.resolveRoute(ctx, "C_NEW", false)
	if rt == nil || rt.AgentName != "assistant" || rt.AgentNamespace != "claw-agents" {
		t.Fatalf("dynamic route = %+v, want assistant/claw-agents", rt)
	}

	// @-only channel should not route a non-mention message.
	_ = r.HandleOnboard(ctx, onboardValue("C_MENTION", "claw-agents", "assistant", true, true))
	if rt := r.resolveRoute(ctx, "C_MENTION", false); rt != nil {
		t.Fatalf("@-only channel routed a non-mention: %+v", rt)
	}
	if rt := r.resolveRoute(ctx, "C_MENTION", true); rt == nil {
		t.Fatal("@-only channel did not route an @mention")
	}
}
