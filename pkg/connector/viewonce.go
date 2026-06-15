package connector

// SyncContact fork: when a recipient opens a view-once photo/video we sent,
// WhatsApp delivers a "played" receipt (types.ReceiptTypePlayed). We mark the
// bridged message with an m.replace edit carrying the
// com.synccontact.whatsapp.view_once_viewed msgtype, mirroring the "Deleted on
// WhatsApp" / "Deleted by timer" markers: the original media event stays
// untouched and the SyncContact web timeline restores it from the target event,
// flipping the view-once badge to "Viewed". Keep the msgtype in sync with
// apps/web/containers/Chat/utils/viewOnceMedia.js.
//
// The edit is sent directly through the bridge bot instead of bridgev2's
// RemoteEdit flow on purpose: our outgoing messages are authored by the real
// Matrix user, and bridgev2 refuses edits whose sender doesn't match the
// original message's sender. A bot-authored m.replace sidesteps that (same
// approach as the disappearing-timer marker in
// cmd/mautrix-whatsapp/disappearfilter.go), and the SyncContact web applies the
// edit regardless of who sent it.

import (
	"context"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-whatsapp/pkg/waid"
)

const WhatsAppViewOnceViewedField = "com.synccontact.whatsapp.view_once_viewed"

const whatsAppViewOnceViewedMsgType = event.MessageType(WhatsAppViewOnceViewedField)

const whatsAppViewOnceViewedFallbackBody = "View once message opened."

// markViewOnceViewed marks every view-once message a "played" receipt targets as
// opened. Best-effort: targets that aren't found, aren't view-once, or are
// already marked are skipped, and a failed edit just leaves the message unmarked.
func (wa *WhatsAppClient) markViewOnceViewed(ctx context.Context, evt *events.Receipt, messageSender types.JID) bool {
	portal, err := wa.Main.Bridge.GetExistingPortalByKey(ctx, wa.makeWAPortalKey(evt.Chat))
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to load portal for view-once played receipt")
		return true
	}
	if portal == nil || portal.MXID == "" {
		return true
	}
	for _, msgID := range evt.MessageIDs {
		targetID := waid.MakeMessageID(evt.Chat, messageSender, msgID)
		parts, err := wa.Main.Bridge.DB.Message.GetAllPartsByID(ctx, portal.Receiver, targetID)
		if err != nil {
			zerolog.Ctx(ctx).Err(err).
				Str("target_message_id", string(targetID)).
				Msg("Failed to look up played message")
			continue
		}
		if len(parts) == 0 {
			continue
		}
		meta, ok := parts[0].Metadata.(*waid.MessageMetadata)
		if !ok || !meta.IsViewOnce || meta.ViewOnceViewed {
			// "played" also fires for voice notes; only mark view-once media.
			continue
		}
		if !wa.sendViewOnceViewedEdit(ctx, portal.MXID, parts[0].MXID) {
			continue
		}
		meta.ViewOnceViewed = true
		if err := wa.Main.Bridge.DB.Message.Update(ctx, parts[0]); err != nil {
			zerolog.Ctx(ctx).Err(err).Msg("Failed to persist view-once viewed flag")
		}
	}
	return true
}

// sendViewOnceViewedEdit sends the bot-authored marker edit for one message.
func (wa *WhatsAppClient) sendViewOnceViewedEdit(ctx context.Context, roomID id.RoomID, target id.EventID) bool {
	marker := &event.MessageEventContent{
		MsgType: whatsAppViewOnceViewedMsgType,
		Body:    whatsAppViewOnceViewedFallbackBody,
	}
	editContent := &event.MessageEventContent{
		MsgType:    whatsAppViewOnceViewedMsgType,
		Body:       "* " + whatsAppViewOnceViewedFallbackBody,
		NewContent: marker,
		RelatesTo: &event.RelatesTo{
			Type:    event.RelReplace,
			EventID: target,
		},
	}
	resp, err := wa.Main.Bridge.Bot.SendMessage(ctx, roomID, event.EventMessage, &event.Content{Parsed: editContent}, nil)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).
			Stringer("target_event_id", target).
			Msg("Failed to send view-once viewed marker edit")
		return false
	}
	zerolog.Ctx(ctx).Debug().
		Stringer("target_event_id", target).
		Stringer("marker_event_id", resp.EventID).
		Msg("Marked view-once message as viewed")
	return true
}
