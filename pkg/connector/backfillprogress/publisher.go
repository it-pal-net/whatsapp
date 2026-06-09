// Package backfillprogress publishes WhatsApp history-backfill progress as
// SyncContact domain events to Redis (stream:domain-events). The realtime gateway
// consumes that stream and fans the events out to room:{matrixRoomId} subscribers,
// so the web UI can show a live progress bar while older messages are inserted.
//
// This mirrors the discoverycommand publisher, but emits domain events (kind
// "domain_event") instead of commands, so no extra consumer is needed — the event
// routes straight to the room channel via its payload.matrixRoomId.
package backfillprogress

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/id"
)

const (
	// Must be registered in apps/api/shared/messaging/domainEvents.js and pass the
	// versioned-name regex used by validateMessage.js.
	eventType     = "whatsapp.backfill.progress.v1"
	aggregateType = "room"
)

// Phase identifies where in a backfill batch a progress event was emitted.
const (
	PhaseStarted  = "started"
	PhaseProgress = "progress"
	PhaseFinished = "finished"
)

type Config struct {
	RedisURL string `yaml:"backfill_progress_redis_url"`
	Stream   string `yaml:"backfill_progress_stream"`
}

type Publisher struct {
	redis  *redis.Client
	stream string
	log    zerolog.Logger
}

// Update is one progress tick for a room's backfill.
type Update struct {
	OwnerMXID    id.UserID
	MatrixRoomID id.RoomID
	Phase        string
	// Current/Total describe progress within the current batch (Total is the batch
	// size; Current counts messages converted so far). Loaded is the cumulative
	// number of messages inserted in this backfill request.
	Current int
	Total   int
	Loaded  int
	HasMore bool
}

func New(cfg Config, log zerolog.Logger) (*Publisher, error) {
	if cfg.RedisURL == "" {
		return nil, nil
	}
	if cfg.Stream == "" {
		cfg.Stream = "stream:domain-events"
	}

	options, err := redis.ParseURL(cfg.RedisURL)
	if err != nil {
		options = &redis.Options{Addr: cfg.RedisURL}
	}

	return &Publisher{
		redis:  redis.NewClient(options),
		stream: cfg.Stream,
		log:    log.With().Str("component", "backfill_progress").Logger(),
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

// Publish emits a single progress domain event. Failures are logged, never fatal —
// progress reporting must not break backfilling itself.
func (p *Publisher) Publish(ctx context.Context, u Update) {
	if !p.Enabled() || u.MatrixRoomID == "" {
		return
	}

	now := time.Now().UTC()
	envelope := map[string]any{
		"messageId":     uuid.NewString(),
		"kind":          "domain_event",
		"eventType":     eventType,
		"aggregateType": aggregateType,
		"aggregateId":   string(u.MatrixRoomID),
		"tenantId":      string(u.OwnerMXID),
		"occurredAt":    now.Format(time.RFC3339Nano),
		"source": map[string]string{
			"kind": "service",
			"name": "whatsapp-bridge",
		},
		"actor": map[string]string{
			"type": "system",
			"id":   "whatsapp-bridge",
		},
		"payload": map[string]any{
			"matrixRoomId": string(u.MatrixRoomID),
			"phase":        u.Phase,
			"current":      u.Current,
			"total":        u.Total,
			"loaded":       u.Loaded,
			"hasMore":      u.HasMore,
		},
	}

	raw, err := json.Marshal(envelope)
	if err != nil {
		p.log.Err(err).Msg("Failed to marshal backfill progress event")
		return
	}

	// Don't inherit the (possibly already-cancelled) backfill context for the publish.
	publishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	if err := p.redis.XAdd(publishCtx, &redis.XAddArgs{
		Stream: p.stream,
		Values: map[string]interface{}{"message": string(raw)},
	}).Err(); err != nil {
		p.log.Warn().Err(err).
			Stringer("matrix_room_id", u.MatrixRoomID).
			Str("phase", u.Phase).
			Msg("Failed to publish backfill progress to Redis")
	}
}
