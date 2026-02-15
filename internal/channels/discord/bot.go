package discord

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
	"openclawssy/internal/channels/chat"
	"openclawssy/internal/config"
)

type QueueFunc func(ctx context.Context, agentID, message, source string) (string, error)

type Bot struct {
	cfg       config.DiscordConfig
	allow     *chat.Allowlist
	limiter   *chat.RateLimiter
	queue     QueueFunc
	session   *discordgo.Session
	closeOnce sync.Once
}

func New(cfg config.Config, queue QueueFunc) (*Bot, error) {
	token := strings.TrimSpace(cfg.Discord.Token)
	if token == "" && cfg.Discord.TokenEnv != "" {
		token = strings.TrimSpace(os.Getenv(cfg.Discord.TokenEnv))
	}
	if token == "" {
		return nil, errors.New("discord token is required")
	}
	allow := chat.NewAllowlist(cfg.Discord.AllowUsers, cfg.Discord.AllowChannels)
	limiter := chat.NewRateLimiter(cfg.Discord.RateLimitPerMin, time.Minute)
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	b := &Bot{cfg: cfg.Discord, allow: allow, limiter: limiter, queue: queue, session: s}
	s.AddHandler(b.onMessage)
	s.Identify.Intents = discordgo.IntentsGuildMessages | discordgo.IntentsDirectMessages | discordgo.IntentsMessageContent
	return b, nil
}

func (b *Bot) Start() error {
	return b.session.Open()
}

func (b *Bot) Stop() error {
	var err error
	b.closeOnce.Do(func() { err = b.session.Close() })
	return err
}

func (b *Bot) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot {
		return
	}
	content := strings.TrimSpace(m.Content)
	if b.cfg.CommandPrefix != "" {
		if !strings.HasPrefix(content, b.cfg.CommandPrefix) {
			return
		}
		content = strings.TrimSpace(strings.TrimPrefix(content, b.cfg.CommandPrefix))
	}
	if content == "" {
		return
	}

	if len(b.cfg.AllowGuilds) > 0 && !contains(b.cfg.AllowGuilds, m.GuildID) {
		return
	}
	if b.allow != nil && !b.allow.MessageAllowed(m.Author.ID, m.ChannelID) {
		return
	}
	if b.limiter != nil && !b.limiter.Allow(m.Author.ID+":"+m.ChannelID) {
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "rate limited, try again soon", m.Reference())
		return
	}

	agentID := b.cfg.DefaultAgentID
	if agentID == "" {
		agentID = "default"
	}
	runID, err := b.queue(context.Background(), agentID, content, "discord")
	if err != nil {
		_, _ = s.ChannelMessageSendReply(m.ChannelID, "failed to queue run: "+err.Error(), m.Reference())
		return
	}
	_, _ = s.ChannelMessageSendReply(m.ChannelID, "queued run `"+runID+"`", m.Reference())
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}
