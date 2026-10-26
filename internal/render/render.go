// Package render prints tables and JSON. Anything that came from the server is
// sanitised before it reaches a terminal, because counterparty text is
// untrusted and could carry terminal escape sequences.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"unicode"
	"unicode/utf8"
)

// Clean removes control characters (including ESC) and caps the length.
func Clean(s string, max int) string {
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r == utf8.RuneError || unicode.IsControl(r) || r == '\u2028' || r == '\u2029' || (r >= 0x200B && r <= 0x200F) || (r >= 0x202A && r <= 0x202E) || (r >= 0x2066 && r <= 0x2069) {
			r = ' '
		}
		if n >= max {
			b.WriteString("…")
			break
		}
		b.WriteRune(r)
		n++
	}
	return b.String()
}

// JSON writes v as indented JSON (Go escapes control characters itself).
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// Table writes a left-aligned table. Cells are cleaned.
func Table(w io.Writer, header []string, rows [][]string) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, strings.Join(header, "\t"))
	for _, r := range rows {
		cells := make([]string, len(r))
		for i, c := range r {
			cells[i] = Clean(c, 60)
		}
		fmt.Fprintln(tw, strings.Join(cells, "\t"))
	}
	_ = tw.Flush()
}
