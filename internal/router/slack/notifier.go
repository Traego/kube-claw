package slack

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/slack-go/slack"
)

// Notifier posts back to Slack: run replies (#2) and interactive PAM approval
// requests (#3). Constructed with the bot token; nil when Slack isn't configured.
// The live posting needs real Slack credentials to exercise.
type Notifier struct {
	api *slack.Client
}

func NewNotifier(botToken string) *Notifier {
	return &Notifier{api: slack.New(botToken)}
}

// PostReply posts an agent's output back to the originating Slack thread.
func (n *Notifier) PostReply(ctx context.Context, channel, threadTS, text string) error {
	opts := []slack.MsgOption{slack.MsgOptionText(text, false)}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	_, _, err := n.api.PostMessageContext(ctx, channel, opts...)
	return err
}

// PostApproval posts an interactive approve/deny message for a blocked run. The
// button values carry the request id (parsed back by HandleApproval); only a
// configured granter's click is honored.
func (n *Notifier) PostApproval(ctx context.Context, channel, threadTS, secretName, reqID string) error {
	section := slack.NewSectionBlock(
		slack.NewTextBlockObject("mrkdwn",
			fmt.Sprintf(":lock: An agent run needs secret *%s*. Approve?", secretName), false, false),
		nil, nil)
	approve := slack.NewButtonBlockElement("approve", ActionValue("approve", reqID),
		slack.NewTextBlockObject("plain_text", "Approve", false, false)).WithStyle(slack.StylePrimary)
	deny := slack.NewButtonBlockElement("deny", ActionValue("deny", reqID),
		slack.NewTextBlockObject("plain_text", "Deny", false, false)).WithStyle(slack.StyleDanger)
	actions := slack.NewActionBlock("claw-approval", approve, deny)

	opts := []slack.MsgOption{slack.MsgOptionBlocks(section, actions)}
	if threadTS != "" {
		opts = append(opts, slack.MsgOptionTS(threadTS))
	}
	_, _, err := n.api.PostMessageContext(ctx, channel, opts...)
	return err
}

// SlackChannel extracts the channel from a run's Source JSON (set by
// HandleMessage as {"trigger":"slack","channel":"...","event":"..."}).
func SlackChannel(source string) string { return slackSource(source).Channel }

// SlackEventTS extracts the triggering message ts from a run's Source JSON.
func SlackEventTS(source string) string { return slackSource(source).Event }

type slackSrc struct {
	Trigger string `json:"trigger"`
	Channel string `json:"channel"`
	Event   string `json:"event"`
}

func slackSource(source string) slackSrc {
	var s slackSrc
	if json.Unmarshal([]byte(source), &s) != nil || s.Trigger != "slack" {
		return slackSrc{}
	}
	return s
}

// AddReaction adds an emoji reaction to a message (e.g. 🤔 while the agent
// works). Needs the bot's reactions:write scope.
func (n *Notifier) AddReaction(ctx context.Context, channel, ts, name string) error {
	return n.api.AddReactionContext(ctx, name, slack.ItemRef{Channel: channel, Timestamp: ts})
}

// RemoveReaction removes a previously-added reaction (best-effort).
func (n *Notifier) RemoveReaction(ctx context.Context, channel, ts, name string) error {
	return n.api.RemoveReactionContext(ctx, name, slack.ItemRef{Channel: channel, Timestamp: ts})
}
