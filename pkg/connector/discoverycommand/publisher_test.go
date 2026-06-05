package discoverycommand

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

func ctxBackground() context.Context {
	return context.Background()
}

func TestPublisherPublishesCommandEnvelope(t *testing.T) {
	t.Parallel()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	publisher := &Publisher{
		cfg: Config{
			CommandsStream: "stream:commands",
			Debounce:       20 * time.Millisecond,
		},
		redis:   client,
		log:     zerolog.Nop(),
		pending: make(map[networkid.UserLoginID]*pendingSchedule),
	}

	publisher.Schedule(
		id.UserID("@synccontact_workspace_ws1:example.com"),
		networkid.UserLoginID("628135901813"),
		"history_sync_portals_complete",
	)

	time.Sleep(40 * time.Millisecond)

	entries, err := client.XRange(ctxBackground(), "stream:commands", "-", "+").Result()
	if err != nil {
		t.Fatalf("xrange: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 stream entry, got %d", len(entries))
	}

	var envelope map[string]any
	if err := json.Unmarshal([]byte(entries[0].Values["message"]), &envelope); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if envelope["commandType"] != commandType {
		t.Fatalf("unexpected commandType: %v", envelope["commandType"])
	}
	payload, ok := envelope["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload missing")
	}
	if payload["bridgeLoginId"] != "628135901813" {
		t.Fatalf("unexpected bridgeLoginId: %v", payload["bridgeLoginId"])
	}
}
