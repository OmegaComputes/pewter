// Package keystore keeps the CLI token in the OS keychain when one is usable,
// and in a 0600 file otherwise. The token is never passed on a command line
// and never written to config.json.
package keystore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// ErrNotFound means no token is stored for the profile.
var ErrNotFound = errors.New("no stored token")

// Store holds one token per profile.
type Store interface {
	Name() string
	Get(profile string) (string, error)
	Set(profile, token string) error
	Delete(profile string) error
}

// Runner runs a command with stdin and returns stdout. It exists so tests can
// prove the secret is never put in argv.
type Runner func(ctx context.Context, stdin string, name string, args ...string) (string, error)

// ExecRunner is the real Runner.
func ExecRunner(ctx context.Context, stdin string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = strings.NewReader(stdin)
	var out strings.Builder
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return out.String(), nil
}

var (
	profileRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,31}$`)
	tokenRE   = regexp.MustCompile(`^[A-Za-z0-9_.~-]{16,512}$`)
)

// ValidProfile reports whether name is a safe profile name.
func ValidProfile(name string) bool { return profileRE.MatchString(name) }

// ValidToken reports whether a token has a plausible, injection-safe shape.
func ValidToken(t string) bool { return tokenRE.MatchString(t) }

func check(profile, token string, needToken bool) error {
	if !ValidProfile(profile) {
		return fmt.Errorf("invalid profile name %q (use a-z, 0-9, _ and -)", profile)
	}
	if needToken && !ValidToken(token) {
		return errors.New("token has an unexpected shape; refusing to store it")
	}
	return nil
}

// ---- file ----

// File stores tokens in <dir>/tokens/<profile> with mode 0600.
type File struct{ Dir string }

func (f File) Name() string         { return "file (0600)" }
func (f File) path(p string) string { return filepath.Join(f.Dir, "tokens", p) }

func (f File) Get(profile string) (string, error) {
	if err := check(profile, "", false); err != nil {
		return "", err
	}
	b, err := os.ReadFile(f.path(profile))
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func (f File) Set(profile, token string) error {
	if err := check(profile, token, true); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(f.Dir, "tokens"), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Join(f.Dir, "tokens"), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(token + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), f.path(profile))
}

func (f File) Delete(profile string) error {
	if err := check(profile, "", false); err != nil {
		return err
	}
	err := os.Remove(f.path(profile))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// ---- macOS keychain ----

// MacKeychain uses /usr/bin/security. The secret goes in on stdin via
// `security -i`, never in argv.
type MacKeychain struct{ Run Runner }

const service = "pewter-cli"

func (MacKeychain) Name() string { return "macOS Keychain" }

func (k MacKeychain) Get(profile string) (string, error) {
	if err := check(profile, "", false); err != nil {
		return "", err
	}
	out, err := k.Run(context.Background(), "", "security", "find-generic-password", "-s", service, "-a", profile, "-w")
	if err != nil {
		return "", ErrNotFound
	}
	return strings.TrimSpace(out), nil
}

func (k MacKeychain) Set(profile, token string) error {
	if err := check(profile, token, true); err != nil {
		return err
	}
	line := fmt.Sprintf("add-generic-password -U -s %s -a %s -w %s\n", service, profile, token)
	_, err := k.Run(context.Background(), line, "security", "-i")
	if err != nil {
		return errors.New("keychain write failed")
	}
	return nil
}

func (k MacKeychain) Delete(profile string) error {
	if err := check(profile, "", false); err != nil {
		return err
	}
	_, _ = k.Run(context.Background(), "", "security", "delete-generic-password", "-s", service, "-a", profile)
	return nil
}

// ---- Linux Secret Service ----

// SecretTool uses libsecret's secret-tool; the secret is read from stdin.
type SecretTool struct{ Run Runner }

func (SecretTool) Name() string { return "Secret Service (secret-tool)" }

func (s SecretTool) Get(profile string) (string, error) {
	if err := check(profile, "", false); err != nil {
		return "", err
	}
	out, err := s.Run(context.Background(), "", "secret-tool", "lookup", "service", service, "account", profile)
	if err != nil || strings.TrimSpace(out) == "" {
		return "", ErrNotFound
	}
	return strings.TrimSpace(out), nil
}

func (s SecretTool) Set(profile, token string) error {
	if err := check(profile, token, true); err != nil {
		return err
	}
	_, err := s.Run(context.Background(), token, "secret-tool", "store", "--label=Pewter CLI", "service", service, "account", profile)
	if err != nil {
		return errors.New("secret service write failed")
	}
	return nil
}

func (s SecretTool) Delete(profile string) error {
	if err := check(profile, "", false); err != nil {
		return err
	}
	_, _ = s.Run(context.Background(), "", "secret-tool", "clear", "service", service, "account", profile)
	return nil
}

// ---- selection ----

// Pick chooses a store. mode is "auto", "keychain" or "file" (PEWTER_KEYSTORE).
// The second return is a warning to show once when the file fallback is used
// on a machine that might have had a keychain.
func Pick(mode, dir, goos string, lookPath func(string) (string, error), getenv func(string) string, run Runner) (Store, string) {
	file := File{Dir: dir}
	if mode == "file" {
		return file, ""
	}
	switch goos {
	case "darwin":
		if _, err := lookPath("security"); err == nil {
			return MacKeychain{Run: run}, ""
		}
	case "linux":
		if _, err := lookPath("secret-tool"); err == nil && getenv("DBUS_SESSION_BUS_ADDRESS") != "" {
			return SecretTool{Run: run}, ""
		}
	}
	if mode == "keychain" {
		return file, "no OS keychain available; using a 0600 file instead"
	}
	return file, "no OS keychain found; token stored in a 0600 file under " + dir
}

// Default picks for the running machine.
func Default(mode, dir string) (Store, string) {
	return Pick(mode, dir, runtime.GOOS, exec.LookPath, os.Getenv, ExecRunner)
}
