package wa

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/types"

	"whatsapp-bridge-v2/internal/db"
)

// WorkingHoursConfig holds working-hours auto-mute settings.
type WorkingHoursConfig struct {
	Enabled     bool     `json:"enabled"`
	Start       string   `json:"start"`        // "HH:MM"
	End         string   `json:"end"`          // "HH:MM"
	WorkingDays []int    `json:"working_days"` // 0=Sun ... 6=Sat
	ChatJIDs    []string `json:"chat_jids"`
	FeatureMuted []string `json:"feature_muted"` // JIDs muted by this feature
}

const workingHoursConfigKey = "working_hours_config"

// workingHoursMu is the package-level mutex shared by Reconcile, ReconcileNow, and ReleaseMutes.
var workingHoursMu sync.Mutex

// defaultWorkingHoursConfig returns a config with default values.
func defaultWorkingHoursConfig() WorkingHoursConfig {
	return WorkingHoursConfig{
		Enabled:      false,
		Start:        "09:00",
		End:          "18:00",
		WorkingDays:  []int{0, 1, 2, 3, 4}, // Sun–Thu
		ChatJIDs:     []string{},
		FeatureMuted: []string{},
	}
}

// LoadWorkingHoursConfig reads the config from store; returns defaults if absent or invalid.
func LoadWorkingHoursConfig(store *db.Store) WorkingHoursConfig {
	def := defaultWorkingHoursConfig()
	raw, _, err := store.GetSyncState(workingHoursConfigKey)
	if err != nil || raw == "" {
		return def
	}
	var cfg WorkingHoursConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return def
	}
	// Apply field-level defaults for empty/nil slices.
	if cfg.Start == "" {
		cfg.Start = def.Start
	}
	if cfg.End == "" {
		cfg.End = def.End
	}
	if cfg.WorkingDays == nil {
		cfg.WorkingDays = def.WorkingDays
	}
	if cfg.ChatJIDs == nil {
		cfg.ChatJIDs = []string{}
	}
	if cfg.FeatureMuted == nil {
		cfg.FeatureMuted = []string{}
	}
	return cfg
}

// SaveWorkingHoursConfig writes the config to store as JSON.
func SaveWorkingHoursConfig(store *db.Store, cfg WorkingHoursConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal working hours config: %w", err)
	}
	return store.PutSyncState(workingHoursConfigKey, string(data))
}

// parseHHMM parses a "HH:MM" string and returns the total minutes from midnight.
// Returns -1 on error.
func parseHHMM(s string) int {
	var h, m int
	if _, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil {
		return -1
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return -1
	}
	return h*60 + m
}

// DesiredMute returns true when the given time falls inside the working window
// for one of the configured working days. Returns false if feature is disabled,
// config is invalid, or it's outside the window / a non-working day.
func DesiredMute(now time.Time, cfg WorkingHoursConfig) bool {
	if !cfg.Enabled {
		return false
	}
	startMin := parseHHMM(cfg.Start)
	endMin := parseHHMM(cfg.End)
	if startMin < 0 || endMin < 0 || startMin >= endMin {
		return false
	}
	wd := int(now.Weekday())
	inWorkingDay := false
	for _, d := range cfg.WorkingDays {
		if d == wd {
			inWorkingDay = true
			break
		}
	}
	if !inWorkingDay {
		return false
	}
	cur := now.Hour()*60 + now.Minute()
	return startMin <= cur && cur < endMin
}

// windowEndUnix returns the Unix timestamp (in now.Location()) for the end of
// today's working window.
func windowEndUnix(now time.Time, cfg WorkingHoursConfig) int64 {
	endMin := parseHHMM(cfg.End)
	if endMin < 0 {
		endMin = 18 * 60 // fallback: 18:00
	}
	endH := endMin / 60
	endM := endMin % 60
	end := time.Date(now.Year(), now.Month(), now.Day(), endH, endM, 0, 0, now.Location())
	return end.Unix()
}

// Reconcile loads the config, compares desired vs current mute state for each
// configured chat JID, and applies changes via the WhatsApp app state protocol.
// It also updates the local DB and persists the FeatureMuted set on change.
func Reconcile(client *Client, store *db.Store, now time.Time) {
	workingHoursMu.Lock()
	defer workingHoursMu.Unlock()

	cfg := LoadWorkingHoursConfig(store)
	if !cfg.Enabled || !client.IsConnected() {
		return
	}

	desired := DesiredMute(now, cfg)
	wa := client.GetWhatsmeowClient()

	// Build current feature-muted set.
	featureMutedSet := make(map[string]bool, len(cfg.FeatureMuted))
	for _, jid := range cfg.FeatureMuted {
		featureMutedSet[jid] = true
	}
	changed := false

	for _, jidStr := range cfg.ChatJIDs {
		chatJID, err := types.ParseJID(jidStr)
		if err != nil {
			continue
		}
		inSet := featureMutedSet[jidStr]

		if desired && !inSet {
			// Mute: use -1 duration (forever) for working-hours mute.
			wa.SendAppState(context.Background(), appstate.BuildMute(chatJID, true, -1))
			store.SetChatMuted(jidStr, true, windowEndUnix(now, cfg))
			featureMutedSet[jidStr] = true
			changed = true
		} else if !desired && inSet {
			// Unmute.
			wa.SendAppState(context.Background(), appstate.BuildMute(chatJID, false, 0))
			store.SetChatMuted(jidStr, false, 0)
			delete(featureMutedSet, jidStr)
			changed = true
		}
	}

	if changed {
		cfg.FeatureMuted = setToSlice(featureMutedSet)
		SaveWorkingHoursConfig(store, cfg)
	}
}

// ReconcileNow calls Reconcile with the current time.
func ReconcileNow(client *Client, store *db.Store) {
	Reconcile(client, store, time.Now())
}

// ReleaseMutes unmutes the given JIDs unconditionally, removes them from
// FeatureMuted, and persists the updated config.
func ReleaseMutes(client *Client, store *db.Store, jids []string) {
	workingHoursMu.Lock()
	defer workingHoursMu.Unlock()

	cfg := LoadWorkingHoursConfig(store)
	wa := client.GetWhatsmeowClient()

	featureMutedSet := make(map[string]bool, len(cfg.FeatureMuted))
	for _, jid := range cfg.FeatureMuted {
		featureMutedSet[jid] = true
	}

	for _, jidStr := range jids {
		chatJID, err := types.ParseJID(jidStr)
		if err != nil {
			continue
		}
		wa.SendAppState(context.Background(), appstate.BuildMute(chatJID, false, 0))
		store.SetChatMuted(jidStr, false, 0)
		delete(featureMutedSet, jidStr)
	}

	cfg.FeatureMuted = setToSlice(featureMutedSet)
	SaveWorkingHoursConfig(store, cfg)
}

// StartWorkingHoursScheduler starts a background goroutine that reconciles mute
// state immediately and then every minute.
func StartWorkingHoursScheduler(client *Client, store *db.Store) {
	go func() {
		Reconcile(client, store, time.Now())
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			Reconcile(client, store, time.Now())
		}
	}()
}

// setToSlice converts a map[string]bool to a sorted slice.
func setToSlice(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
