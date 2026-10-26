package render

import (
	"bytes"
	"strings"
	"testing"
)

func TestCleanStripsEscapes(t *testing.T) {
	in := "ok\x1b[31mRED\x1b]0;title\x07 \u202Eevil\r\n"
	got := Clean(in, 100)
	for _, bad := range []string{"\x1b", "\x07", "\r", "\n", "\u202E"} {
		if strings.Contains(got, bad) {
			t.Fatalf("%q still contains %q", got, bad)
		}
	}
	if !strings.Contains(got, "RED") {
		t.Fatalf("visible text lost: %q", got)
	}
}

func TestCleanTruncates(t *testing.T) {
	if got := Clean(strings.Repeat("a", 100), 10); got != strings.Repeat("a", 10)+"…" {
		t.Fatalf("got %q", got)
	}
}

func TestTableCleansCells(t *testing.T) {
	var b bytes.Buffer
	Table(&b, []string{"A", "B"}, [][]string{{"x\x1b[2J", "y"}})
	if strings.Contains(b.String(), "\x1b") {
		t.Fatal("escape reached the terminal")
	}
}

func TestJSONEscapesControl(t *testing.T) {
	var b bytes.Buffer
	_ = JSON(&b, map[string]string{"k": "a\x1bb"})
	if strings.Contains(b.String(), "\x1b") {
		t.Fatal("raw ESC in JSON output")
	}
}
