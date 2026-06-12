package msgconv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/event"
)

// AddressbookContactsMsgType is the custom msgtype the synccontact web app
// uses when a user shares addressbook contacts in a chat. The same string is
// also the key of the raw content field that carries the contact details.
const AddressbookContactsMsgType = "com.synccontact.addressbook.contacts"

type AddressbookContactField struct {
	Label string `json:"label"`
	Value string `json:"value"`
}

type AddressbookContact struct {
	ID           string                    `json:"id"`
	DisplayName  string                    `json:"display_name"`
	Organization string                    `json:"organization"`
	Title        string                    `json:"title"`
	Emails       []AddressbookContactField `json:"emails"`
	Phones       []AddressbookContactField `json:"phones"`
}

type AddressbookContactsMeta struct {
	Version  int                  `json:"version"`
	Contacts []AddressbookContact `json:"contacts"`
}

func (contact *AddressbookContact) getDisplayName() string {
	if contact.DisplayName != "" {
		return contact.DisplayName
	}
	if len(contact.Phones) > 0 && contact.Phones[0].Value != "" {
		return contact.Phones[0].Value
	}
	if len(contact.Emails) > 0 && contact.Emails[0].Value != "" {
		return contact.Emails[0].Value
	}
	return "Unnamed contact"
}

func escapeVCardValue(value string) string {
	return strings.NewReplacer(
		`\`, `\\`,
		";", `\;`,
		",", `\,`,
		"\r\n", `\n`,
		"\n", `\n`,
		"\r", `\n`,
	).Replace(value)
}

// formatVCardTypeParam turns a free-text email/phone label into a safe vCard
// TYPE parameter value, or returns an empty string if nothing usable is left.
func formatVCardTypeParam(label string) string {
	var b strings.Builder
	for _, char := range label {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '-' {
			b.WriteRune(char)
		}
	}
	return b.String()
}

func writeVCardLabeledValues(b *strings.Builder, property string, fields []AddressbookContactField) {
	for _, field := range fields {
		if field.Value == "" {
			continue
		}
		b.WriteString(property)
		if param := formatVCardTypeParam(field.Label); param != "" {
			fmt.Fprintf(b, ";TYPE=%s", param)
		}
		fmt.Fprintf(b, ":%s\r\n", escapeVCardValue(field.Value))
	}
}

func (contact *AddressbookContact) toVCard() string {
	name := escapeVCardValue(contact.getDisplayName())
	var b strings.Builder
	b.WriteString("BEGIN:VCARD\r\nVERSION:3.0\r\n")
	fmt.Fprintf(&b, "N:;%s;;;\r\n", name)
	fmt.Fprintf(&b, "FN:%s\r\n", name)
	if contact.Organization != "" {
		fmt.Fprintf(&b, "ORG:%s\r\n", escapeVCardValue(contact.Organization))
	}
	if contact.Title != "" {
		fmt.Fprintf(&b, "TITLE:%s\r\n", escapeVCardValue(contact.Title))
	}
	writeVCardLabeledValues(&b, "TEL", contact.Phones)
	writeVCardLabeledValues(&b, "EMAIL", contact.Emails)
	b.WriteString("END:VCARD")
	return b.String()
}

// getAddressbookContactsFallbackBody mirrors getFallbackForContacts in the web
// app's contactAttachments.js, which generates the body when the user typed
// nothing.
func getAddressbookContactsFallbackBody(contacts []AddressbookContact) string {
	names := make([]string, 0, len(contacts))
	for _, contact := range contacts {
		if contact.DisplayName != "" {
			names = append(names, contact.DisplayName)
		}
	}
	switch len(names) {
	case 0:
		return "Shared contacts"
	case 1:
		return "Shared contact: " + names[0]
	default:
		return "Shared contacts: " + strings.Join(names, ", ")
	}
}

func parseAddressbookContactsMeta(raw map[string]any) (*AddressbookContactsMeta, error) {
	rawMeta, ok := raw[AddressbookContactsMsgType]
	if !ok {
		return nil, fmt.Errorf("event content has no %s field", AddressbookContactsMsgType)
	}
	payload, err := json.Marshal(rawMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal addressbook contacts meta: %w", err)
	}
	var meta AddressbookContactsMeta
	err = json.Unmarshal(payload, &meta)
	if err != nil {
		return nil, fmt.Errorf("failed to parse addressbook contacts meta: %w", err)
	}
	return &meta, nil
}

// constructAddressbookContactsMessage converts a shared-contacts Matrix event
// into a native WhatsApp contact (vCard) message, so the recipient gets a real
// contact card they can open or save. Multiple contacts become a contacts
// array message. If the contact metadata is missing or empty, the plain body
// text is sent so the message still reaches the WhatsApp user.
func (mc *MessageConverter) constructAddressbookContactsMessage(
	ctx context.Context,
	content *event.MessageEventContent,
	raw map[string]any,
	contextInfo *waE2E.ContextInfo,
) *waE2E.Message {
	meta, err := parseAddressbookContactsMeta(raw)
	if err != nil || len(meta.Contacts) == 0 {
		zerolog.Ctx(ctx).Warn().Err(err).
			Msg("Addressbook contacts message has no usable contact meta, sending plain body text")
		return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
			Text:        proto.String(content.Body),
			ContextInfo: contextInfo,
		}}
	}
	contactMessages := make([]*waE2E.ContactMessage, len(meta.Contacts))
	for i, contact := range meta.Contacts {
		contactMessages[i] = &waE2E.ContactMessage{
			DisplayName: proto.String(contact.getDisplayName()),
			Vcard:       proto.String(contact.toVCard()),
		}
	}
	if len(contactMessages) == 1 {
		contactMessages[0].ContextInfo = contextInfo
		return &waE2E.Message{ContactMessage: contactMessages[0]}
	}
	return &waE2E.Message{ContactsArrayMessage: &waE2E.ContactsArrayMessage{
		DisplayName: proto.String(fmt.Sprintf("%d contacts", len(contactMessages))),
		Contacts:    contactMessages,
		ContextInfo: contextInfo,
	}}
}

// AddressbookContactsCaptionToWhatsApp returns a follow-up text message
// carrying the user-typed caption of a shared-contacts event, or nil when
// there is no caption. WhatsApp contact messages can't carry text, so the
// caption has to be sent as a separate message after the contact cards.
func (mc *MessageConverter) AddressbookContactsCaptionToWhatsApp(
	ctx context.Context,
	content *event.MessageEventContent,
	raw map[string]any,
	portal *bridgev2.Portal,
) *waE2E.Message {
	meta, err := parseAddressbookContactsMeta(raw)
	if err != nil || len(meta.Contacts) == 0 {
		// The fallback path already sent the body as the message text.
		return nil
	}
	caption := getSharedAttachmentCaption(content.Body, getAddressbookContactsFallbackBody(meta.Contacts))
	if caption == "" {
		return nil
	}
	return &waE2E.Message{ExtendedTextMessage: &waE2E.ExtendedTextMessage{
		Text:        proto.String(caption),
		ContextInfo: mc.generateContextInfo(ctx, nil, portal, content.BeeperDisappearingTimer, false),
	}}
}
