// Command claw is the kube-claw control-plane CLI (DESIGN.md §14). It talks to
// the controller API; for local use, port-forward the controller and pass
// --controller-url (or set CLAW_CONTROLLER_URL).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
)

var controllerURL string

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "claw", Short: "kube-claw control-plane CLI", SilenceUsage: true, SilenceErrors: true}
	def := os.Getenv("CLAW_CONTROLLER_URL")
	if def == "" {
		def = "http://localhost:8443"
	}
	root.PersistentFlags().StringVar(&controllerURL, "controller-url", def, "controller API base URL")
	root.AddCommand(newSecretCmd(), newRunCmd(), newRunsCmd())
	return root
}

func newSecretCmd() *cobra.Command {
	c := &cobra.Command{Use: "secret", Short: "Manage secrets"}

	var ns, typ string
	var granters []string
	create := &cobra.Command{
		Use:   "create NAME",
		Short: "Create secret metadata; prints a one-time intake link",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			var out map[string]string
			if err := apiJSON(http.MethodPost, "/v1/secrets", map[string]any{
				"namespace": ns, "name": args[0], "type": typ, "granters": granters,
			}, &out); err != nil {
				return err
			}
			fmt.Printf("created secret %s\n", out["id"])
			fmt.Printf("open this one-time link to submit the value:\n  %s\n", out["intakeURL"])
			return nil
		},
	}
	create.Flags().StringVar(&ns, "namespace", "claw-agents", "namespace")
	create.Flags().StringVar(&typ, "type", "", "secret type")
	create.Flags().StringArrayVar(&granters, "granter", nil, "granter principal (repeatable)")

	var putNS, fromFile string
	put := &cobra.Command{
		Use:   "put NAME",
		Short: "Upload a value (break-glass / CI)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			data, err := os.ReadFile(fromFile)
			if err != nil {
				return err
			}
			return apiRaw(http.MethodPut, "/v1/secrets/"+args[0]+"/versions?namespace="+putNS, data)
		},
	}
	put.Flags().StringVar(&putNS, "namespace", "claw-agents", "namespace")
	put.Flags().StringVar(&fromFile, "from-file", "", "file containing the secret value")
	_ = put.MarkFlagRequired("from-file")

	var metaNS string
	meta := &cobra.Command{
		Use:   "metadata NAME",
		Short: "Show secret metadata (never the value)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return apiPrint(http.MethodGet, "/v1/secrets/"+args[0]+"/metadata?namespace="+metaNS)
		},
	}
	meta.Flags().StringVar(&metaNS, "namespace", "claw-agents", "namespace")

	c.AddCommand(create, put, meta)
	return c
}

func newRunCmd() *cobra.Command {
	c := &cobra.Command{Use: "run", Short: "Trigger runs"}
	var ns, agent, input string
	create := &cobra.Command{
		Use:   "create",
		Short: "Trigger a run directly (no Slack)",
		RunE: func(_ *cobra.Command, _ []string) error {
			var out map[string]string
			if err := apiJSON(http.MethodPost, "/v1/runs", map[string]any{
				"namespace": ns, "agent": agent, "input": input,
			}, &out); err != nil {
				return err
			}
			fmt.Printf("run %s (%s)\n", out["id"], out["phase"])
			return nil
		},
	}
	create.Flags().StringVar(&ns, "namespace", "claw-agents", "namespace")
	create.Flags().StringVar(&agent, "agent", "", "agent name")
	create.Flags().StringVar(&input, "input", "", "input text")
	_ = create.MarkFlagRequired("agent")
	c.AddCommand(create)
	return c
}

func newRunsCmd() *cobra.Command {
	c := &cobra.Command{Use: "runs", Short: "Inspect runs"}
	c.AddCommand(&cobra.Command{
		Use: "show RUN_ID", Short: "Show a run", Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return apiPrint(http.MethodGet, "/v1/runs/"+args[0])
		},
	})
	return c
}

// --- tiny API client ---

func httpClient() *http.Client { return &http.Client{Timeout: 15 * time.Second} }

func apiJSON(method, path string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, controllerURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, string(data))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

func apiRaw(method, path string, body []byte) error {
	req, err := http.NewRequest(method, controllerURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	resp, err := httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, string(data))
	}
	fmt.Println(string(data))
	return nil
}

func apiPrint(method, path string) error {
	req, _ := http.NewRequest(method, controllerURL+path, nil)
	resp, err := httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, string(data))
	}
	var pretty bytes.Buffer
	if json.Indent(&pretty, data, "", "  ") == nil {
		fmt.Println(pretty.String())
	} else {
		fmt.Println(string(data))
	}
	return nil
}
