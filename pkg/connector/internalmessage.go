package connector

import (
	"errors"
	"strings"
	"time"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-whatsapp/pkg/waid"
)

// InternalMessagePrefix marks Matrix messages that must stay on the Matrix
// side of a portal room: they are acknowledged and stored like any other
// message so the timeline keeps working, but they are never sent to WhatsApp.
// The synccontact web timeline uses the same prefix to render them with an
// "internal" badge.
const InternalMessagePrefix = "!"

var ErrInternalEditOfRelayedMessage = bridgev2.WrapErrorInStatus(
	errors.New("this message was already sent to WhatsApp and can't be edited into an internal note"),
).WithErrorAsMessage().WithIsCertain(true).WithSendNotice(true).WithErrorReason(event.MessageStatusUnsupported)

func isInternalMessage(content *event.MessageEventContent) bool {
	if content == nil {
		return false
	}
	switch content.MsgType {
	case event.MsgText, event.MsgNotice, event.MsgEmote:
		return strings.HasPrefix(content.Body, InternalMessagePrefix)
	default:
		return false
	}
}

func isInternalDBMessage(msg *database.Message) bool {
	if msg == nil {
		return false
	}
	meta, ok := msg.Metadata.(*waid.MessageMetadata)
	return ok && meta.Internal
}

// handleInternalMatrixMessage stores an internal note without contacting
// WhatsApp. The fake message ID can never collide with or be parsed as a real
// WhatsApp message ID, so no echo or receipt handling will ever match it.
func (wa *WhatsAppClient) handleInternalMatrixMessage(msg *bridgev2.MatrixMessage) (*bridgev2.MatrixMessageResponse, error) {
	chatJID, err := waid.ParsePortalID(msg.Portal.ID)
	if err != nil {
		return nil, err
	}
	timestamp := time.UnixMilli(msg.Event.Timestamp)
	return &bridgev2.MatrixMessageResponse{
		DB: &database.Message{
			ID:        waid.MakeFakeMessageID(chatJID, wa.JID, "internal-"+string(msg.Event.ID)),
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
