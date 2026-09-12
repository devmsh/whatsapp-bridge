package adapters

import (
	"strconv"
	"strings"

	"whatsapp-bridge-v2/internal/extract"
)

// judgeUser lays out the proposals for the second opinion, numbered, with the
// conversation under them so a verdict can be read in context.
func judgeUser(in extract.JudgeInput) string {
	var b strings.Builder
	b.WriteString("Tasks to check:\n")
	for i, it := range in.Items {
		b.WriteString(strconv.Itoa(i))
		b.WriteString(". ")
		b.WriteString(it.Title)
		b.WriteString("\n   said in: ")
		b.WriteString(it.Evidence)
		b.WriteString("\n")
	}
	b.WriteString("\nThe conversation they came from:\n\n")
	b.WriteString(in.Chunk.Rendered)
	return b.String()
}
