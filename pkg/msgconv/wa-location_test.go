package msgconv

import (
	"context"
	"testing"

	"go.mau.fi/whatsmeow/proto/waE2E"
	"google.golang.org/protobuf/proto"
	"maunium.net/go/mautrix/event"
)

func TestGetLocationName(t *testing.T) {
	if name := getLocationName("Villa Gusku", -8.5016, 115.27373); name != "Villa Gusku" {
		t.Errorf("unexpected name: %q", name)
	}
	if name := getLocationName("", -8.5016, 115.27373); name != "8.5016° S 115.2737° E" {
		t.Errorf("unexpected coordinate fallback: %q", name)
	}
	if name := getLocationName("", 48.2082, -16.3738); name != "48.2082° N 16.3738° W" {
		t.Errorf("unexpected coordinate fallback: %q", name)
	}
}

func TestGetLocationURL(t *testing.T) {
	if url := getLocationURL("https://abnb.me/x", 1, 2); url != "https://abnb.me/x" {
		t.Errorf("unexpected url: %q", url)
	}
	if url := getLocationURL("", -8.5016, 115.27373); url != "https://maps.google.com/?q=-8.50160,115.27373" {
		t.Errorf("unexpected fallback url: %q", url)
	}
}

func TestConvertLiveLocationMessage(t *testing.T) {
	mc := &MessageConverter{}
	part, _ := mc.convertLiveLocationMessage(context.Background(), &waE2E.LiveLocationMessage{
		DegreesLatitude:  proto.Float64(-8.5016),
		DegreesLongitude: proto.Float64(115.27373),
		Caption:          proto.String("On my way"),
	})

	if part.Content.MsgType != event.MsgLocation {
		t.Errorf("unexpected msgtype: %q", part.Content.MsgType)
	}
	if part.Content.GeoURI != "geo:-8.50160,115.27373" {
		t.Errorf("unexpected geo uri: %q", part.Content.GeoURI)
	}
	expectedBody := "Live location: On my way\nhttps://maps.google.com/?q=-8.50160,115.27373"
	if part.Content.Body != expectedBody {
		t.Errorf("unexpected body:\n%q\nwant:\n%q", part.Content.Body, expectedBody)
	}
	if live, _ := part.Extra[LiveLocationExtraField].(bool); !live {
		t.Errorf("expected %s extra flag to be true, got %v", LiveLocationExtraField, part.Extra[LiveLocationExtraField])
	}
}

func TestConvertLiveLocationMessage_NoCaption(t *testing.T) {
	mc := &MessageConverter{}
	part, _ := mc.convertLiveLocationMessage(context.Background(), &waE2E.LiveLocationMessage{
		DegreesLatitude:  proto.Float64(-8.5016),
		DegreesLongitude: proto.Float64(115.27373),
	})

	expectedBody := "Live location: 8.5016° S 115.2737° E\nhttps://maps.google.com/?q=-8.50160,115.27373"
	if part.Content.Body != expectedBody {
		t.Errorf("unexpected body:\n%q\nwant:\n%q", part.Content.Body, expectedBody)
	}
}
