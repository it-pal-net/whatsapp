package msgconv

import (
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
)

func TestJoinContactVcards(t *testing.T) {
	firstVcard := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Asep\r\nEND:VCARD"
	secondVcard := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:Budi\r\nEND:VCARD"

	data := joinContactVcards([]*waE2E.ContactMessage{
		{DisplayName: proto.String("Asep"), Vcard: proto.String(firstVcard + "\r\n")},
		{DisplayName: proto.String("No vcard")},
		{DisplayName: proto.String("Budi"), Vcard: proto.String(secondVcard)},
	})

	expected := firstVcard + "\r\n" + secondVcard + "\r\n"
	if string(data) != expected {
		t.Errorf("unexpected joined vcards:\n%q\nwant:\n%q", data, expected)
	}
}

func TestJoinContactVcards_Empty(t *testing.T) {
	if data := joinContactVcards(nil); data != nil {
		t.Errorf("expected nil for no contacts, got %q", data)
	}
	if data := joinContactVcards([]*waE2E.ContactMessage{
		{DisplayName: proto.String("No vcard")},
		{Vcard: proto.String("  \r\n")},
	}); data != nil {
		t.Errorf("expected nil for contacts without vcards, got %q", data)
	}
}

func TestGetContactsArrayDisplayName(t *testing.T) {
	withName := &waE2E.ContactsArrayMessage{
		DisplayName: proto.String("Asep and 2 other contacts"),
		Contacts:    make([]*waE2E.ContactMessage, 3),
	}
	if name := getContactsArrayDisplayName(withName); name != "Asep and 2 other contacts" {
		t.Errorf("unexpected display name: %q", name)
	}

	withoutName := &waE2E.ContactsArrayMessage{
		DisplayName: proto.String("  "),
		Contacts:    make([]*waE2E.ContactMessage, 3),
	}
	if name := getContactsArrayDisplayName(withoutName); name != "3 contacts" {
		t.Errorf("unexpected fallback display name: %q", name)
	}
}
