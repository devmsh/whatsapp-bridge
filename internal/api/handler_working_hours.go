package api

import (
	"net/http"
	"time"

	"whatsapp-bridge-v2/internal/wa"
)

func (s *Server) handleWorkingHours(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonOK(w, wa.LoadWorkingHoursConfig(s.store))

	case http.MethodPut:
		var req struct {
			Enabled     bool     `json:"enabled"`
			Start       string   `json:"start"`
			End         string   `json:"end"`
			WorkingDays []int    `json:"working_days"`
			ChatJIDs    []string `json:"chat_jids"`
		}
		if err := decodeJSON(r, &req); err != nil {
			jsonError(w, 400, "invalid JSON")
			return
		}

		// Validate Start and End match HH:MM format.
		if _, err := time.Parse("15:04", req.Start); err != nil {
			jsonError(w, 400, "invalid start time: must be HH:MM")
			return
		}
		if _, err := time.Parse("15:04", req.End); err != nil {
			jsonError(w, 400, "invalid end time: must be HH:MM")
			return
		}

		// Validate start < end in total minutes.
		// We already validated parsing above so these are safe.
		ts, _ := time.Parse("15:04", req.Start)
		te, _ := time.Parse("15:04", req.End)
		startMinutes := ts.Hour()*60 + ts.Minute()
		endMinutes := te.Hour()*60 + te.Minute()
		if startMinutes >= endMinutes {
			jsonError(w, 400, "start time must be before end time")
			return
		}

		old := wa.LoadWorkingHoursConfig(s.store)

		// Build set of new ChatJIDs for quick lookup.
		newChatSet := make(map[string]bool, len(req.ChatJIDs))
		for _, jid := range req.ChatJIDs {
			newChatSet[jid] = true
		}

		// Build set of old FeatureMuted for quick lookup.
		oldFeatureMutedSet := make(map[string]bool, len(old.FeatureMuted))
		for _, jid := range old.FeatureMuted {
			oldFeatureMutedSet[jid] = true
		}

		// Compute the deduplicated set of JIDs to release:
		//   – chats removed from selection that were feature-muted, AND
		//   – all feature-muted chats when disabling.
		// Merging both into one set avoids two sequential ReleaseMutes calls.
		releaseSet := make(map[string]bool)
		for _, jid := range old.ChatJIDs {
			if oldFeatureMutedSet[jid] && !newChatSet[jid] {
				releaseSet[jid] = true
			}
		}
		if !req.Enabled {
			for _, jid := range old.FeatureMuted {
				releaseSet[jid] = true
			}
		}
		jidsToRelease := make([]string, 0, len(releaseSet))
		for jid := range releaseSet {
			jidsToRelease = append(jidsToRelease, jid)
		}

		newChatJIDs := req.ChatJIDs
		if newChatJIDs == nil {
			newChatJIDs = []string{}
		}
		newWorkingDays := req.WorkingDays
		if newWorkingDays == nil {
			newWorkingDays = []int{}
		}

		newCfg := wa.WorkingHoursConfig{
			Enabled:     req.Enabled,
			Start:       req.Start,
			End:         req.End,
			WorkingDays: newWorkingDays,
			ChatJIDs:    newChatJIDs,
		}

		// ApplyWorkingHoursUpdate performs release → save → reconcile atomically
		// under workingHoursMu, preventing any background Reconcile race.
		if err := wa.ApplyWorkingHoursUpdate(s.client, s.store, newCfg, jidsToRelease); err != nil {
			jsonError(w, 500, "failed to save config")
			return
		}

		jsonOK(w, wa.LoadWorkingHoursConfig(s.store))

	default:
		methodNotAllowed(w)
	}
}
