// mautrix-whatsapp - A Matrix-WhatsApp puppeting bridge.
//
// Album (com.beeper.gallery) support: a single Matrix gallery event is sent to
// WhatsApp as an AlbumMessage container followed by the individual media, each
// linked back to the container via MessageAssociation(MEDIA_ALBUM) — the same
// shape official clients use, so the recipient renders one grouped album.

package msgconv

import (
	"context"
	"fmt"

	"go.mau.fi/util/ptr"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"
)

// BuildAlbumContainer builds the AlbumMessage stanza that announces an album.
// It only declares how many images and videos follow; the media are sent
// separately and reference this container (see BuildAlbumMediaMessage).
func (mc *MessageConverter) BuildAlbumContainer(
	ctx context.Context,
	images []*event.MessageEventContent,
	replyTo *database.Message,
	portal *bridgev2.Portal,
) *waE2E.Message {
	var imageCount, videoCount uint32
	for _, img := range images {
		if img.MsgType == event.MsgVideo {
			videoCount++
		} else {
			imageCount++
		}
	}
	return &waE2E.Message{
		AlbumMessage: &waE2E.AlbumMessage{
			ExpectedImageCount: ptr.Ptr(imageCount),
			ExpectedVideoCount: ptr.Ptr(videoCount),
			ContextInfo:        mc.generateContextInfo(ctx, replyTo, portal, nil, false),
		},
	}
}

// BuildAlbumMediaMessage uploads one album item and wraps it as an image/video
// message linked to the album container via MessageAssociation(MEDIA_ALBUM).
// `index` preserves the album's display order on the recipient side.
func (mc *MessageConverter) BuildAlbumMediaMessage(
	ctx context.Context,
	client *whatsmeow.Client,
	evt *event.Event,
	content *event.MessageEventContent,
	portal *bridgev2.Portal,
	parentKey *waCommon.MessageKey,
	index int,
) (*waE2E.Message, error) {
	ctx = context.WithValue(ctx, contextKeyClient, client)
	ctx = context.WithValue(ctx, contextKeyPortal, portal)
	uploaded, thumbnail, mime, err := mc.reuploadFileToWhatsApp(ctx, content)
	if err != nil {
		return nil, err
	}
	contextInfo := mc.generateContextInfo(ctx, nil, portal, content.BeeperDisappearingTimer, false)
	message := mc.constructMediaMessage(ctx, content, evt, uploaded, thumbnail, contextInfo, mime)
	if message == nil {
		return nil, fmt.Errorf("%w %s in album item", bridgev2.ErrUnsupportedMediaType, content.MsgType)
	}
	message.MessageContextInfo = &waE2E.MessageContextInfo{
		MessageAssociation: &waE2E.MessageAssociation{
			AssociationType:  waE2E.MessageAssociation_MEDIA_ALBUM.Enum(),
			ParentMessageKey: parentKey,
			MessageIndex:     ptr.Ptr(int32(index)),
		},
	}
	return message, nil
}
