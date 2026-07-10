package connector

import (
	"context"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"

	"go.mau.fi/mautrix-whatsapp/pkg/waid"
)

// Debug portals are fabricated by the inbound injector (debuginject.go /
// debugprovision.go, gated on SYNCCONTACT_DEBUG_INBOUND). They are real Matrix
// rooms on a real WhatsApp login, so every Matrix->WhatsApp handler must treat
// them as no-network: the message is stored/acknowledged on the Matrix side so
// the timeline behaves normally, but nothing is ever relayed to the live
// WhatsApp network (which would message the synthetic — usually random — phone
// number for real). See PortalMetadata.SyncContactDebug.

// portalIsSyncContactDebug reports whether a portal must never talk to WhatsApp.
// The durable source of truth is the stamped metadata flag; the in-memory JID
// set is a belt-and-suspenders for the brief window around creation before the
// stamp is persisted (and is harmless after a restart clears it, since the
// metadata flag survives).
func (wa *WhatsAppClient) portalIsSyncContactDebug(portal *bridgev2.Portal) bool {
	if portal == nil {
		return false
	}
	if meta, ok := portal.Metadata.(*waid.PortalMetadata); ok && meta.SyncContactDebug {
		return true
	}
	if jid, err := waid.ParsePortalID(portal.ID); err == nil {
		return wa.debugInboundJIDs.Has(jid)
	}
	return false
}

// stampSyncContactDebug is an ExtraUpdater that marks a portal as debug at
// creation/update time. Idempotent; returns whether it changed the metadata so
// the framework only re-saves when needed.
func stampSyncContactDebug(_ context.Context, portal *bridgev2.Portal) bool {
	meta := portal.Metadata.(*waid.PortalMetadata)
	if meta.SyncContactDebug {
		return false
	}
	meta.SyncContactDebug = true
	return true
}

// handleDebugPortalMatrixMessage stores an outbound Matrix message from a debug
// portal without contacting WhatsApp. Mirrors handleInternalMatrixMessage: a
// fake message ID that can never be parsed as (or collide with) a real WhatsApp
// id, so no echo/receipt handling will ever match it, and the message is marked
// Internal so any later reaction/edit/redaction to it also stays Matrix-only.
func (wa *WhatsAppClient) handleDebugPortalMatrixMessage(msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	chatJID, err := waid.ParsePortalID(msg.Portal.ID)
	if err != nil {
		return nil, err
	}
	timestamp := time.UnixMilli(msg.Event.Timestamp)
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        waid.MakeFakeMessageID(chatJID, wa.JID, "scdebug-"+string(msg.Event.ID)),
			SenderID:  waid.MakeUserID(wa.JID),
			Timestamp: timestamp,
			Metadata: &waid.MessageMetadata{
				SenderDeviceID: wa.JID.Device,
				Internal:       true,
			},
		},
		StreamOrder: timestamp.Unix(),
	}, nil
}
