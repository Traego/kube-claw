package sqlite

import (
	"database/sql"
	"errors"

	"github.com/traego/kube-claw/internal/store"
)

// SetChannelConfig creates or replaces a channel's bot behavior.
func (t *tx) SetChannelConfig(c store.ChannelConfig) error {
	if c.UpdatedAt == "" {
		c.UpdatedAt = store.NowRFC3339()
	}
	_, err := t.tx.Exec(
		`INSERT INTO channel_configs (channel, agent_ns, agent_name, mention_required, thread_only, updated_at)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT(channel) DO UPDATE SET
		   agent_ns=excluded.agent_ns, agent_name=excluded.agent_name,
		   mention_required=excluded.mention_required, thread_only=excluded.thread_only,
		   updated_at=excluded.updated_at`,
		c.Channel, c.AgentNamespace, c.AgentName, boolInt(c.MentionRequired), boolInt(c.ThreadOnly), c.UpdatedAt)
	return err
}

// GetChannelConfig returns a channel's config, or ErrNotFound.
func (t *tx) GetChannelConfig(channel string) (store.ChannelConfig, error) {
	var c store.ChannelConfig
	var mention, thread int
	err := t.tx.QueryRow(
		`SELECT channel, agent_ns, agent_name, mention_required, thread_only, updated_at
		 FROM channel_configs WHERE channel=?`, channel).
		Scan(&c.Channel, &c.AgentNamespace, &c.AgentName, &mention, &thread, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return store.ChannelConfig{}, store.ErrNotFound
	}
	if err != nil {
		return store.ChannelConfig{}, err
	}
	c.MentionRequired, c.ThreadOnly = mention != 0, thread != 0
	return c, nil
}

// ListChannelConfigs returns all configured channels.
func (t *tx) ListChannelConfigs() ([]store.ChannelConfig, error) {
	rows, err := t.tx.Query(
		`SELECT channel, agent_ns, agent_name, mention_required, thread_only, updated_at FROM channel_configs ORDER BY channel`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.ChannelConfig
	for rows.Next() {
		var c store.ChannelConfig
		var mention, thread int
		if err := rows.Scan(&c.Channel, &c.AgentNamespace, &c.AgentName, &mention, &thread, &c.UpdatedAt); err != nil {
			return nil, err
		}
		c.MentionRequired, c.ThreadOnly = mention != 0, thread != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
