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
	"context"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

// convertVcardToFilePart uploads raw vCard data and wraps it in the m.file
// message the web timeline renders as a contact card.
func (mc *MessageConverter) convertVcardToFilePart(ctx context.Context, displayName string, data []byte) *bridgev2.ConvertedMessagePart {
	fileName := fmt.Sprintf("%s.vcf", displayName)
	const mimeType = "text/vcard"

	mxc, file, err := getIntent(ctx).UploadMedia(ctx, getPortal(ctx).MXID, data, fileName, mimeType)
	if err != nil {
		zerolog.Ctx(ctx).Err(err).Msg("Failed to reupload WhatsApp contact message")
		return &bridgev2.ConvertedMessagePart{
			Type: event.EventMessage,
			Content: &event.MessageEventContent{
				MsgType: event.MsgNotice,
				Body:    "Failed to reupload vcard",
			},
		}
	}

	return &bridgev2.ConvertedMessagePart{
		Type: event.EventMessage,
		Content: &event.MessageEventContent{
			Body:     fileName,
			FileName: fileName,
			URL:      mxc,
			Info: &event.FileInfo{
				MimeType: mimeType,
				Size:     len(data),
			},
			File:    file,
			MsgType: event.MsgFile,
		},
		Extra: make(map[string]any),
	}
}

func (mc *MessageConverter) convertContactMessage(ctx context.Context, msg *waE2E.ContactMessage) (*bridgev2.ConvertedMessagePart, *waE2E.ContextInfo) {
	part := mc.convertVcardToFilePart(ctx, msg.GetDisplayName(), []byte(msg.GetVcard()))
	return part, msg.GetContextInfo()
}

// joinContactVcards concatenates the vCards of a contacts array message into a
// single .vcf stream (RFC 6350 allows multiple VCARD blocks per file). Returns
// nil when no contact carries a vCard.
func joinContactVcards(contacts []*waE2E.ContactMessage) []byte {
	vcards := make([]string, 0, len(contacts))
	for _, contact := range contacts {
		if vcard := strings.TrimSpace(contact.GetVcard()); vcard != "" {
			vcards = append(vcards, vcard)
		}
	}
	if len(vcards) == 0 {
		return nil
	}
	return []byte(strings.Join(vcards, "\r\n") + "\r\n")
}

// getContactsArrayDisplayName mirrors WhatsApp's own naming ("Alice and 2
// other contacts") with a count-based fallback, and doubles as the .vcf
// filename.
func getContactsArrayDisplayName(msg *waE2E.ContactsArrayMessage) string {
	if displayName := strings.TrimSpace(msg.GetDisplayName()); displayName != "" {
		return displayName
	}
	return fmt.Sprintf("%d contacts", len(msg.GetContacts()))
}

func (mc *MessageConverter) convertContactsArrayMessage(ctx context.Context, msg *waE2E.ContactsArrayMessage) (*bridgev2.ConvertedMessagePart, *waE2E.ContextInfo) {
	data := joinContactVcards(msg.GetContacts())
	if data == nil {
		return &bridgev2.ConvertedMessagePart{
			Type: event.EventMessage,
			Content: &event.MessageEventContent{
				MsgType: event.MsgNotice,
				Body:    "Received a contact array message without any contacts",
			},
		}, msg.GetContextInfo()
	}

	part := mc.convertVcardToFilePart(ctx, getContactsArrayDisplayName(msg), data)
	return part, msg.GetContextInfo()
}
