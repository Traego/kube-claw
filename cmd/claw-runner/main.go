// Command claw-runner is the default reference agent runner. It is invoked by
// claw-bootstrap as a subprocess and honors the CLAW_* env contract: it reads
// the task input, uses the materialized credential, runs the agent loop, and
// posts outputs back to the controller (DESIGN.md §11).
//
// Phase 0 prints the contract it received; the agent loop lands in Phase 7.
package main

import (
	"fmt"
	"os"
)

// clawEnv are the contract variables the bootstrap sets (DESIGN.md §11).
var clawEnv = []string{
	"CLAW_TOKEN", "CLAW_RUN_ID", "CLAW_AGENT_NAME", "CLAW_AGENT_NAMESPACE",
	"CLAW_SESSION_ID", "CLAW_CONTROLLER_URL", "CLAW_SECRETS_DIR", "CLAW_INPUT_FILE",
	"CLAW_WORKSPACE_DIR", "CLAW_MEMORY_DIR",
}

func main() {
	fmt.Println("claw-runner (phase 0 skeleton): contract env:")
	for _, k := range clawEnv {
		if k == "CLAW_TOKEN" {
			// Never print the token value.
			fmt.Printf("  %s=%s\n", k, redacted(os.Getenv(k)))
			continue
		}
		fmt.Printf("  %s=%q\n", k, os.Getenv(k))
	}
}

func redacted(v string) string {
	if v == "" {
		return ""
	}
	return "<set>"
}
