package keystore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const tok = "tok_abcdefghijklmnop1234"

func TestFileStoreRoundTripAndPerms(t *testing.T) {
	dir := t.TempDir()
	s := File{Dir: dir}
	if _, err := s.Get("default"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if err := s.Set("default", tok); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get("default")
	if err != nil || got != tok {
		t.Fatalf("got %q %v", got, err)
	}
	fi, _ := os.Stat(filepath.Join(dir, "tokens", "default"))
	di, _ := os.Stat(filepath.Join(dir, "tokens"))
	if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
		t.Fatalf("perms file=%v dir=%v", fi.Mode().Perm(), di.Mode().Perm())
	}
	if err := s.Delete("default"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("default"); !errors.Is(err, ErrNotFound) {
		t.Fatal("not deleted")
	}
	if err := s.Delete("default"); err != nil {
		t.Fatal("delete should be idempotent")
	}
}

func TestInputValidation(t *testing.T) {
	s := File{Dir: t.TempDir()}
	for _, p := range []string{"../x", "a/b", "", "UP", "a b", strings.Repeat("a", 40)} {
		if err := s.Set(p, tok); err == nil {
			t.Errorf("profile %q accepted", p)
		}
		if _, err := s.Get(p); err == nil {
			t.Errorf("get profile %q accepted", p)
		}
	}
	for _, v := range []string{"short", "tok_has space abcdefghijkl", "tok_semi;colon_abcdefghi", "tok_quote\"abcdefghijkl", "tok_new\nline_abcdefghijk", "$(reboot)_abcdefghijkl"} {
		if err := s.Set("default", v); err == nil {
			t.Errorf("token %q accepted", v)
		}
	}
}

// The secret must only ever travel on stdin, never in argv.
func TestKeychainNeverPutsSecretInArgv(t *testing.T) {
	var calls [][]string
	var stdins []string
	run := func(_ context.Context, stdin string, name string, args ...string) (string, error) {
		calls = append(calls, append([]string{name}, args...))
		stdins = append(stdins, stdin)
		return tok + "\n", nil
	}
	for _, s := range []Store{MacKeychain{Run: run}, SecretTool{Run: run}} {
		calls, stdins = nil, nil
		if err := s.Set("default", tok); err != nil {
			t.Fatal(err)
		}
		if got, err := s.Get("default"); err != nil || got != tok {
			t.Fatalf("get: %q %v", got, err)
		}
		_ = s.Delete("default")
		for _, c := range calls {
			if strings.Contains(strings.Join(c, " "), tok) {
				t.Fatalf("%s: secret in argv: %v", s.Name(), c)
			}
		}
		if !strings.Contains(strings.Join(stdins, ""), tok) {
			t.Fatalf("%s: secret was not sent on stdin", s.Name())
		}
	}
}

func TestKeychainErrorsDoNotEchoSecret(t *testing.T) {
	run := func(context.Context, string, string, ...string) (string, error) { return "", errors.New("boom " + tok) }
	for _, s := range []Store{MacKeychain{Run: run}, SecretTool{Run: run}} {
		err := s.Set("default", tok)
		if err == nil || strings.Contains(err.Error(), tok) {
			t.Fatalf("%s: err=%v", s.Name(), err)
		}
	}
}

func TestPick(t *testing.T) {
	has := func(names ...string) func(string) (string, error) {
		return func(n string) (string, error) {
			for _, x := range names {
				if x == n {
					return "/x/" + n, nil
				}
			}
			return "", errors.New("no")
		}
	}
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	run := func(context.Context, string, string, ...string) (string, error) { return "", nil }
	if s, w := Pick("auto", "d", "darwin", has("security"), env(nil), run); s.Name() != "macOS Keychain" || w != "" {
		t.Fatalf("darwin: %s %q", s.Name(), w)
	}
	if s, _ := Pick("auto", "d", "linux", has("secret-tool"), env(map[string]string{"DBUS_SESSION_BUS_ADDRESS": "x"}), run); !strings.Contains(s.Name(), "Secret Service") {
		t.Fatalf("linux: %s", s.Name())
	}
	if s, w := Pick("auto", "d", "linux", has("secret-tool"), env(nil), run); !strings.HasPrefix(s.Name(), "file") || w == "" {
		t.Fatalf("linux without dbus should fall back with a warning: %s %q", s.Name(), w)
	}
	if s, w := Pick("file", "d", "darwin", has("security"), env(nil), run); !strings.HasPrefix(s.Name(), "file") || w != "" {
		t.Fatalf("forced file: %s %q", s.Name(), w)
	}
}
