package connector

import "testing"

func TestHistorySyncPhase(t *testing.T) {
	tests := []struct {
		name                 string
		pendingNotifications int
		pendingConversations int
		portalsNeedCreating  bool
		want                 string
	}{
		{"ready", 0, 0, false, HistorySyncPhaseReady},
		{"receiving chunks", 2, 0, false, HistorySyncPhaseReceivingChunks},
		{"waiting dispatch", 0, 0, true, HistorySyncPhaseWaitingDispatch},
		{"creating portals", 0, 5, true, HistorySyncPhaseCreatingPortals},
		{"chunks beat portals flag", 1, 10, true, HistorySyncPhaseReceivingChunks},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := historySyncPhase(tt.pendingNotifications, tt.pendingConversations, tt.portalsNeedCreating)
			if got != tt.want {
				t.Fatalf("historySyncPhase() = %q, want %q", got, tt.want)
			}
		})
	}
}
