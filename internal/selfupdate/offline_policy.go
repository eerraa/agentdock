package selfupdate

import "errors"

// This fork has no verified online release channel. The runtime policy must be
// enabled in source only after its complete source/asset/rollback gate passes.
const OfflineUpdateCode = "online-updates-disabled"
const offlineUpdateMessage = "AgentDock Eerraa uses verified offline Setup.exe packages for manual updates. Online checks and updates are unavailable; upstream packages are never used as a fallback."

var ErrOnlineUpdatesDisabled = errors.New(OfflineUpdateCode + ": " + offlineUpdateMessage)

func offlineCheckResult(version string) CheckResult {
	return CheckResult{
		Code: OfflineUpdateCode, Channel: "offline-manual",
		CurrentVersion: normalizeVersion(version), Message: offlineUpdateMessage,
	}
}
