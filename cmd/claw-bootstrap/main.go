// Command claw-bootstrap is the agent pod entrypoint (PID 1). It performs the
// /login token exchange, materializes approved secrets to tmpfs, then fork/execs
// the runner as a subprocess, forwarding signals and the exit code, and wiping
// the tmpfs secret on exit (DESIGN.md §9, §11).
//
// Phase 0 parses the CLI contract; the login/materialize/exec/supervise flow
// lands in Phase 5.
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	var runID, controllerURL, secretsDir, runner string
	flag.StringVar(&runID, "run-id", "", "AgentRun id")
	flag.StringVar(&controllerURL, "controller-url", "", "controller base URL")
	flag.StringVar(&secretsDir, "secrets-dir", "/var/run/claw/secrets", "tmpfs dir for materialized secrets")
	flag.StringVar(&runner, "runner", "/claw/runner", "path to the runner binary to exec")
	flag.Parse()
	runnerArgs := flag.Args() // args after "--" are passed verbatim to the runner

	// Phase 5 will: login -> materialize(secretsDir) -> fork/exec(runner, runnerArgs)
	// -> forward signals + reap -> wipe(secretsDir) -> exit(childCode).
	fmt.Printf("claw-bootstrap (phase 0 skeleton): run-id=%q controller=%q secrets-dir=%q runner=%q args=%v\n",
		runID, controllerURL, secretsDir, runner, runnerArgs)
	os.Exit(0)
}
