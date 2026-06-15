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

// WhatsAppTimerDeletedField is the msgtype of the marker edit the bridge sends
// instead of redacting a timer-expired message in portals that keep messages
// (the default). It mirrors the manual-deletion marker
// (com.synccontact.whatsapp.deleted, see pkg/connector/messagedeletion.go) so
// the SyncContact timeline restores the original content from the untouched
// target event and badges it "Deleted by timer". Keep in sync with
// apps/web/containers/Chat/utils/whatsappDeletedMessages.js.
const WhatsAppTimerDeletedField = "com.synccontact.whatsapp.timer_deleted"

const whatsAppTimerDeletedMsgType = event.MessageType(WhatsAppTimerDeletedField)

const whatsAppTimerDeletedFallbackBody = "This message was removed by the disappearing message timer."

// disappearFilteringBot wraps the bridge bot so the DisappearLoop's redactions
// of timer-expired messages are turned into a "deleted by timer" marker edit
// instead, keeping the original message in the SyncContact timeline (the
// default). bridgev2 has no per-portal hook for disappearing messages — the
// loop redacts through the bot directly — so the bot is the narrowest
// interception point. The per-room respect_disappearing_timer override
// (waid.PortalMetadata) lets the real redaction through.
type disappearFilteringBot struct {
	bridgev2.MatrixAPI
	bridge *bridgev2.Bridge
}

func (bot *disappearFilteringBot) SendMessage(ctx context.Context, roomID id.RoomID, eventType event.Type, content *event.Content, extra *bridgev2.MatrixSendExtra) (*mautrix.RespSendEvent, error) {
	if eventType == event.EventRedaction {
		if redaction, ok := content.Parsed.(*event.RedactionEventContent); ok &&
			redaction.Reason == disappearRedactionReason &&
			bot.keepsDisappearingMessages(ctx, roomID) {
			return bot.markTimerDeleted(ctx, roomID, redaction.Redacts)
		}
	}
	return bot.MatrixAPI.SendMessage(ctx, roomID, eventType, content, extra)
}

// keepsDisappearingMessages reports whether timer-expired messages should be
// kept (and marked) instead of redacted in this portal. Defaults to true; the
// per-room respect_disappearing_timer override re-enables real deletion. When
// the portal can't be loaded it keeps the message — preserving content beats
// timer fidelity when in doubt.
func (bot *disappearFilteringBot) keepsDisappearingMessages(ctx context.Context, roomID id.RoomID) bool {
	portal, err := bot.bridge.GetPortalByMXID(ctx, roomID)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).
			Stringer("room_id", roomID).
			Msg("Failed to load portal for disappear redaction; keeping message")
		return true
	}
	if portal == nil {
		return true
	}
	meta, ok := portal.Metadata.(*waid.PortalMetadata)
	return !ok || !meta.RespectDisappearingTimer
}

// markTimerDeleted replaces the disappear redaction with an m.replace edit that
// flags the message as timer-deleted, keeping the original event (and its
// content) in the room. Returns a non-nil response so the bridgev2 DisappearLoop
// treats the message as handled and stops tracking it. Marking is best-effort:
// if the edit fails the message simply stays unmarked rather than being lost.
func (bot *disappearFilteringBot) markTimerDeleted(ctx context.Context, roomID id.RoomID, target id.EventID) (*mautrix.RespSendEvent, error) {
	if target == "" {
		return &mautrix.RespSendEvent{}, nil
	}
	marker := &event.MessageEventContent{
		MsgType: whatsAppTimerDeletedMsgType,
		Body:    whatsAppTimerDeletedFallbackBody,
	}
	editContent := &event.MessageEventContent{
		MsgType:    whatsAppTimerDeletedMsgType,
		Body:       "* " + whatsAppTimerDeletedFallbackBody,
		NewContent: marker,
		RelatesTo: &event.RelatesTo{
			Type:    event.RelReplace,
			EventID: target,
		},
	}
	resp, err := bot.MatrixAPI.SendMessage(ctx, roomID, event.EventMessage, &event.Content{Parsed: editContent}, nil)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).
			Stringer("target_event_id", target).
			Msg("Failed to mark timer-expired message; leaving it unmarked")
		return &mautrix.RespSendEvent{}, nil
	}
	zerolog.Ctx(ctx).Debug().
		Stringer("target_event_id", target).
		Stringer("marker_event_id", resp.EventID).
		Msg("Marked timer-expired message instead of redacting it")
	return resp, nil
}
