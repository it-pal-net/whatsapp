package main

import (
	"net/http"

	"github.com/rs/zerolog/hlog"
	"go.mau.fi/util/exhttp"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/bridgev2/matrix"
	"maunium.net/go/mautrix/bridgev2/networkid"

	"go.mau.fi/mautrix-whatsapp/pkg/connector"
)

func provLoginSyncStatus(w http.ResponseWriter, r *http.Request) {
	user := m.Matrix.Provisioning.GetUser(r)
	if user == nil {
		mautrix.MForbidden.WithMessage("Authenticated user not found").Write(w)
		return
	}

	loginID := networkid.UserLoginID(r.PathValue("login_id"))
	if loginID == "" {
		mautrix.MInvalidParam.WithMessage("login_id is required").Write(w)
		return
	}

	waConnector, ok := m.Connector.(*connector.WhatsAppConnector)
	if !ok || waConnector == nil || waConnector.DB == nil {
		hlog.FromRequest(r).Error().Msg("WhatsApp connector database is not available")
		mautrix.MUnknown.WithMessage("Internal error loading sync status").Write(w)
		return
	}

	status, err := connector.GetLoginSyncStatusForLoginID(
		r.Context(),
		m.Bridge,
		waConnector,
		user.MXID,
		loginID,
	)
	if err != nil {
		hlog.FromRequest(r).Err(err).Str("login_id", string(loginID)).Msg("Failed to build login sync status")
		matrix.RespondWithError(w, err, "Internal error loading sync status")
		return
	} else if status == nil {
		mautrix.MNotFound.WithMessage("Login not found").Write(w)
		return
	}

	exhttp.WriteJSONResponse(w, http.StatusOK, status)
}
