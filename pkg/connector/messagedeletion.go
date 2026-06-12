package connector

// SyncContact fork: WhatsApp "delete for everyone" (revoke) does not delete the
// bridged Matrix message by default. Blocked revokes are bridged as a Matrix
// edit that carries WhatsAppDeletedField, so the original event stays in the
// room and the SyncContact web timeline renders a "Deleted on WhatsApp" badge
// instead of applying the edit. Per-portal opt-out lives in
// waid.PortalMetadata.AllowMessageDeletion (set through the
// /v3/portals/{roomID}/settings provisioning endpoint).

import (
	"context"
	"fmt"
	"slices"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/types"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-whatsapp/pkg/waid"
)

// WhatsAppDeletedField is set at the top level of the marker edit's content
// and doubles as the msgtype of its replacement content. Matrix edit handling
// folds the replacement msgtype into the target message wherever the edit is
// applied (live sync and server-bundled aggregations alike), so the SyncContact
// web timeline can always recognize a deleted message by this msgtype and
// restore the original content from the unmodified target event. Keep in sync
// with apps/web/containers/Chat/utils/whatsappDeletedMessages.js.
const WhatsAppDeletedField = "com.synccontact.whatsapp.deleted"

const whatsAppDeletedMsgType = event.MessageType(WhatsAppDeletedField)

const whatsAppDeletedFallbackBody = "This message was deleted on WhatsApp."

// isMessageDeletionAllowed reports whether a WhatsApp revoke may redact the
// bridged Matrix message in this chat. Lookup errors block the deletion: losing
// a revoke marker is recoverable, losing the message content is not.
func (wa *WhatsAppClient) isMessageDeletionAllowed(ctx context.Context, ms types.MessageSource) bool {
	portal, err := wa.Main.Bridge.GetExistingPortalByKey(ctx, wa.getPortalKeyByMessageSource(ms))
	if err != nil {
		zerolog.Ctx(ctx).Err(err).
			Stringer("chat_jid", ms.Chat).
			Msg("Failed to get portal to check message deletion setting, blocking deletion")
		return false
	}
	if portal == nil {
		// No portal means nothing was bridged, so the revoke is a no-op either way.
		return false
	}
	meta, ok := portal.Metadata.(*waid.PortalMetadata)
	return ok && meta.AllowMessageDeletion
}

// convertBlockedRevokeToEdit marks every part of the target message as deleted.
// The fallback content is what non-SyncContact Matrix clients render for the
// edit; the SyncContact timeline ignores it and keeps the original content.
func (evt *WAMessageEvent) convertBlockedRevokeToEdit(existing []*database.Message) (*bridgev2.ConvertedEdit, error) {
	meta := existing[0].Metadata.(*waid.MessageMetadata)
	if slices.Contains(meta.Edits, evt.Info.ID) {
		return nil, fmt.Errorf("%w: revoke already handled", bridgev2.ErrIgnoringRemoteEvent)
	}
	meta.Edits = append(meta.Edits, evt.Info.ID)
	modifiedParts := make([]*bridgev2.ConvertedEditPart, len(existing))
	for i, part := range existing {
		modifiedParts[i] = &bridgev2.ConvertedEditPart{
			Part: part,
			Type: event.EventMessage,
			Content: &event.MessageEventContent{
				MsgType: whatsAppDeletedMsgType,
				Body:    whatsAppDeletedFallbackBody,
			},
			TopLevelExtra: map[string]any{
				WhatsAppDeletedField: true,
			},
		}
	}
	return &bridgev2.ConvertedEdit{
		ModifiedParts: modifiedParts,
	}, nil
}
