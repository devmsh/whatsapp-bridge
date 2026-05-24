package wa

import (
	waCompanionReg "go.mau.fi/whatsmeow/proto/waCompanionReg"
	"go.mau.fi/whatsmeow/store"
	"google.golang.org/protobuf/proto"
)

// historyPeriodKey persists the chosen period so it survives restarts.
const historyPeriodKey = "history_period"

// History period presets shown on the link screen. They map to whatsmeow's
// DeviceProps, which are sent to WhatsApp during pairing — so a change only
// takes effect on the NEXT link (unlink + scan again).
const (
	History3Months   = "3months"
	History1Year     = "1year"
	HistoryEverything = "everything"
)

// ValidHistoryPeriod reports whether s is a known preset.
func ValidHistoryPeriod(s string) bool {
	return s == History3Months || s == History1Year || s == HistoryEverything
}

// ApplyHistoryPeriod sets whatsmeow's global DeviceProps for the next pairing.
// store.DeviceProps is read when the pairing payload is built, so this must run
// before connecting/pairing (or before restarting the QR flow).
func ApplyHistoryPeriod(period string) {
	cfg := store.DeviceProps.GetHistorySyncConfig()
	if cfg == nil {
		cfg = &waCompanionReg.DeviceProps_HistorySyncConfig{}
		store.DeviceProps.HistorySyncConfig = cfg
	}
	switch period {
	case History1Year:
		store.DeviceProps.RequireFullSync = proto.Bool(true)
		cfg.FullSyncDaysLimit = proto.Uint32(365)
		cfg.RecentSyncDaysLimit = nil
	case HistoryEverything:
		store.DeviceProps.RequireFullSync = proto.Bool(true)
		cfg.FullSyncDaysLimit = nil
		cfg.RecentSyncDaysLimit = nil
	default: // History3Months
		store.DeviceProps.RequireFullSync = proto.Bool(false)
		cfg.FullSyncDaysLimit = proto.Uint32(90)
		cfg.RecentSyncDaysLimit = proto.Uint32(90)
	}
}

// HistoryPeriod returns the persisted period, or "3months" if unset.
func (c *Client) HistoryPeriod() string {
	return c.HistoryPeriodOr(History3Months)
}

// HistoryPeriodOr returns the persisted period, falling back to def (then to
// "3months" if def is invalid). Used at startup so an env default applies when
// the user has not chosen one in the GUI.
func (c *Client) HistoryPeriodOr(def string) string {
	if v, _, _ := c.Store.GetSyncState(historyPeriodKey); ValidHistoryPeriod(v) {
		return v
	}
	if ValidHistoryPeriod(def) {
		return def
	}
	return History3Months
}

// SetHistoryPeriod persists the period and applies it to DeviceProps. It does
// NOT re-link; the caller restarts the QR flow if currently logged out.
func (c *Client) SetHistoryPeriod(period string) error {
	ApplyHistoryPeriod(period)
	return c.Store.PutSyncState(historyPeriodKey, period)
}
