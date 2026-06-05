package connector

func (wa *WhatsAppClient) scheduleDiscoveryCommand(reason string) {
	if wa.Main.DiscoveryCommand == nil || !wa.Main.DiscoveryCommand.Enabled() {
		return
	}
	if wa.UserLogin == nil || wa.UserLogin.User == nil {
		return
	}
	wa.Main.DiscoveryCommand.Schedule(wa.UserLogin.User.MXID, wa.UserLogin.ID, reason)
}
