// Command claw is the kube-claw CLI. It talks to the controller API
// (DESIGN.md §14). Phase 0 ships the command tree; handlers land with their
// features.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "claw",
		Short:         "kube-claw control-plane CLI",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newSecretCmd(), newRunsCmd(), newAgentsCmd(), newInstallCmd())
	return root
}

// notImplemented is the Phase 0 placeholder run function.
func notImplemented(name string) func(*cobra.Command, []string) error {
	return func(*cobra.Command, []string) error {
		return fmt.Errorf("%q is not implemented yet (phase 0 skeleton)", name)
	}
}

func newSecretCmd() *cobra.Command {
	c := &cobra.Command{Use: "secret", Short: "Manage secrets, requests, and grants"}
	c.AddCommand(
		&cobra.Command{Use: "create NAME", Short: "Create secret metadata; prints a one-time intake link", RunE: notImplemented("secret create")},
		&cobra.Command{Use: "put NAME", Short: "Upload a value (break-glass / CI)", RunE: notImplemented("secret put")},
		&cobra.Command{Use: "metadata NAME", Short: "Show secret metadata", RunE: notImplemented("secret metadata")},
		&cobra.Command{Use: "approve REQUEST_ID", Short: "Approve a request (break-glass)", RunE: notImplemented("secret approve")},
		&cobra.Command{Use: "deny REQUEST_ID", Short: "Deny a request", RunE: notImplemented("secret deny")},
	)
	return c
}

func newRunsCmd() *cobra.Command {
	c := &cobra.Command{Use: "runs", Short: "Inspect agent runs"}
	c.AddCommand(
		&cobra.Command{Use: "list", Short: "List runs", RunE: notImplemented("runs list")},
		&cobra.Command{Use: "show RUN_ID", Short: "Show a run", RunE: notImplemented("runs show")},
	)
	return c
}

func newAgentsCmd() *cobra.Command {
	c := &cobra.Command{Use: "agents", Short: "Manage agents"}
	c.AddCommand(
		&cobra.Command{Use: "list", Short: "List agents", RunE: notImplemented("agents list")},
		&cobra.Command{Use: "wake AGENT", Short: "Wake an agent", RunE: notImplemented("agents wake")},
		&cobra.Command{Use: "sleep AGENT", Short: "Sleep an agent", RunE: notImplemented("agents sleep")},
	)
	return c
}

func newInstallCmd() *cobra.Command {
	c := &cobra.Command{Use: "install", Short: "Install helpers"}
	c.AddCommand(&cobra.Command{Use: "check", Short: "Check the install", RunE: notImplemented("install check")})
	return c
}
