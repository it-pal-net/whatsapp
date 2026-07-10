// mautrix-whatsapp - A Matrix-WhatsApp puppeting bridge.
//
// SyncContact fork addition: development-only synthetic inbound injection.

package connector

import (
	"bytes"
	"context"
	"fmt"
	"image"
	// Register the decoders used by image.DecodeConfig to read dimensions off
	// the fixture bytes. These are the formats WhatsApp accepts for photos.
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-whatsapp/pkg/waid"
)

// FakeInboundMessage describes one synthetic inbound WhatsApp message.
// Type is "text" (default) or "image". For images, ImageData holds the raw
// bytes and Body is the caption.
type FakeInboundMessage struct {
	Type      string
	Body      string
	ImageData []byte
	ImageMime string
}

// FakeInboundSpec is a synthetic inbound conversation from a single sender.
// Phone is the sender's number in international format, digits only
// (e.g. "5511998887777"). A phone that has no existing portal reproduces a
// brand-new client landing in the bridge for the first time.
type FakeInboundSpec struct {
	Phone    string
	PushName string
	Messages []FakeInboundMessage
}

// InjectFakeInbound synthesizes inbound WhatsApp events and feeds them through
// handleWAEvent — the exact same entry point whatsmeow's real message callback
// uses (see handleWAEvent in handlewhatsapp.go). Because injection happens at
// that seam, everything downstream runs the real code path: portal creation for
// an unknown chat, ghost puppeting, the Matrix timeline events, and the
// SyncContact discovery command published to Redis. Nothing below the seam can
// tell the message wasn't delivered by WhatsApp.
//
// This is a development/testing affordance reached only through the debug
// provisioning endpoint, which is registered solely when SYNCCONTACT_DEBUG_INBOUND
// is set. It must never be reachable in production.
//
// Images are uploaded to WhatsApp's real media CDN through the live login, so
// the bridge downloads and decrypts them back through the normal media path
// (mediaKey/directPath/url), exactly like a real photo. This therefore requires
// a connected WhatsApp login.
//
// It returns the generated WhatsApp message IDs, one per injected message.
func (wa *WhatsAppClient) InjectFakeInbound(ctx context.Context, spec FakeInboundSpec) ([]string, error) {
	if wa.Client == nil || !wa.IsLoggedIn() {
		return nil, fmt.Errorf("whatsapp login is not connected")
	}
	if spec.Phone == "" {
		return nil, fmt.Errorf("phone is required")
	}
	if len(spec.Messages) == 0 {
		return nil, fmt.Errorf("at least one message is required")
	}
	// A bare phone-number JID (@s.whatsapp.net) routes through phone-number
	// addressing, which avoids the LID store lookups that a real device would
	// have populated. For a 1:1 chat the portal key is derived from Chat, and
	// Chat == Sender == the other party.
	senderJID := types.JID{User: spec.Phone, Server: types.DefaultUserServer}

	// Mark this JID before the synthetic event is queued, so the portal it
	// creates is stamped SyncContactDebug during creation (chatinfo.go) — before
	// discovery or any automation could send a Matrix event into it. Every
	// Matrix->WhatsApp handler then drops the network send for this portal, so a
	// fixture/repro conversation never messages the real number.
	wa.debugInboundJIDs.Add(senderJID)

	// The ghost's display name is resolved from the WhatsApp contact store
	// (GetUserInfo -> Contacts.GetContact), NOT from the message's PushName
	// field. A real inbound message reaches the store through whatsmeow's own
	// pipeline; we bypass that by calling handleWAEvent directly, so without
	// seeding the contact the ghost would show as the bare phone number. Store
	// the push name first so the conversation gets a proper name.
	if spec.PushName != "" {
		if _, _, err := wa.GetStore().Contacts.PutPushName(ctx, senderJID, spec.PushName); err != nil {
			wa.UserLogin.Log.Warn().Err(err).Str("phone", spec.Phone).
				Msg("Failed to seed synthetic contact push name")
		}
	}

	ids := make([]string, 0, len(spec.Messages))
	for i, msg := range spec.Messages {
		waMsg, err := wa.buildFakeInboundMessage(ctx, msg)
		if err != nil {
			return ids, fmt.Errorf("message %d: %w", i, err)
		}
		msgID := wa.Client.GenerateMessageID()
		evt := &events.Message{
			Info: types.MessageInfo{
				MessageSource: types.MessageSource{
					Chat:     senderJID,
					Sender:   senderJID,
					IsFromMe: false,
				},
				ID:        msgID,
				PushName:  spec.PushName,
				Timestamp: time.Now(),
			},
			Message: waMsg,
		}
		if !wa.handleWAEvent(evt) {
			return ids, fmt.Errorf("message %d: bridge did not accept the synthetic event", i)
		}
		ids = append(ids, string(msgID))
	}

	// Re-assert the DM room name out-of-band. The name is applied during room
	// creation, but on a brand-new portal an async ghost resync re-sends
	// m.room.name under a context that is cancelled as room creation returns,
	// leaving name_set=false — so the SyncContact chat list shows the bare phone
	// number even though the room-info panel (which reads the ghost) shows the
	// name. Runs in the background on the bridge's non-cancellable context once
	// the room exists.
	if spec.PushName != "" {
		go wa.ensureInjectedPortalNamed(wa.makeWAPortalKey(senderJID), spec.PushName)
	}

	return ids, nil
}

func (wa *WhatsAppClient) ensureInjectedPortalNamed(key networkid.PortalKey, name string) {
	ctx := wa.Main.Bridge.BackgroundCtx
	var portal *bridgev2.Portal
	for i := 0; i < 60; i++ {
		p, err := wa.Main.Bridge.GetPortalByKey(ctx, key)
		if err == nil && p != nil && p.MXID != "" {
			portal = p
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if portal == nil {
		return
	}
	// Let the creation's own (cancel-prone) metadata writes settle before we
	// assert the final state, so we don't race them.
	time.Sleep(1500 * time.Millisecond)
	// Belt-and-suspenders: ensure the debug flag is persisted even if the
	// creation-time stamp was missed (e.g. the portal already existed). The
	// ExtraUpdater is a no-op when the flag is already set.
	if meta, ok := portal.Metadata.(*waid.PortalMetadata); !ok || !meta.SyncContactDebug {
		portal.UpdateInfo(ctx, &bridgev2.ChatInfo{
			ExtraUpdates: stampSyncContactDebug,
		}, wa.UserLogin, nil, time.Time{})
	}
	if portal.NameSet && portal.Name == name {
		return
	}
	// Force a re-send: updateName skips when Name matches and NameSet is true.
	portal.NameSet = false
	portal.UpdateInfo(ctx, &bridgev2.ChatInfo{Name: &name}, wa.UserLogin, nil, time.Time{})
}

func (wa *WhatsAppClient) buildFakeInboundMessage(ctx context.Context, msg FakeInboundMessage) (*waE2E.Message, error) {
	switch msg.Type {
	case "", "text":
		if msg.Body == "" {
			return nil, fmt.Errorf("text message has an empty body")
		}
		return &waE2E.Message{Conversation: proto.String(msg.Body)}, nil
	case "image":
		if len(msg.ImageData) == 0 {
			return nil, fmt.Errorf("image message has no data")
		}
		mime := msg.ImageMime
		if mime == "" {
			mime = "image/jpeg"
		}
		uploaded, err := wa.Client.Upload(ctx, msg.ImageData, whatsmeow.MediaImage)
		if err != nil {
			return nil, fmt.Errorf("upload image to whatsapp: %w", err)
		}
		var width, height uint32
		if cfg, _, decErr := image.DecodeConfig(bytes.NewReader(msg.ImageData)); decErr == nil {
			width = uint32(cfg.Width)
			height = uint32(cfg.Height)
		}
		return &waE2E.Message{
			ImageMessage: &waE2E.ImageMessage{
				Caption:       proto.String(msg.Body),
				Mimetype:      proto.String(mime),
				Width:         proto.Uint32(width),
				Height:        proto.Uint32(height),
				URL:           proto.String(uploaded.URL),
				DirectPath:    proto.String(uploaded.DirectPath),
				MediaKey:      uploaded.MediaKey,
				FileEncSHA256: uploaded.FileEncSHA256,
				FileSHA256:    uploaded.FileSHA256,
				FileLength:    proto.Uint64(uploaded.FileLength),
			},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported message type %q", msg.Type)
	}
}
