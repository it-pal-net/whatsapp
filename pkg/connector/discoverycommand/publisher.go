package discoverycommand

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

const (
	commandType   = "external.conversation.discover.requested.v1"
	targetService = "chat-service"
)

type Config struct {
	RedisURL       string        `yaml:"discovery_redis_url"`
	CommandsStream string        `yaml:"discovery_commands_stream"`
	Debounce       time.Duration `yaml:"discovery_command_debounce"`
}

type Publisher struct {
	cfg    Config
	redis  *redis.Client
	log    zerolog.Logger
	mu     sync.Mutex
	pending map[networkid.UserLoginID]*pendingSchedule
}

type pendingSchedule struct {
	ownerMXID id.UserID
	loginID   networkid.UserLoginID
	reason    string
	timer     *time.Timer
}

func New(cfg Config, log zerolog.Logger) (*Publisher, error) {
	if cfg.RedisURL == "" {
		return nil, nil
	}
	if cfg.CommandsStream == "" {
		cfg.CommandsStream = "stream:commands"
	}
	if cfg.Debounce <= 0 {
		cfg.Debounce = 30 * time.Second
	}

	options, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		options = &redis.Options{Addr: cfg.RedisURL}
	}
	client := redis.NewClient(options)

	return &Publisher{
		cfg:     cfg,
		redis:   client,
		log:     log.With().Str("component", "discovery_command").Logger(),
		pending: make(map[networkid.UserLoginID]*pendingSchedule),
	}, nil
}

func (p *Publisher) Enabled() bool {
	return p != nil && p.redis != nil
}

func (p *Publisher) Close() error {
	if p == nil || p.redis == nil {
		return nil
	}
	return p.redis.Close()
}

func (p *Publisher) Schedule(ownerMXID id.UserID, loginID networkid.UserLoginID, reason string) {
	if !p.Enabled() {
		return
	}
	if ownerMXID == "" || loginID == "" {
		return
	}
	if reason == "" {
		reason = "bridge_portals_updated"
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if existing, ok := p.pending[loginID]; ok {
		existing.reason = reason
		existing.timer.Stop()
		existing.timer = time.AfterFunc(p.cfg.Debounce, func() {
			p.publish(existing.ownerMXID, existing.loginID, existing.reason)
		})
		return
	}

	entry := &pendingSchedule{
		ownerMXID: ownerMXID,
		loginID:   loginID,
		reason:    reason,
	}
	entry.timer = time.AfterFunc(p.cfg.Debounce, func() {
		p.publish(entry.ownerMXID, entry.loginID, entry.reason)
	})
	p.pending[loginID] = entry
}

func (p *Publisher) publish(ownerMXID id.UserID, loginID networkid.UserLoginID, reason string) {
	p.mu.Lock()
	delete(p.pending, loginID)
	p.mu.Unlock()

	bucket := time.Now().Unix() / 60
	envelope := map[string]any{
		"messageId":      uuid.NewString(),
		"kind":           "command",
		"commandType":    commandType,
		"targetService":  targetService,
		"tenantId":       string(loginID),
		"requestedAt":    time.Now().UTC().Format(time.RFC3339Nano),
		"idempotencyKey": fmt.Sprintf("external-conversation-discover:bridge:%s:%s:%d", ownerMXID, loginID, bucket),
		"actor": map[string]string{
			"type": "system",
			"id":   "whatsapp-bridge",
		},
		"payload": map[string]any{
			"ownerMatrixUserId":   string(ownerMXID),
			"bridgeLoginId":       string(loginID),
			"bridgeTriggerReason": reason,
			"force":               true,
		},
	}

	raw, err := json.Marshal(envelope)
	if err != nil {
		p.log.Err(err).Msg("Failed to marshal discovery command envelope")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	streamID, err := p.redis.XAdd(ctx, &redis.XAddArgs{
		Stream: p.cfg.CommandsStream,
		Values: map[string]interface{}{
			"message": string(raw),
		},
	}).Result()
	if err != nil {
		p.log.Err(err).
			Str("owner_mxid", string(ownerMXID)).
			Str("login_id", string(loginID)).
			Str("reason", reason).
			Msg("Failed to publish discovery command to Redis")
		return
	}

	p.log.Info().
		Str("stream_id", streamID).
		Str("owner_mxid", string(ownerMXID)).
		Str("login_id", string(loginID)).
		Str("reason", reason).
		Msg("Published external conversation discovery command to Redis")
}
