package msgconv

import (
	"context"
	"encoding/json"
	"testing"

	"maunium.net/go/mautrix/event"
)

const exampleTrelloRawContent = `{
	"body": "Shared Trello card: 📊 HR Analytics Dashboard",
	"com.synccontact.trello.cards": {
		"cards": [
			{
				"board": {
					"id": "5cd2d50fe51bb72a4b3221d2",
					"name": "☣️ ChronosLocalDev"
				},
				"closed": false,
				"id": "640b0425a222357641daded0",
				"list": {
					"id": "5d95d43d3c8165618e1f8c58",
					"name": "Done 🎉"
				},
				"name": "📊 HR Analytics Dashboard",
				"short_link": "",
				"short_url": "https://trello.com/c/QTlWT1Dg",
				"source": {
					"label": "Trello",
					"type": "trello"
				},
				"trello_card_id": "640b0425a222357641daded0",
				"url": "https://trello.com/c/QTlWT1Dg/201-%F0%9F%93%8A-hr-analytics-dashboard"
			}
		],
		"version": 1
	},
	"m.text": "Shared Trello card: 📊 HR Analytics Dashboard",
	"msgtype": "com.synccontact.trello.cards"
}`

func parseTestRawContent(t *testing.T, rawJSON string) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal([]byte(rawJSON), &raw); err != nil {
		t.Fatalf("failed to parse test content: %v", err)
	}
	return raw
}

func TestConstructTrelloCardsMessage_SingleCard(t *testing.T) {
	raw := parseTestRawContent(t, exampleTrelloRawContent)
	content := &event.MessageEventContent{
		MsgType: TrelloCardsMsgType,
		Body:    "Shared Trello card: 📊 HR Analytics Dashboard",
	}
	mc := &MessageConverter{}

	msg := mc.constructTrelloCardsMessage(context.Background(), content, raw, nil)
	etm := msg.GetExtendedTextMessage()
	if etm == nil {
		t.Fatal("expected an ExtendedTextMessage")
	}

	expectedText := "📋 *📊 HR Analytics Dashboard*\n" +
		"☣️ ChronosLocalDev · Done 🎉\n" +
		"https://trello.com/c/QTlWT1Dg"
	if etm.GetText() != expectedText {
		t.Errorf("unexpected text:\n%q\nwant:\n%q", etm.GetText(), expectedText)
	}
	if etm.GetMatchedText() != "https://trello.com/c/QTlWT1Dg" {
		t.Errorf("unexpected matched text: %q", etm.GetMatchedText())
	}
	if etm.GetTitle() != "📊 HR Analytics Dashboard" {
		t.Errorf("unexpected preview title: %q", etm.GetTitle())
	}
	if etm.GetDescription() != "☣️ ChronosLocalDev · Done 🎉" {
		t.Errorf("unexpected preview description: %q", etm.GetDescription())
	}
}

func TestConstructTrelloCardsMessage_UserCaption(t *testing.T) {
	raw := parseTestRawContent(t, exampleTrelloRawContent)
	content := &event.MessageEventContent{
		MsgType: TrelloCardsMsgType,
		Body:    "test message",
	}
	mc := &MessageConverter{}

	msg := mc.constructTrelloCardsMessage(context.Background(), content, raw, nil)
	expectedText := "test message\n\n" +
		"📋 *📊 HR Analytics Dashboard*\n" +
		"☣️ ChronosLocalDev · Done 🎉\n" +
		"https://trello.com/c/QTlWT1Dg"
	if text := msg.GetExtendedTextMessage().GetText(); text != expectedText {
		t.Errorf("unexpected text:\n%q\nwant:\n%q", text, expectedText)
	}
}

func TestGetTrelloCardsFallbackBody(t *testing.T) {
	cards := []TrelloCard{{Name: "First"}, {Name: "Second"}}
	if fallback := getTrelloCardsFallbackBody(cards); fallback != "Shared Trello cards: First, Second" {
		t.Errorf("unexpected multi-card fallback: %q", fallback)
	}
	if fallback := getTrelloCardsFallbackBody(cards[:1]); fallback != "Shared Trello card: First" {
		t.Errorf("unexpected single-card fallback: %q", fallback)
	}
	if fallback := getTrelloCardsFallbackBody(nil); fallback != "Shared Trello cards" {
		t.Errorf("unexpected empty fallback: %q", fallback)
	}
}

func TestFormatTrelloCardsText_MultipleAndArchived(t *testing.T) {
	cards := []TrelloCard{
		{
			Name:     "First card",
			ShortURL: "https://trello.com/c/aaa",
			Board:    TrelloCardParent{Name: "Board A"},
			List:     TrelloCardParent{Name: "Doing"},
		},
		{
			Name:   "Second card",
			URL:    "https://trello.com/c/bbb/2-second-card",
			Closed: true,
			Board:  TrelloCardParent{Name: "Board B"},
		},
	}

	expected := "📋 *First card*\n" +
		"Board A · Doing\n" +
		"https://trello.com/c/aaa" +
		"\n\n" +
		"📋 *Second card (archived)*\n" +
		"Board B\n" +
		"https://trello.com/c/bbb/2-second-card"
	if text := formatTrelloCardsText(cards); text != expected {
		t.Errorf("unexpected text:\n%q\nwant:\n%q", text, expected)
	}
}

func TestConstructTrelloCardsMessage_FallsBackToBody(t *testing.T) {
	content := &event.MessageEventContent{
		MsgType: TrelloCardsMsgType,
		Body:    "Shared Trello card: 📊 HR Analytics Dashboard",
	}
	mc := &MessageConverter{}

	for name, raw := range map[string]map[string]any{
		"missing meta": {"msgtype": string(TrelloCardsMsgType)},
		"empty cards":  {TrelloCardsMsgType: map[string]any{"cards": []any{}, "version": 1}},
		"invalid meta": {TrelloCardsMsgType: "not an object"},
	} {
		t.Run(name, func(t *testing.T) {
			msg := mc.constructTrelloCardsMessage(context.Background(), content, raw, nil)
			if text := msg.GetExtendedTextMessage().GetText(); text != content.Body {
				t.Errorf("expected fallback to body, got %q", text)
			}
			if msg.GetExtendedTextMessage().GetMatchedText() != "" {
				t.Error("expected no link preview on fallback")
			}
		})
	}
}
