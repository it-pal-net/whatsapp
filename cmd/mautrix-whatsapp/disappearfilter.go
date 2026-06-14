package main

import (
	"context"

	"github.com/rs/zerolog"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-whatsapp/pkg/waid"
)

// disappearRedactionReason is the redaction reason bridgev2's DisappearLoop
// attaches to timer-expired messages — keep in sync with
// maunium.net/go/mautrix/bridgev2/disappear.go.
const disappearRedactionReason = "Message disappeared"

// disappearFilteringBot wraps the bridge bot to drop the DisappearLoop's
// redactions in portals that keep timer-expired messages (the default;
// waid.PortalMetadata.RespectDisappearingTimer turns the redactions back on
// per room). bridgev2 has no per-portal hook for disappearing messages — the
// loop redacts through the bot directly — so the bot is the narrowest
// interception point. Returning success makes the loop clear its queue row,
// which is fine either way: a kept message needs no retry, and flipping the
// setting later intentionally only affects messages bridged afterwards.
type disappearFilteringBot struct {
	bridgev2.MatrixAPI
	bridge *bridgev2.Bridge
}

func (bot *disappearFilteringBot) SendMessage(ctx context.Context, roomID id.RoomID, eventType event.Type, content *event.Content, extra *bridgev2.MatrixSendExtra) (*mautrix.RespSendEvent, error) {
	if eventType == event.EventRedaction && bot.shouldKeepDisappearedMessage(ctx, roomID, content) {
		zerolog.Ctx(ctx).Debug().
			Stringer("room_id", roomID).
			Msg("Keeping timer-expired message: dropping disappear redaction")
		return &mautrix.RespSendEvent{}, nil
	}
	return bot.MatrixAPI.SendMessage(ctx, roomID, eventType, content, extra)
}

func (bot *disappearFilteringBot) shouldKeepDisappearedMessage(ctx context.Context, roomID id.RoomID, content *event.Content) bool {
	redaction, ok := content.Parsed.(*event.RedactionEventContent)
	if !ok || redaction.Reason != disappearRedactionReason {
		return false
	}
	portal, err := bot.bridge.GetPortalByMXID(ctx, roomID)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).
			Stringer("room_id", roomID).
			Msg("Failed to load portal for disappear redaction; keeping message")
		// Preserving content beats timer fidelity when in doubt.
		return true
	}
	if portal == nil {
		return true
	}
	meta, ok := portal.Metadata.(*waid.PortalMetadata)
	return !ok || !meta.RespectDisappearingTimer
}
