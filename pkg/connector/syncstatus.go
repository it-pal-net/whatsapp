package connector

import (
	"context"

	"maunium.net/go/mautrix/bridgev2"
	"maunium.net/go/mautrix/bridgev2/networkid"
	"maunium.net/go/mautrix/id"

	"go.mau.fi/mautrix-whatsapp/pkg/connector/wadb"
	"go.mau.fi/mautrix-whatsapp/pkg/waid"
)

const (
	HistorySyncPhaseReady            = "ready"
	HistorySyncPhaseReceivingChunks    = "receiving_chunks"
	HistorySyncPhaseWaitingDispatch    = "waiting_dispatch"
	HistorySyncPhaseCreatingPortals    = "creating_portals"
)

type LoginSessionStatus struct {
	BridgeState        string `json:"bridge_state"`
	WhatsAppConnected  bool   `json:"whatsapp_connected"`
}

type LoginHistorySyncStatus struct {
	Phase                     string `json:"phase"`
	PortalsNeedCreating       bool   `json:"portals_need_creating"`
	PendingNotificationCount  int    `json:"pending_notification_count"`
	PendingConversationCount  int    `json:"pending_conversation_count"`
	LoggedInAt                *int64 `json:"logged_in_at,omitempty"`
}

type LoginSyncStatusResponse struct {
	LoginID            string                 `json:"login_id"`
	Session            LoginSessionStatus     `json:"session"`
	HistorySync        LoginHistorySyncStatus `json:"history_sync"`
	ReadyForDiscovery  bool                   `json:"ready_for_discovery"`
}

func historySyncPhase(
	pendingNotifications int,
	pendingConversations int,
	portalsNeedCreating bool,
) string {
	if pendingNotifications > 0 {
		return HistorySyncPhaseReceivingChunks
	}
	if portalsNeedCreating {
		if pendingConversations > 0 {
			return HistorySyncPhaseCreatingPortals
		}
		return HistorySyncPhaseWaitingDispatch
	}
	if pendingConversations > 0 {
		return HistorySyncPhaseCreatingPortals
	}
	return HistorySyncPhaseReady
}

func isWhatsAppSessionConnected(waClient *WhatsAppClient) bool {
	if waClient == nil || waClient.Client == nil {
		return false
	}
	return waClient.IsLoggedIn() && waClient.Client.IsConnected()
}

func bridgeStateEventName(userLogin *bridgev2.UserLogin) string {
	if userLogin == nil || userLogin.BridgeState == nil {
		return ""
	}
	prev := userLogin.BridgeState.GetPrev()
	return string(prev.StateEvent)
}

func BuildLoginSyncStatus(
	ctx context.Context,
	db *wadb.Database,
	userLogin *bridgev2.UserLogin,
	waClient *WhatsAppClient,
) (*LoginSyncStatusResponse, error) {
	loginID := userLogin.ID
	meta, _ := userLogin.Metadata.(*waid.UserLoginMetadata)
	if meta == nil {
		meta = &waid.UserLoginMetadata{}
	}

	var loggedInAtUnix int64
	if !meta.LoggedInAt.IsZero() {
		loggedInAtUnix = meta.LoggedInAt.Unix()
	}

	pendingNotifications, err := db.HSNotif.CountPending(ctx, loginID)
	if err != nil {
		return nil, err
	}

	pendingConversations, err := db.Conversation.CountPendingPortalCreation(ctx, loginID, loggedInAtUnix)
	if err != nil {
		return nil, err
	}

	portalsNeedCreating := meta.HistorySyncPortalsNeedCreating
	phase := historySyncPhase(pendingNotifications, pendingConversations, portalsNeedCreating)
	whatsappConnected := isWhatsAppSessionConnected(waClient)
	bridgeState := bridgeStateEventName(userLogin)

	readyForDiscovery := whatsappConnected &&
		!portalsNeedCreating &&
		pendingNotifications == 0 &&
		pendingConversations == 0

	var loggedInAtPtr *int64
	if loggedInAtUnix > 0 {
		loggedInAtPtr = &loggedInAtUnix
	}

	return &LoginSyncStatusResponse{
		LoginID: string(loginID),
		Session: LoginSessionStatus{
			BridgeState:       bridgeState,
			WhatsAppConnected: whatsappConnected,
		},
		HistorySync: LoginHistorySyncStatus{
			Phase:                    phase,
			PortalsNeedCreating:      portalsNeedCreating,
			PendingNotificationCount: pendingNotifications,
			PendingConversationCount: pendingConversations,
			LoggedInAt:               loggedInAtPtr,
		},
		ReadyForDiscovery: readyForDiscovery,
	}, nil
}

func GetLoginSyncStatusForLoginID(
	ctx context.Context,
	bridge *bridgev2.Bridge,
	waConnector *WhatsAppConnector,
	ownerMXID id.UserID,
	loginID networkid.UserLoginID,
) (*LoginSyncStatusResponse, error) {
	userLogin, err := bridge.GetExistingUserLoginByID(ctx, loginID)
	if err != nil {
		return nil, err
	} else if userLogin == nil {
		return nil, nil
	} else if userLogin.UserMXID != ownerMXID {
		return nil, nil
	}

	var waClient *WhatsAppClient
	if userLogin.Client != nil {
		waClient, _ = userLogin.Client.(*WhatsAppClient)
	}

	return BuildLoginSyncStatus(ctx, waConnector.DB, userLogin, waClient)
}
