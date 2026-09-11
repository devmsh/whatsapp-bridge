package mcp

import "whatsapp-bridge-v2/internal/db"

// The real implementation moved to internal/db so the extraction pipeline and
// the MCP read tools cannot drift apart: a voice note has to read identically
// to both, or a task's evidence quote will not match the text it came from.
//
// These aliases keep the existing call sites unchanged.

type enrichedMedia = db.EnrichedMedia

type mediaRef = db.MediaRef

func (s *Server) loadMediaUnderstanding(refs []mediaRef) map[string]enrichedMedia {
	return db.LoadMediaUnderstandingDB(s.db, refs)
}

func mergeAIText(content, mediaType string, em enrichedMedia) string {
	return db.MergeAIText(content, mediaType, em)
}
