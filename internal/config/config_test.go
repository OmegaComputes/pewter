package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDirPrecedence(t *testing.T) {
	g := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	if d, _ := Dir(g(map[string]string{"PEWTER_CONFIG_DIR": "/a", "XDG_CONFIG_HOME": "/b", "HOME": "/c"})); d != "/a" {
		t.Fatal(d)
	}
	if d, _ := Dir(g(map[string]string{"XDG_CONFIG_HOME": "/b", "HOME": "/c"})); d != "/b/pewter" {
		t.Fatal(d)
	}
	if d, _ := Dir(g(map[string]string{"HOME": "/c"})); d != "/c/.config/pewter" {
		t.Fatal(d)
	}
	if _, err := Dir(g(nil)); err == nil {
		t.Fatal("no home should error")
	}
}

func TestSaveLoadPerms(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pw")
	if f, err := Load(dir); err != nil || f.APIURL != "" {
		t.Fatal("missing file should be empty config")
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := Save(dir, File{APIURL: "https://x/api/v1", Profile: "p", SignedAt: &now}); err != nil {
		t.Fatal(err)
	}
	f, err := Load(dir)
	if err != nil || f.Profile != "p" || f.SignedAt == nil || !f.SignedAt.Equal(now) {
		t.Fatalf("%+v %v", f, err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "config.json"))
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("perm %v", fi.Mode().Perm())
	}
}
