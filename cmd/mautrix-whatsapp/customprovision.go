package main

// This file holds SyncContact fork-specific provisioning endpoints served under
// the bridgev2 provisioning API (/_matrix/provision/v3). Unlike legacyprovision.go
// — which reimplements the historical v1 provisioning API for backwards
// compatibility — these handlers are new extensions that the SyncContact chat API
// drives directly (e.g. relay configuration and portal deletion).

import (
	"encoding/json"
	"net/http"

	"github.com/rs/zerolog/hlog"
	"go.mau.fi/util/exhttp"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2/matrix"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"
)

type RelayRequest struct {
	RelayLoginID networkid.UserLoginID `json:"relay_login_id"`
}

func setPortalRelay(w http.ResponseWriter, r *http.Request) {
	roomID := id.RoomID(r.PathValue("roomID"))
	if roomID == "" {
		mautrix.MInvalidParam.WithMessage("Missing room ID").Write(w)
		return
	}

	user := m.Matrix.Provisioning.GetUser(r)
	if user == nil {
		mautrix.MForbidden.WithMessage("Authenticated user not found").Write(w)
		return
	}

	var req RelayRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		mautrix.MBadJSON.WithMessage("Invalid JSON body").Write(w)
		return
	} else if req.RelayLoginID == "" {
		mautrix.MInvalidParam.WithMessage("relay_login_id is required").Write(w)
		return
	}

	relay, err := m.Bridge.GetExistingUserLoginByID(r.Context(), req.RelayLoginID)
	if err != nil {
		hlog.FromRequest(r).Err(err).Str("relay_login_id", string(req.RelayLoginID)).Msg("Failed to load relay login")
		matrix.RespondWithError(w, err, "Internal error loading relay login")
		return
	} else if relay == nil {
		mautrix.MNotFound.WithMessage("Relay login not found").Write(w)
		return
	} else if relay.UserMXID != user.MXID {
		mautrix.MForbidden.WithMessage("Relay login is owned by another user").Write(w)
		return
	} else if !relay.Client.IsLoggedIn() {
		mautrix.MForbidden.WithMessage("Relay login is not connected").Write(w)
		return
	}

	portal, err := m.Bridge.GetPortalByMXID(r.Context(), roomID)
	if err != nil {
		hlog.FromRequest(r).Err(err).Str("room_id", string(roomID)).Msg("Failed to load portal by Matrix room ID")
		matrix.RespondWithError(w, err, "Internal error loading portal")
		return
	} else if portal == nil || portal.MXID == "" {
		mautrix.MNotFound.WithMessage("Portal not found").Write(w)
		return
	}

	err = portal.SetRelay(r.Context(), relay)
	if err != nil {
		hlog.FromRequest(r).Err(err).
			Str("room_id", string(roomID)).
			Str("relay_login_id", string(req.RelayLoginID)).
			Msg("Failed to set portal relay")
		matrix.RespondWithError(w, err, "Internal error setting portal relay")
		return
	}

	exhttp.WriteJSONResponse(w, http.StatusOK, map[string]bool{"ok": true})
}

// deletePortal mirrors the `delete-portal` bridge command: it removes the
// portal from the bridge database and tears down the Matrix room. It is the
// HTTP entry point the SyncContact chat API uses instead of the chat command.
func deletePortal(w http.ResponseWriter, r *http.Request) {
	roomID := id.RoomID(r.PathValue("roomID"))
	if roomID == "" {
		mautrix.MInvalidParam.WithMessage("Missing room ID").Write(w)
		return
	}

	user := m.Matrix.Provisioning.GetUser(r)
	if user == nil {
		mautrix.MForbidden.WithMessage("Authenticated user not found").Write(w)
		return
	}

	portal, err := m.Bridge.GetPortalByMXID(r.Context(), roomID)
	if err != nil {
		hlog.FromRequest(r).Err(err).Str("room_id", string(roomID)).Msg("Failed to load portal by Matrix room ID")
		matrix.RespondWithError(w, err, "Internal error loading portal")
		return
	} else if portal == nil || portal.MXID == "" {
		mautrix.MNotFound.WithMessage("Portal not found").Write(w)
		return
	}

	err = portal.Delete(r.Context())
	if err != nil {
		hlog.FromRequest(r).Err(err).Str("room_id", string(roomID)).Msg("Failed to delete portal")
		matrix.RespondWithError(w, err, "Internal error deleting portal")
		return
	}

	err = m.Bridge.Bot.DeleteRoom(r.Context(), portal.MXID, false)
	if err != nil {
		hlog.FromRequest(r).Err(err).Str("room_id", string(roomID)).Msg("Failed to clean up portal room")
		matrix.RespondWithError(w, err, "Internal error cleaning up portal room")
		return
	}

	exhttp.WriteJSONResponse(w, http.StatusOK, map[string]bool{"ok": true})
}
