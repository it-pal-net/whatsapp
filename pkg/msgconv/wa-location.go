// mautrix-whatsapp - A Matrix-WhatsApp puppeting bridge.
// Copyright (C) 2024 Tulir Asokan
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <https://www.gnu.org/licenses/>.

package msgconv

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"math"
	"net/http"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

// LiveLocationExtraField marks an m.location event as the start of a live
// location share. The bridge only receives the initial position — WhatsApp
// distributes the follow-up updates through a channel whatsmeow doesn't
// deliver — so clients should present the pin as a starting point.
const LiveLocationExtraField = "com.synccontact.live_location"

func getLocationName(name string, lat, lng float64) string {
	if len(name) > 0 {
		return name
	}
	latChar := 'N'
	if lat < 0 {
		latChar = 'S'
	}
	longChar := 'E'
	if lng < 0 {
		longChar = 'W'
	}
	return fmt.Sprintf("%.4f° %c %.4f° %c", math.Abs(lat), latChar, math.Abs(lng), longChar)
}

func getLocationURL(url string, lat, lng float64) string {
	if len(url) > 0 {
		return url
	}
	return fmt.Sprintf("https://maps.google.com/?q=%.5f,%.5f", lat, lng)
}

func attachLocationThumbnail(ctx context.Context, content *event.MessageEventContent, jpegThumbnail []byte) {
	if len(jpegThumbnail) == 0 {
		return
	}
	thumbnailMime := http.DetectContentType(jpegThumbnail)
	thumbnailURL, thumbnailFile, err := getIntent(ctx).UploadMedia(ctx, getPortal(ctx).MXID, jpegThumbnail, "thumb.jpeg", thumbnailMime)
	if err != nil {
		return
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(jpegThumbnail))
	content.Info = &event.FileInfo{
		ThumbnailInfo: &event.FileInfo{
			Size:     len(jpegThumbnail),
			Width:    cfg.Width,
			Height:   cfg.Height,
			MimeType: thumbnailMime,
		},
		ThumbnailURL:  thumbnailURL,
		ThumbnailFile: thumbnailFile,
	}
}

func (mc *MessageConverter) convertLocationMessage(ctx context.Context, msg *waE2E.LocationMessage) (*bridgev2.ConvertedMessagePart, *waE2E.ContextInfo) {
	url := getLocationURL(msg.GetURL(), msg.GetDegreesLatitude(), msg.GetDegreesLongitude())
	name := getLocationName(msg.GetName(), msg.GetDegreesLatitude(), msg.GetDegreesLongitude())

	content := &event.MessageEventContent{
		MsgType:       event.MsgLocation,
		Body:          fmt.Sprintf("Location: %s\n%s\n%s", name, msg.GetAddress(), url),
		Format:        event.FormatHTML,
		FormattedBody: fmt.Sprintf("Location: <a href='%s'>%s</a><br>%s", url, name, msg.GetAddress()),
		GeoURI:        fmt.Sprintf("geo:%.5f,%.5f", msg.GetDegreesLatitude(), msg.GetDegreesLongitude()),
	}
	attachLocationThumbnail(ctx, content, msg.GetJPEGThumbnail())

	return &bridgev2.ConvertedMessagePart{
		Type:    event.EventMessage,
		Content: content,
	}, msg.GetContextInfo()
}

func (mc *MessageConverter) convertLiveLocationMessage(ctx context.Context, msg *waE2E.LiveLocationMessage) (*bridgev2.ConvertedMessagePart, *waE2E.ContextInfo) {
	url := getLocationURL("", msg.GetDegreesLatitude(), msg.GetDegreesLongitude())
	name := getLocationName(msg.GetCaption(), msg.GetDegreesLatitude(), msg.GetDegreesLongitude())

	content := &event.MessageEventContent{
		MsgType:       event.MsgLocation,
		Body:          fmt.Sprintf("Live location: %s\n%s", name, url),
		Format:        event.FormatHTML,
		FormattedBody: fmt.Sprintf("Live location: <a href='%s'>%s</a>", url, name),
		GeoURI:        fmt.Sprintf("geo:%.5f,%.5f", msg.GetDegreesLatitude(), msg.GetDegreesLongitude()),
	}
	attachLocationThumbnail(ctx, content, msg.GetJPEGThumbnail())

	return &bridgev2.ConvertedMessagePart{
		Type:    event.EventMessage,
		Content: content,
		Extra: map[string]any{
			LiveLocationExtraField: true,
		},
	}, msg.GetContextInfo()
}
