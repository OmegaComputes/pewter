package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/OmegaComputes/pewter/internal/mock"
)

type rig struct {
	t    *testing.T
	srv  *httptest.Server
	mock *mock.Server
	dir  string
	env  map[string]string
	out  []string // every byte printed, to scan for secrets
}

func newRig(t *testing.T) *rig {
	t.Helper()
	m := mock.New()
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", m)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	dir := t.TempDir()
	return &rig{t: t, srv: srv, mock: m, dir: dir, env: map[string]string{
		"PEWTER_CONFIG_DIR": dir, "PEWTER_KEYSTORE": "file", "PEWTER_API_URL": srv.URL + "/api/v1", "HOME": dir,
	}}
}

func (r *rig) run(stdin string, args ...string) (int, string, string) {
	r.t.Helper()
	var o, e bytes.Buffer
	env := DefaultEnv()
	env.Stdin = strings.NewReader(stdin)
	env.Stdout, env.Stderr = &o, &e
	env.Getenv = func(k string) string { return r.env[k] }
	env.Sleep = func(time.Duration) {}
	code := Run(args, env)
	r.out = append(r.out, o.String(), e.String())
	return code, o.String(), e.String()
}

func (r *rig) assertNoSecrets() {
	r.t.Helper()
	for _, o := range r.out {
		for _, s := range []string{mock.AgentToken, mock.DeviceToken, "Bearer "} {
			if strings.Contains(o, s) {
				r.t.Fatalf("secret %q appeared in output: %q", s[:6], o)
			}
		}
	}
}

func (r *rig) signIn() {
	r.t.Helper()
	if code, _, e := r.run(mock.AgentToken+"\n", "login", "--with-token"); code != 0 {
		r.t.Fatalf("login failed %d: %s", code, e)
	}
}

func TestVersionJSON(t *testing.T) {
	r := newRig(t)
	code, out, _ := r.run("", "version", "--json")
	var v map[string]string
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil || v["version"] == "" || v["os"] == "" {
		t.Fatalf("code=%d out=%q", code, out)
	}
}

func TestHelpAndUsageCodes(t *testing.T) {
	r := newRig(t)
	if code, out, _ := r.run("", "--help"); code != 0 || !strings.Contains(out, "pew <command>") || !strings.Contains(out, "orchestration") {
		t.Fatalf("help: %d %q", code, out)
	}
	if code, _, _ := r.run(""); code != ExitUsage {
		t.Fatalf("bare pewter should exit 2, got %d", code)
	}
	if code, _, e := r.run("", "frobnicate"); code != ExitUsage || !strings.Contains(e, "unknown command") {
		t.Fatalf("unknown: %d %q", code, e)
	}
	if code, out, _ := r.run("", "help", "login"); code != 0 || !strings.Contains(out, "device-code") {
		t.Fatalf("help login: %d %q", code, out)
	}
}

func TestPlannedCommandsAreGated(t *testing.T) {
	r := newRig(t)
	for _, c := range []string{"buy", "ask", "sell", "rfq", "accept", "send"} {
		code, _, e := r.run("", c)
		if code != ExitUsage || !strings.Contains(e, "unavailable in this release") {
			t.Fatalf("%s: code=%d err=%q", c, code, e)
		}
	}
}

func TestLoginWithTokenStoresSecurely(t *testing.T) {
	r := newRig(t)
	code, out, e := r.run(mock.AgentToken+"\n", "login", "--with-token")
	if code != 0 || !strings.Contains(out, "Signed in as Demo Seller") {
		t.Fatalf("code=%d out=%q err=%q", code, out, e)
	}
	// token file: 0600; config.json holds no secret
	fi, err := os.Stat(filepath.Join(r.dir, "tokens", "default"))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("token file: %v %v", err, fi)
	}
	cfg, _ := os.ReadFile(filepath.Join(r.dir, "config.json"))
	if strings.Contains(string(cfg), "mock0000") || strings.Contains(string(cfg), "mock_agent") {
		t.Fatalf("config.json contains the token: %s", cfg)
	}
	r.assertNoSecrets()
	// whoami works from the stored token
	if code, out, _ := r.run("", "whoami"); code != 0 || !strings.Contains(out, "sandbox") {
		t.Fatalf("whoami: %d %q", code, out)
	}
	r.assertNoSecrets()
}

func TestLoginWithTokenRejects(t *testing.T) {
	r := newRig(t)
	cases := map[string]string{
		"":                                   "no token",
		"bad token with spaces 1234567890\n": "unexpected shape",
	}
	for in, want := range cases {
		code, _, e := r.run(in, "login", "--with-token")
		if code != ExitUsage || !strings.Contains(e, want) {
			t.Fatalf("in=%q code=%d err=%q want %q", in, code, e, want)
		}
	}
	// a syntactically fine but wrong token is rejected by the server and not stored
	if code, _, _ := r.run("tok_wrong000000000000000000\n", "login", "--with-token"); code != ExitAuth {
		t.Fatalf("wrong token code=%d", code)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "tokens", "default")); err == nil {
		t.Fatal("rejected token was stored")
	}
}

func TestDeviceFlowSuccess(t *testing.T) {
	r := newRig(t)
	r.mock.SlowDownOnce = true
	code, out, e := r.run("", "login")
	if code != 0 || !strings.Contains(out, "Signed in as") {
		t.Fatalf("code=%d out=%q err=%q", code, out, e)
	}
	if !strings.Contains(e, "WXYZ-4821") || !strings.Contains(e, "/device") {
		t.Fatalf("code and link not shown: %q", e)
	}
	r.assertNoSecrets()
	tok, _ := os.ReadFile(filepath.Join(r.dir, "tokens", "default"))
	if strings.TrimSpace(string(tok)) != mock.DeviceToken {
		t.Fatal("device token not stored")
	}
	if code, _, _ := r.run("", "balance"); code != 0 {
		t.Fatalf("balance after device login: %d", code)
	}
}

func TestDeviceFlowFailures(t *testing.T) {
	r := newRig(t)
	r.mock.Deny = true
	if code, _, e := r.run("", "login"); code != ExitError || !strings.Contains(e, "declined") {
		t.Fatalf("deny: %d %q", code, e)
	}
	r.mock.Deny, r.mock.Expire = false, true
	if code, _, e := r.run("", "login"); code != ExitError || !strings.Contains(e, "expired") {
		t.Fatalf("expire: %d %q", code, e)
	}
	r.mock.Expire, r.mock.VerifyURI = false, "https://evil.example.net/device"
	if code, _, e := r.run("", "login"); code != ExitError || !strings.Contains(e, "refusing") {
		t.Fatalf("evil verification host: %d %q", code, e)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "tokens", "default")); err == nil {
		t.Fatal("token stored after failed login")
	}
}

func TestDeviceFlowUnavailableOnRealServer(t *testing.T) {
	// A server without the proposed device routes answers 404 (a server without those routes).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(404)
		w.Write([]byte("<html>404</html>"))
	}))
	defer srv.Close()
	r := newRig(t)
	r.env["PEWTER_API_URL"] = srv.URL + "/api/v1"
	code, _, e := r.run("", "login")
	if code != ExitError || !strings.Contains(e, "does not offer browser sign-in") || !strings.Contains(e, "--with-token") {
		t.Fatalf("code=%d err=%q", code, e)
	}
}

func TestReadCommandsJSONAndTables(t *testing.T) {
	r := newRig(t)
	r.signIn()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"orders"}, "ORD-MOCK0001"},
		{[]string{"orders", "--status", "matched"}, "ORD-MOCK0002"},
		{[]string{"listings"}, "LST-MOCK0001"},
		{[]string{"balance"}, "Sandbox balance (simulated funds"},
		{[]string{"instruments", "--gpu", "H100"}, "gpu:H100:od:US:now"},
		{[]string{"verify", "LST-MOCK0001"}, "simulated validation"},
		{[]string{"status"}, "ok"},
	} {
		code, out, e := r.run("", c.args...)
		if code != 0 || !strings.Contains(out, c.want) {
			t.Fatalf("%v: code=%d out=%q err=%q", c.args, code, out, e)
		}
	}
	// filters
	if _, out, _ := r.run("", "orders", "--status", "matched"); strings.Contains(out, "ORD-MOCK0001") {
		t.Fatal("status filter ignored")
	}
	// --json is valid JSON, either position
	for _, args := range [][]string{{"orders", "--json"}, {"--json", "orders"}} {
		code, out, _ := r.run("", args...)
		var rows []map[string]any
		if code != 0 || json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 2 {
			t.Fatalf("%v: %d %q", args, code, out)
		}
	}
	code, out, _ := r.run("", "balance", "--json")
	var w map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &w) != nil || w["sandbox"] != true {
		t.Fatalf("balance json: %q", out)
	}
	r.assertNoSecrets()
}

func TestHostileTextNeverReachesTerminal(t *testing.T) {
	r := newRig(t)
	r.signIn()
	_, out, _ := r.run("", "listings")
	if strings.Contains(out, "\x1b") || strings.Contains(out, "\x07") {
		t.Fatalf("escape sequence in table output: %q", out)
	}
	_, jo, _ := r.run("", "listings", "--json")
	if strings.Contains(jo, "\x1b") {
		t.Fatalf("raw ESC in JSON output")
	}
}

func TestUnauthenticatedAndEnvToken(t *testing.T) {
	r := newRig(t)
	if code, _, e := r.run("", "orders"); code != ExitAuth || !strings.Contains(e, "pewter login") {
		t.Fatalf("code=%d err=%q", code, e)
	}
	// JSON error shape on stderr
	code, out, e := r.run("", "orders", "--json")
	var je map[string]map[string]any
	if code != ExitAuth || out != "" || json.Unmarshal([]byte(e), &je) != nil || je["error"]["code"] != "unauthorized" {
		t.Fatalf("json error: %d out=%q err=%q", code, out, e)
	}
	// PEWTER_TOKEN works with nothing stored
	r.env["PEWTER_TOKEN"] = mock.AgentToken
	if code, _, _ := r.run("", "orders"); code != 0 {
		t.Fatalf("env token: %d", code)
	}
	r.env["PEWTER_TOKEN"] = "bad token!"
	if code, _, _ := r.run("", "orders"); code != ExitAuth {
		t.Fatalf("bad env token: %d", code)
	}
	r.assertNoSecrets()
}

func TestLogoutRemovesToken(t *testing.T) {
	r := newRig(t)
	r.signIn()
	if code, _, _ := r.run("", "logout"); code != 0 {
		t.Fatal("logout failed")
	}
	if code, _, _ := r.run("", "orders"); code != ExitAuth {
		t.Fatalf("still signed in: %d", code)
	}
}

func TestAPIURLRules(t *testing.T) {
	r := newRig(t)
	for _, u := range []string{"http://example.com/api/v1", "ftp://x/y", "https://user:pw@example.com/api/v1"} {
		code, _, e := r.run("", "--api-url", u, "status")
		if code == 0 || e == "" {
			t.Fatalf("%s accepted", u)
		}
	}
	if code, _, _ := r.run("", "--api-url", r.srv.URL+"/api/v1", "status"); code != 0 {
		t.Fatal("loopback http should be allowed")
	}
}

func TestNetworkErrorExitCode(t *testing.T) {
	r := newRig(t)
	r.env["PEWTER_API_URL"] = "http://127.0.0.1:1/api/v1"
	if code, _, e := r.run("", "status"); code != ExitNetwork || !strings.Contains(e, "network error") {
		t.Fatalf("code=%d err=%q", code, e)
	}
}

func TestServerNeverSawMissingIdempotency(t *testing.T) {
	r := newRig(t)
	r.run("", "login")
	if len(r.mock.Seen.IdemMissing) != 0 {
		t.Fatalf("POST without Idempotency-Key: %v", r.mock.Seen.IdemMissing)
	}
}

func TestOnlyReadsAfterLogin(t *testing.T) {
	r := newRig(t)
	r.signIn()
	for _, c := range [][]string{{"orders"}, {"listings"}, {"balance"}, {"whoami"}, {"verify", "LST-MOCK0001"}, {"instruments"}, {"status"}} {
		r.run("", c...)
	}
	for _, req := range r.mock.Seen.Requests {
		if strings.HasPrefix(req, "POST") || strings.HasPrefix(req, "PUT") || strings.HasPrefix(req, "DELETE") || strings.HasPrefix(req, "PATCH") {
			t.Fatalf("a non-read request was sent: %s", req)
		}
	}
}

func TestCompletion(t *testing.T) {
	r := newRig(t)
	for _, sh := range []string{"bash", "zsh"} {
		if code, out, _ := r.run("", "completion", sh); code != 0 || !strings.Contains(out, "pewter") || !strings.Contains(out, "pew") {
			t.Fatalf("%s: %d", sh, code)
		}
	}
	if code, _, _ := r.run("", "completion", "fish"); code != ExitUsage {
		t.Fatal("fish should be a usage error")
	}
}
