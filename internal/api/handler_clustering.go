package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"whatsapp-bridge-v2/internal/db"
)

// One cluster proposed by the LLM.
type proposedCluster struct {
	ParentExistingID *int64 `json:"parent_existing_id,omitempty"`
	ParentNew        *struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"parent_new,omitempty"`
	ChildIDs  []int64 `json:"child_ids"`
	Rationale string  `json:"rationale,omitempty"`
}

// ClusterResult summarizes what the cluster pass changed.
type ClusterResult struct {
	NewParents     int    `json:"new_parents"`     // newly-created parent tasks
	ReusedParents  int    `json:"reused_parents"`  // existing tasks promoted to parent
	ChildrenLinked int    `json:"children_linked"` // tasks now pointing to a parent
	Skipped        int    `json:"skipped"`         // clusters rejected (invalid / no children)
	Rationales     []string `json:"rationales,omitempty"`
}

// clusterCircleTasks runs the cluster-tasks sidecar on a circle's
// currently-open, non-rejected, non-already-clustered tasks. Returns the
// applied changes. Best-effort: AI/sidecar failures return empty results, not
// an HTTP error.
func (s *Server) clusterCircleTasks(circleID int64) (*ClusterResult, error) {
	tasks, err := s.collectClusterCandidates(circleID)
	if err != nil {
		return nil, err
	}
	if len(tasks) < 2 {
		return &ClusterResult{}, nil
	}

	circle, _ := s.store.GetCircle(circleID)
	circleName := ""
	circleDesc := ""
	if circle != nil {
		circleName = circle.Name
		if p, _ := s.store.GetProfile(db.ProfileCircle, strconv.FormatInt(circleID, 10)); p != nil {
			circleDesc = p.Description
		}
	}

	// Slim shape the sidecar consumes.
	type miniTask struct {
		ID          int64  `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description,omitempty"`
		Priority    string `json:"priority,omitempty"`
		Status      string `json:"status,omitempty"`
		Assignee    string `json:"assignee,omitempty"`
	}
	mini := make([]miniTask, 0, len(tasks))
	for _, t := range tasks {
		mini = append(mini, miniTask{
			ID:          t.ID,
			Title:       t.Title,
			Description: strings.TrimSpace(t.Description),
			Priority:    t.Priority,
			Status:      t.Status,
			Assignee:    s.resolveAssigneeLabel(t.AssigneeJID),
		})
	}
	input := map[string]any{
		"circle_name":        circleName,
		"circle_description": circleDesc,
		"tasks":              mini,
	}
	stdin, _ := json.Marshal(input)

	out, runErr := s.runAgentInput(3*time.Minute, string(stdin), "cluster-tasks.mjs")
	if runErr != nil {
		return &ClusterResult{}, nil // best-effort; no error to caller
	}
	var res struct {
		OK       bool              `json:"ok"`
		Clusters []proposedCluster `json:"clusters"`
	}
	if line := lastJSONLine(out); line != nil {
		json.Unmarshal(line, &res)
	}
	if !res.OK || len(res.Clusters) == 0 {
		return &ClusterResult{}, nil
	}

	return s.applyClusters(circleID, tasks, res.Clusters), nil
}

// collectClusterCandidates returns the open/in_progress tasks in this circle
// that are NOT already in a parent/child relationship and NOT rejected.
func (s *Server) collectClusterCandidates(circleID int64) ([]db.Task, error) {
	tasks, err := s.store.TasksForCircle(circleID)
	if err != nil {
		return nil, err
	}
	out := tasks[:0]
	for _, t := range tasks {
		if t.ReviewStatus == db.ReviewRejected {
			continue
		}
		if t.Status == db.TaskDone || t.Status == db.TaskCancelled {
			continue
		}
		if t.ParentID != nil {
			continue
		}
		// Skip tasks that are themselves parents (have children).
		var n int
		s.store.DB.QueryRow(`SELECT COUNT(*) FROM tasks WHERE parent_id = ?`, t.ID).Scan(&n)
		if n > 0 {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}

// applyClusters writes the LLM's proposed clusters to the DB, defensively:
// it ignores duplicate child ids, child ids the sidecar invented, clusters
// without ≥2 children, and circular references.
func (s *Server) applyClusters(circleID int64, tasks []db.Task, clusters []proposedCluster) *ClusterResult {
	res := &ClusterResult{}
	idSet := make(map[int64]bool, len(tasks))
	for _, t := range tasks {
		idSet[t.ID] = true
	}
	usedAsChild := map[int64]bool{}

	for _, c := range clusters {
		// Filter children: must be candidates, not seen yet.
		validChildren := make([]int64, 0, len(c.ChildIDs))
		for _, cid := range c.ChildIDs {
			if !idSet[cid] || usedAsChild[cid] {
				continue
			}
			validChildren = append(validChildren, cid)
		}
		if len(validChildren) < 2 {
			res.Skipped++
			continue
		}

		var parentID int64
		switch {
		case c.ParentExistingID != nil:
			pid := *c.ParentExistingID
			if !idSet[pid] {
				res.Skipped++
				continue
			}
			// Remove parent from child list if the sidecar accidentally included it.
			pruned := validChildren[:0]
			for _, cid := range validChildren {
				if cid != pid {
					pruned = append(pruned, cid)
				}
			}
			validChildren = pruned
			if len(validChildren) < 1 {
				res.Skipped++
				continue
			}
			parentID = pid
			res.ReusedParents++

		case c.ParentNew != nil && strings.TrimSpace(c.ParentNew.Title) != "":
			t, err := s.store.CreateTask(&db.Task{
				Title:        strings.TrimSpace(c.ParentNew.Title),
				Description:  strings.TrimSpace(c.ParentNew.Description),
				ReviewStatus: db.ReviewAccepted, // user asked: apply immediately
			})
			if err != nil {
				res.Skipped++
				continue
			}
			s.store.AddTaskCircle(t.ID, circleID)
			parentID = t.ID
			res.NewParents++

		default:
			res.Skipped++
			continue
		}

		// Link children.
		for _, cid := range validChildren {
			if err := s.store.SetTaskParent(cid, &parentID); err == nil {
				res.ChildrenLinked++
				usedAsChild[cid] = true
			}
		}
		if c.Rationale != "" {
			res.Rationales = append(res.Rationales, c.Rationale)
		}
	}
	return res
}

// resolveAssigneeLabel turns a JID into a human label (best effort).
func (s *Server) resolveAssigneeLabel(jid string) string {
	if jid == "" {
		return ""
	}
	var name, push, biz string
	s.store.DB.QueryRow(`SELECT COALESCE(name,''), COALESCE(push_name,''), COALESCE(business_name,'')
		FROM contacts WHERE jid = ?`, jid).Scan(&name, &push, &biz)
	for _, v := range []string{name, biz, push} {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return strings.SplitN(jid, "@", 2)[0]
}

// handleClusterTasks is the manual "Cluster tasks" trigger.
// POST /api/v2/tasks/cluster?circle=<id>
func (s *Server) handleClusterTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	circleStr := r.URL.Query().Get("circle")
	if circleStr == "" {
		jsonError(w, 400, "circle id required")
		return
	}
	circleID, err := strconv.ParseInt(circleStr, 10, 64)
	if err != nil {
		jsonError(w, 400, "invalid circle id")
		return
	}
	fmt.Printf("Clustering tasks for circle %d\n", circleID)
	res, err := s.clusterCircleTasks(circleID)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOK(w, res)
}
