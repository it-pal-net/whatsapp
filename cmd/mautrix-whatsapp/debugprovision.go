package main

// Development-only injection endpoint. Unlike customprovision.go (which serves
// production fork endpoints on the authenticated provisioning Router), this
// handler is registered on the raw appservice router behind DebugAuthMiddleware
// (shared-secret only, no Matrix user needed) and ONLY when the
// SYNCCONTACT_DEBUG_INBOUND env var is set — see registerDebugInbound in main.go.
//
// It fabricates inbound WhatsApp messages and feeds them through the bridge's
// real inbound handler so the SyncContact chat stack sees a genuine new
// conversation (portal creation, discovery command, timeline events) without a
// real phone sending anything. See pkg/connector/debuginject.go.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/hlog"
	"go.mau.fi/util/exhttp"
	"go.mau.fi/whatsmeow/types"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2/matrix"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-whatsapp/pkg/connector"
)

type debugInboundMessage struct {
	Type      string `json:"type"`
	Body      string `json:"body"`
	ImageB64  string `json:"image_base64"`
	ImageMime string `json:"image_mime"`
}

type debugInboundRequest struct {
	Phone    string                `json:"phone"`
	PushName string                `json:"push_name"`
	Messages []debugInboundMessage `json:"messages"`
}

// debugInjectInbound handles POST /_synccontact/debug/inbound/{login_id}.
func debugInjectInbound(w http.ResponseWriter, r *http.Request) {
	loginID := networkid.UserLoginID(r.PathValue("login_id"))
	if loginID == "" {
		mautrix.MInvalidParam.WithMessage("login_id is required").Write(w)
		return
	}

	login, err := m.Bridge.GetExistingUserLoginByID(r.Context(), loginID)
	if err != nil {
		hlog.FromRequest(r).Err(err).Str("login_id", string(loginID)).Msg("Failed to load login for debug inject")
		matrix.RespondWithError(w, err, "Internal error loading login")
		return
	} else if login == nil {
		mautrix.MNotFound.WithMessage("Login not found").Write(w)
		return
	}

	waClient, ok := login.Client.(*connector.WhatsAppClient)
	if !ok || waClient.Client == nil {
		mautrix.MForbidden.WithMessage("Login has no connected WhatsApp client").Write(w)
		return
	}

	var req debugInboundRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		mautrix.MBadJSON.WithMessage("Invalid JSON body").Write(w)
		return
	}

	spec := connector.FakeInboundSpec{
		Phone:    req.Phone,
		PushName: req.PushName,
		Messages: make([]connector.FakeInboundMessage, 0, len(req.Messages)),
	}
	for i, msg := range req.Messages {
		fake := connector.FakeInboundMessage{Type: msg.Type, Body: msg.Body, ImageMime: msg.ImageMime}
		if msg.ImageB64 != "" {
			data, decErr := base64.StdEncoding.DecodeString(msg.ImageB64)
			if decErr != nil {
				mautrix.MInvalidParam.WithMessage(fmt.Sprintf("message %d: invalid image_base64", i)).Write(w)
				return
			}
			fake.ImageData = data
		}
		spec.Messages = append(spec.Messages, fake)
	}

	ids, err := waClient.InjectFakeInbound(r.Context(), spec)
	if err != nil {
		hlog.FromRequest(r).Err(err).
			Str("login_id", string(loginID)).
			Str("phone", req.Phone).
			Msg("Failed to inject synthetic inbound message")
		matrix.RespondWithError(w, err, "Failed to inject synthetic inbound message")
		return
	}

	exhttp.WriteJSONResponse(w, http.StatusOK, map[string]any{"ok": true, "message_ids": ids})
}

// debugResyncContacts handles POST /_synccontact/debug/resync-contacts/{login_id}.
//
// It force-fetches the WhatsApp critical_unblock_low app-state collection (the
// address book) for the login and repaints ghost displaynames from it. This
// recovers saved-contact names when a login never synced that collection, which
// otherwise leaves every DM portal named by phone number. See
// pkg/connector/userinfo.go (ResyncAppStateContacts). The login owner mxid on
// this deployment is virtual (no real Matrix account), so the equivalent
// `!wa sync appstate` management command cannot be issued in a room — this
// shared-secret endpoint is the only trigger.
func debugResyncContacts(w http.ResponseWriter, r *http.Request) {
	loginID := networkid.UserLoginID(r.PathValue("login_id"))
	if loginID == "" {
		mautrix.MInvalidParam.WithMessage("login_id is required").Write(w)
		return
	}

	login, err := m.Bridge.GetExistingUserLoginByID(r.Context(), loginID)
	if err != nil {
		hlog.FromRequest(r).Err(err).Str("login_id", string(loginID)).Msg("Failed to load login for debug resync")
		matrix.RespondWithError(w, err, "Internal error loading login")
		return
	} else if login == nil {
		mautrix.MNotFound.WithMessage("Login not found").Write(w)
		return
	}

	waClient, ok := login.Client.(*connector.WhatsAppClient)
	if !ok || waClient.Client == nil {
		mautrix.MForbidden.WithMessage("Login has no connected WhatsApp client").Write(w)
		return
	}

	// Optional body: {"jids": ["554199342564", ...]}. When present, do a live
	// user-info (usync) fetch for those JIDs — this recovers verified BUSINESS
	// names for contacts that never messaged (the app-state address book alone
	// can't). Bare numbers are treated as s.whatsapp.net JIDs. When absent, fall
	// back to the full contact app-state resync.
	var req struct {
		JIDs []string `json:"jids"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	if len(req.JIDs) > 0 {
		jids := make([]types.JID, 0, len(req.JIDs))
		for _, raw := range req.JIDs {
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "+"))
			if raw == "" {
				continue
			}
			if !strings.ContainsRune(raw, '@') {
				raw = raw + "@" + types.DefaultUserServer
			}
			jid, parseErr := types.ParseJID(raw)
			if parseErr != nil {
				mautrix.MInvalidParam.WithMessage(fmt.Sprintf("invalid jid %q: %v", raw, parseErr)).Write(w)
				return
			}
			jids = append(jids, jid)
		}
		results, err := waClient.ResyncGhostNames(r.Context(), jids)
		if err != nil {
			hlog.FromRequest(r).Err(err).Str("login_id", string(loginID)).Msg("Failed to resync ghost names")
			matrix.RespondWithError(w, err, "Failed to resync ghost names")
			return
		}
		exhttp.WriteJSONResponse(w, http.StatusOK, map[string]any{"ok": true, "results": results})
		return
	}

	if err := waClient.ResyncAppStateContacts(r.Context(), nil); err != nil {
		hlog.FromRequest(r).Err(err).
			Str("login_id", string(loginID)).
			Msg("Failed to resync contacts app state")
		matrix.RespondWithError(w, err, "Failed to resync contacts app state")
		return
	}

	exhttp.WriteJSONResponse(w, http.StatusOK, map[string]any{"ok": true})
}
