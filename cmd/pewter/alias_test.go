package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// run executes the built binary under the given name and returns stdout, stderr, exit code.
func run(t *testing.T, bin string, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "PEWTER_CONFIG_DIR="+t.TempDir(), "PEWTER_KEYSTORE=file", "PEWTER_TOKEN=")
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return o.String(), e.String(), code
}

// The alias is the same binary under another name; invoking it as `pew` must
// behave exactly like `pewter`.
func TestPewAliasBehavesIdentically(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink alias is unix only")
	}
	dir := t.TempDir()
	pewter := filepath.Join(dir, "pewter")
	if out, err := exec.Command("go", "build", "-o", pewter, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	pew := filepath.Join(dir, "pew")
	if err := os.Symlink("pewter", pew); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{
		{"--help"}, {"version", "--json"}, {"help", "login"}, {}, {"frobnicate"},
		{"orders"}, {"buy"}, {"completion", "bash"}, {"completion", "zsh"},
	} {
		wo, we, wc := run(t, pewter, args...)
		go1, ge, gc := run(t, pew, args...)
		if wo != go1 || we != ge || wc != gc {
			t.Fatalf("pew differs from pewter for %v:\npewter %d %q %q\npew    %d %q %q", args, wc, wo, we, gc, go1, ge)
		}
	}
	if _, _, c := run(t, pew, "orders"); c != 3 {
		t.Fatalf("pew orders without sign-in should exit 3, got %d", c)
	}
}
