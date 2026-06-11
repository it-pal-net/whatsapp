package connector

import (
	"testing"

	"maunium.net/go/mautrix/bridgev2/database"
	"maunium.net/go/mautrix/event"

	"go.mau.fi/mautrix-whatsapp/pkg/waid"
)

func TestIsInternalMessage(t *testing.T) {
	tests := []struct {
		name    string
		content *event.MessageEventContent
		want    bool
	}{
		{"text with prefix", &event.MessageEventContent{MsgType: event.MsgText, Body: "!note for the team"}, true},
		{"notice with prefix", &event.MessageEventContent{MsgType: event.MsgNotice, Body: "!note"}, true},
		{"emote with prefix", &event.MessageEventContent{MsgType: event.MsgEmote, Body: "!note"}, true},
		{"text without prefix", &event.MessageEventContent{MsgType: event.MsgText, Body: "hello!"}, false},
		{"prefix after whitespace", &event.MessageEventContent{MsgType: event.MsgText, Body: " !note"}, false},
		{"file with prefix in name", &event.MessageEventContent{MsgType: event.MsgFile, Body: "!file.pdf"}, false},
		{"image with prefix in name", &event.MessageEventContent{MsgType: event.MsgImage, Body: "!photo.jpg"}, false},
		{"empty body", &event.MessageEventContent{MsgType: event.MsgText, Body: ""}, false},
		{"nil content", nil, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInternalMessage(tt.content); got != tt.want {
				t.Fatalf("isInternalMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsInternalDBMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  *database.Message
		want bool
	}{
		{"internal metadata", &database.Message{Metadata: &waid.MessageMetadata{Internal: true}}, true},
		{"regular metadata", &database.Message{Metadata: &waid.MessageMetadata{}}, false},
		{"nil message", nil, false},
		{"nil metadata", &database.Message{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isInternalDBMessage(tt.msg); got != tt.want {
				t.Fatalf("isInternalDBMessage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestInternalFakeMessageIDIsNeverParsedAsReal(t *testing.T) {
	chat, err := waid.ParsePortalID("123456789@s.whatsapp.net")
	if err != nil {
		t.Fatalf("failed to parse portal ID: %v", err)
	}
	id := waid.MakeFakeMessageID(chat, chat, "internal-$someEventID")
	if !waid.IsFakeMessageID(id) {
		t.Fatalf("IsFakeMessageID(%q) = false, want true", id)
	}
	if parsed, err := waid.ParseMessageID(id); err == nil {
		t.Fatalf("ParseMessageID(%q) = %v, want error", id, parsed)
	}
}
