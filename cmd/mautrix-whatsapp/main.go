package main

import (
	"maunium.net/go/mautrix/bridgev2/matrix/mxmain"

	"go.mau.fi/mautrix-whatsapp/pkg/connector"
)

// Information to find out exactly which commit the bridge was built from.
// These are filled at build time with the -X linker flag.
var (
	Tag       = "unknown"
	Commit    = "unknown"
	BuildTime = "unknown"
)

var m = mxmain.BridgeMain{
	Name:        "mautrix-whatsapp",
	URL:         "https://github.com/mautrix/whatsapp",
	Description: "A Matrix-WhatsApp puppeting bridge.",
	Version:     "26.05",
	SemCalVer:   true,
	Connector:   &connector.WhatsAppConnector{},
}

func main() {
	m.PostStart = func() {
		// Force-enable batch sending so history backfill works against our
		// standard Synapse. Synapse does not advertise com.beeper.batch_sending in
		// /versions, so the connector leaves BatchSending=false after Start; we flip
		// it here (PostStart runs after the version fetch). The bridge then routes
		// backfill through POST /_matrix/client/unstable/com.beeper.backfill/.../batch_send,
		// which the synccontact_backfill Synapse module implements. See
		// docs/chat/bridges/whatsapp/history-backfill.md
		// The backfill queue (br.RunBackfillQueue) is launched during bridge start —
		// BEFORE this PostStart hook — and checks BatchSending once, bailing because
		// Synapse doesn't advertise it. Since we only flip the capability here, that
		// first launch already gave up, so re-launch the queue. Only do this when we
		// actually flip false->true: on a real batch-sending homeserver the framework
		// already started the queue and re-launching would create a second loop.
		if m.Matrix.Capabilities != nil && !m.Matrix.Capabilities.BatchSending {
			m.Matrix.Capabilities.BatchSending = true
			go m.Bridge.RunBackfillQueue()
		}
		if m.Matrix.Provisioning != nil {
			m.Matrix.Provisioning.Router.HandleFunc("GET /v1/contacts", legacyProvContacts)
			m.Matrix.Provisioning.Router.HandleFunc("GET /v1/resolve_identifier/{number}", legacyProvResolveIdentifier)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/pm/{number}", legacyProvResolveIdentifier)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/debug/appstate/{patch}", provAppStateDebug)
			m.Matrix.Provisioning.Router.HandleFunc("POST /v1/debug/recover-appstate/{patch}", provRecoverAppStateDebug)
			m.Matrix.Provisioning.Router.HandleFunc(
				"PUT /v3/portals/{roomID}/relay",
				setPortalRelay,
			)
			m.Matrix.Provisioning.Router.HandleFunc(
				"DELETE /v3/portals/{roomID}",
				deletePortal,
			)
			m.Matrix.Provisioning.Router.HandleFunc(
				"GET /v3/logins/{login_id}/sync-status",
				provLoginSyncStatus,
			)
			m.Matrix.Provisioning.Router.HandleFunc(
				"DELETE /v3/logins/{login_id}/portals",
				deleteLoginPortals,
			)
		}
	}
	m.InitVersion(Tag, Commit, BuildTime)
	m.Run()
}
