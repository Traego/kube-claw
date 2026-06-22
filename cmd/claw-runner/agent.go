package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
)

// agentSession is one warm Slack thread: a Claude tool-use loop (claude-opus-4-8,
// adaptive thinking, a bash tool) whose message history persists across turns in
// memory — so follow-up messages continue the same conversation without replay.
type agentSession struct {
	client   anthropic.Client
	sys      string
	tools    []anthropic.ToolUnionParam
	messages []anthropic.MessageParam
}

func newAgentSession(systemPrompt string) *agentSession {
	sys := strings.TrimSpace(systemPrompt)
	if sys == "" {
		sys = "You are a cloud operations assistant."
	}
	notes := []string{
		"You are running inside an isolated, ephemeral Linux container. You have a `bash` tool to run shell commands here.",
	}
	for _, cli := range []string{"gcloud", "aws", "az"} {
		if haveCLI(cli) {
			notes = append(notes, "The `"+cli+"` CLI is installed and already authenticated via mounted credentials.")
		}
	}
	for _, d := range manifestDescriptions() {
		if d != "" {
			notes = append(notes, "Available secret: "+d)
		}
	}
	notes = append(notes, "Prefer read-only commands. Answer the user's question concisely, then stop. This is a chat thread — you may be asked follow-up questions.")
	sys = sys + "\n\n" + strings.Join(notes, "\n")

	bashTool := anthropic.ToolParam{
		Name:        "bash",
		Description: anthropic.String("Run a shell command in the container; returns combined stdout+stderr. Use read-only commands."),
		InputSchema: anthropic.ToolInputSchemaParam{
			Properties: map[string]any{
				"command": map[string]any{"type": "string", "description": "The shell command to run"},
			},
			Required: []string{"command"},
		},
	}
	return &agentSession{
		client: anthropic.NewClient(), // reads ANTHROPIC_API_KEY
		sys:    sys,
		tools:  []anthropic.ToolUnionParam{{OfTool: &bashTool}},
	}
}

// turn runs one user message to a final answer, executing bash tool calls along
// the way. History accumulates on the session for the next turn.
func (s *agentSession) turn(ctx context.Context, userText string) (string, error) {
	s.messages = append(s.messages, anthropic.NewUserMessage(anthropic.NewTextBlock(userText)))
	adaptive := anthropic.ThinkingConfigAdaptiveParam{}

	var final []string
	for i := 0; i < 12; i++ { // bound the agentic loop per turn
		resp, err := s.client.Messages.New(ctx, anthropic.MessageNewParams{
			Model:     anthropic.ModelClaudeOpus4_8,
			MaxTokens: 4096,
			System:    []anthropic.TextBlockParam{{Text: s.sys}},
			Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
			Tools:     s.tools,
			Messages:  s.messages,
		})
		if err != nil {
			return "", err
		}
		s.messages = append(s.messages, resp.ToParam())

		var turn []string
		var toolResults []anthropic.ContentBlockParamUnion
		for _, block := range resp.Content {
			switch v := block.AsAny().(type) {
			case anthropic.TextBlock:
				if t := strings.TrimSpace(v.Text); t != "" {
					turn = append(turn, t)
				}
			case anthropic.ToolUseBlock:
				var in struct {
					Command string `json:"command"`
				}
				_ = json.Unmarshal([]byte(v.JSON.Input.Raw()), &in)
				toolResults = append(toolResults, anthropic.NewToolResultBlock(block.ID, runBash(ctx, in.Command), false))
			}
		}
		if len(turn) > 0 {
			final = turn
		}
		if resp.StopReason != anthropic.StopReasonToolUse {
			break
		}
		s.messages = append(s.messages, anthropic.NewUserMessage(toolResults...))
	}
	answer := strings.Join(final, "\n\n")
	if answer == "" {
		return "", fmt.Errorf("agent produced no text answer")
	}
	return answer, nil
}

// runBash executes one shell command in the writable /workspace, with a timeout
// and a bounded output. The container's read-only rootfs + dropped caps + tmpfs
// secrets are the sandbox; this is not a second security boundary.
func runBash(parent context.Context, cmd string) string {
	if strings.TrimSpace(cmd) == "" {
		return "(empty command)"
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, "bash", "-lc", cmd)
	c.Dir = "/workspace"
	out, _ := c.CombinedOutput()
	s := strings.TrimSpace(string(out))
	if len(s) > 8000 {
		s = s[:8000] + "\n…(truncated)"
	}
	if s == "" {
		s = "(no output)"
	}
	return s
}
