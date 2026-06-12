package msgconv

import (
	"context"
	"testing"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-whatsapp/pkg/waid"
)

const exampleContactRawContent = `{
	"body": "Shared contact: Vladimir Pal",
	"com.synccontact.addressbook.contacts": {
		"contacts": [
			{
				"addressbook_id": "af94fd22-e50c-4ec3-9b84-750c46cfb349",
				"avatar_url": null,
				"contact_id": "79486d5a-9f1c-4e3d-98de-c0eea4ed8455",
				"display_name": "Vladimir Pal",
				"emails": [],
				"id": "79486d5a-9f1c-4e3d-98de-c0eea4ed8455",
				"initials": "VP",
				"organization": "Chronos",
				"phones": [],
				"source": {
					"label": "Chronos",
					"type": "addressbook"
				},
				"title": ""
			}
		],
		"version": 1
	},
	"m.text": "Shared contact: Vladimir Pal",
	"msgtype": "com.synccontact.addressbook.contacts"
}`

func TestConstructAddressbookContactsMessage_SingleContact(t *testing.T) {
	raw := parseTestRawContent(t, exampleContactRawContent)
	content := &event.MessageEventContent{
		MsgType: AddressbookContactsMsgType,
		Body:    "Shared contact: Vladimir Pal",
	}
	mc := &MessageConverter{}

	msg := mc.constructAddressbookContactsMessage(context.Background(), content, raw, nil)
	contactMsg := msg.GetContactMessage()
	if contactMsg == nil {
		t.Fatal("expected a ContactMessage")
	}
	if contactMsg.GetDisplayName() != "Vladimir Pal" {
		t.Errorf("unexpected display name: %q", contactMsg.GetDisplayName())
	}
	expectedVCard := "BEGIN:VCARD\r\n" +
		"VERSION:3.0\r\n" +
		"N:;Vladimir Pal;;;\r\n" +
		"FN:Vladimir Pal\r\n" +
		"ORG:Chronos\r\n" +
		"END:VCARD"
	if contactMsg.GetVcard() != expectedVCard {
		t.Errorf("unexpected vcard:\n%q\nwant:\n%q", contactMsg.GetVcard(), expectedVCard)
	}
}

func TestConstructAddressbookContactsMessage_MultipleContacts(t *testing.T) {
	raw := map[string]any{
		AddressbookContactsMsgType: map[string]any{
			"version": 1,
			"contacts": []any{
				map[string]any{
					"display_name": "Ada; Lovelace",
					"title":        "Engineer",
					"phones": []any{
						map[string]any{"label": "mobile", "value": "+1 555 0100"},
						map[string]any{"label": "", "value": "+1 555 0101"},
					},
					"emails": []any{
						map[string]any{"label": "work email", "value": "ada@example.com"},
					},
				},
				map[string]any{
					"display_name": "",
					"phones": []any{
						map[string]any{"label": "home", "value": "+1 555 0200"},
					},
				},
			},
		},
	}
	mc := &MessageConverter{}

	msg := mc.constructAddressbookContactsMessage(context.Background(), &event.MessageEventContent{}, raw, nil)
	arrayMsg := msg.GetContactsArrayMessage()
	if arrayMsg == nil {
		t.Fatal("expected a ContactsArrayMessage")
	}
	if arrayMsg.GetDisplayName() != "2 contacts" {
		t.Errorf("unexpected display name: %q", arrayMsg.GetDisplayName())
	}
	if len(arrayMsg.GetContacts()) != 2 {
		t.Fatalf("expected 2 contacts, got %d", len(arrayMsg.GetContacts()))
	}

	expectedFirstVCard := "BEGIN:VCARD\r\n" +
		"VERSION:3.0\r\n" +
		"N:;Ada\\; Lovelace;;;\r\n" +
		"FN:Ada\\; Lovelace\r\n" +
		"TITLE:Engineer\r\n" +
		"TEL;TYPE=mobile:+1 555 0100\r\n" +
		"TEL:+1 555 0101\r\n" +
		"EMAIL;TYPE=workemail:ada@example.com\r\n" +
		"END:VCARD"
	if vcard := arrayMsg.GetContacts()[0].GetVcard(); vcard != expectedFirstVCard {
		t.Errorf("unexpected first vcard:\n%q\nwant:\n%q", vcard, expectedFirstVCard)
	}
	// A contact without a display name falls back to its first phone number.
	if name := arrayMsg.GetContacts()[1].GetDisplayName(); name != "+1 555 0200" {
		t.Errorf("unexpected second display name: %q", name)
	}
}

func TestAddressbookContactsCaptionToWhatsApp(t *testing.T) {
	raw := parseTestRawContent(t, exampleContactRawContent)
	portal := &bridgev2.Portal{Portal: &database.Portal{Metadata: &waid.PortalMetadata{}}}
	mc := &MessageConverter{}

	captionMsg := mc.AddressbookContactsCaptionToWhatsApp(context.Background(), &event.MessageEventContent{
		MsgType: AddressbookContactsMsgType,
		Body:    "here is the contact you asked for",
	}, raw, portal)
	if captionMsg == nil {
		t.Fatal("expected a caption message for a user-typed body")
	}
	if text := captionMsg.GetExtendedTextMessage().GetText(); text != "here is the contact you asked for" {
		t.Errorf("unexpected caption text: %q", text)
	}

	for name, body := range map[string]string{
		"fallback body": "Shared contact: Vladimir Pal",
		"empty body":    "",
	} {
		t.Run(name, func(t *testing.T) {
			captionMsg := mc.AddressbookContactsCaptionToWhatsApp(context.Background(), &event.MessageEventContent{
				MsgType: AddressbookContactsMsgType,
				Body:    body,
			}, raw, portal)
			if captionMsg != nil {
				t.Errorf("expected no caption message, got %q", captionMsg.GetExtendedTextMessage().GetText())
			}
		})
	}

	t.Run("missing meta", func(t *testing.T) {
		captionMsg := mc.AddressbookContactsCaptionToWhatsApp(context.Background(), &event.MessageEventContent{
			MsgType: AddressbookContactsMsgType,
			Body:    "some caption",
		}, map[string]any{}, portal)
		if captionMsg != nil {
			t.Error("expected no caption message when meta is missing")
		}
	})
}

func TestConstructAddressbookContactsMessage_FallsBackToBody(t *testing.T) {
	content := &event.MessageEventContent{
		MsgType: AddressbookContactsMsgType,
		Body:    "Shared contact: Vladimir Pal",
	}
	mc := &MessageConverter{}

	for name, raw := range map[string]map[string]any{
		"missing meta":   {"msgtype": string(AddressbookContactsMsgType)},
		"empty contacts": {AddressbookContactsMsgType: map[string]any{"contacts": []any{}, "version": 1}},
		"invalid meta":   {AddressbookContactsMsgType: "not an object"},
	} {
		t.Run(name, func(t *testing.T) {
			msg := mc.constructAddressbookContactsMessage(context.Background(), content, raw, nil)
			if msg.GetContactMessage() != nil || msg.GetContactsArrayMessage() != nil {
				t.Fatal("expected no contact message on fallback")
			}
			if text := msg.GetExtendedTextMessage().GetText(); text != content.Body {
				t.Errorf("expected fallback to body, got %q", text)
			}
		})
	}
}
