package msgconv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rs/zerolog"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix/event"
)

// TrelloCardsMsgType is the custom msgtype the synccontact web app uses when
// a user shares Trello cards in a chat. The same string is also the key of
// the raw content field that carries the card details.
const TrelloCardsMsgType = "com.synccontact.trello.cards"

type TrelloCardParent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type TrelloCard struct {
	ID       string           `json:"id"`
	Name     string           `json:"name"`
	URL      string           `json:"url"`
	ShortURL string           `json:"short_url"`
	Closed   bool             `json:"closed"`
	Board    TrelloCardParent `json:"board"`
	List     TrelloCardParent `json:"list"`
}

type TrelloCardsMeta struct {
	Version int          `json:"version"`
	Cards   []TrelloCard `json:"cards"`
}

func (card *TrelloCard) getPreferredURL() string {
	if card.ShortURL != "" {
		return card.ShortURL
	}
	return card.URL
}

// getLocationLine renders where the card lives, e.g. "☣️ ChronosLocalDev · Done 🎉".
func (card *TrelloCard) getLocationLine() string {
	parts := make([]string, 0, 2)
	if card.Board.Name != "" {
		parts = append(parts, card.Board.Name)
	}
	if card.List.Name != "" {
		parts = append(parts, card.List.Name)
	}
	return strings.Join(parts, " · ")
}

// getSharedAttachmentCaption returns the user-typed caption of a shared
// attachment message. The web app puts either the user's text or a generated
// fallback ("Shared Trello card: …", "Shared contact: …") in the body, so the
// body only counts as a caption when it differs from the fallback.
func getSharedAttachmentCaption(body, fallbackBody string) string {
	if body == fallbackBody {
		return ""
	}
	return body
}

// getTrelloCardsFallbackBody mirrors getFallbackForCards in the web app's
// trelloAttachments.js, which generates the body when the user typed nothing.
func getTrelloCardsFallbackBody(cards []TrelloCard) string {
	names := make([]string, 0, len(cards))
	for _, card := range cards {
		if card.Name != "" {
			names = append(names, card.Name)
		}
	}
	switch len(names) {
	case 0:
		return "Shared Trello cards"
	case 1:
		return "Shared Trello card: " + names[0]
	default:
		return "Shared Trello cards: " + strings.Join(names, ", ")
	}
}

func parseTrelloCardsMeta(raw map[string]any) (*TrelloCardsMeta, error) {
	rawMeta, ok := raw[TrelloCardsMsgType]
	if !ok {
		return nil, fmt.Errorf("event content has no %s field", TrelloCardsMsgType)
	}
	payload, err := json.Marshal(rawMeta)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Trello cards meta: %w", err)
	}
	var meta TrelloCardsMeta
	err = json.Unmarshal(payload, &meta)
	if err != nil {
		return nil, fmt.Errorf("failed to parse Trello cards meta: %w", err)
	}
	return &meta, nil
}

// formatTrelloCardsText renders the shared cards as WhatsApp-formatted text:
// the card name in bold, the board/list location, and the card link.
func formatTrelloCardsText(cards []TrelloCard) string {
	blocks := make([]string, len(cards))
	for i, card := range cards {
		lines := make([]string, 0, 3)
		name := card.Name
		if card.Closed {
			name += " (archived)"
		}
		lines = append(lines, fmt.Sprintf("📋 *%s*", name))
		if location := card.getLocationLine(); location != "" {
			lines = append(lines, location)
		}
		if url := card.getPreferredURL(); url != "" {
			lines = append(lines, url)
		}
		blocks[i] = strings.Join(lines, "\n")
	}
	return strings.Join(blocks, "\n\n")
}

// constructTrelloCardsMessage converts a shared-Trello-cards Matrix event into
// a WhatsApp text message. A single card additionally gets link preview fields
// so WhatsApp clients render it as a card-style bubble. If the card metadata
// is missing or empty, the plain body text is sent so the message still
// reaches the WhatsApp user.
func (mc *MessageConverter) constructTrelloCardsMessage(
	ctx context.Context,
	content *event.MessageEventContent,
	raw map[string]any,
	contextInfo *waE2E.ContextInfo,
) *waE2E.Message {
	etm := &waE2E.ExtendedTextMessage{
		Text:        proto.String(content.Body),
		ContextInfo: contextInfo,
	}
	meta, err := parseTrelloCardsMeta(raw)
	if err != nil || len(meta.Cards) == 0 {
		zerolog.Ctx(ctx).Warn().Err(err).
			Msg("Trello cards message has no usable card meta, sending plain body text")
		return &waE2E.Message{ExtendedTextMessage: etm}
	}
	text := formatTrelloCardsText(meta.Cards)
	if caption := getSharedAttachmentCaption(content.Body, getTrelloCardsFallbackBody(meta.Cards)); caption != "" {
		text = caption + "\n\n" + text
	}
	etm.Text = proto.String(text)
	if len(meta.Cards) == 1 {
		card := meta.Cards[0]
		if url := card.getPreferredURL(); url != "" {
			etm.MatchedText = proto.String(url)
			etm.Title = proto.String(card.Name)
			if location := card.getLocationLine(); location != "" {
				etm.Description = proto.String(location)
			}
		}
	}
	return &waE2E.Message{ExtendedTextMessage: etm}
}
