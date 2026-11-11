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
	if code, out, _ := r.run("", "--help"); code != 0 || !strings.Contains(out, "pew <command>") || !strings.Contains(out, "orchestration") || strings.Contains(out, "Prototype") {
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
		"omk_examplekey000000000000\n":       "omk_",
		"pws_examplesession000000000\n":      "pws_",
		"tok_wrong000000000000000000\n":      "pck_",
	}
	for in, want := range cases {
		before := len(r.mock.Seen.Requests)
		code, _, e := r.run(in, "login", "--with-token")
		if code != ExitUsage || !strings.Contains(e, want) {
			t.Fatalf("in=%q code=%d err=%q want %q", in, code, e, want)
		}
		if strings.Contains(e, strings.TrimSpace(in)) && strings.TrimSpace(in) != "" {
			t.Fatalf("rejected token echoed: %q", e)
		}
		if len(r.mock.Seen.Requests) != before {
			t.Fatalf("rejected token was sent: %v", r.mock.Seen.Requests[before:])
		}
	}
	// a pck_ the server does not know is not stored
	if code, _, e := r.run("pck_unknown_token_0000000000\n", "login", "--with-token"); code != ExitAuth {
		t.Fatalf("unknown pck_ code=%d err=%q", code, e)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "tokens", "default")); err == nil {
		t.Fatal("rejected token was stored")
	}
	joined := strings.Join(r.mock.Seen.Requests, " ")
	if !strings.Contains(joined, "GET /me") {
		t.Fatal("a well formed pck_ should be checked with GET /me")
	}
}

func TestDeviceFlowSuccess(t *testing.T) {
	r := newRig(t)
	r.mock.SlowDownOnce = true
	code, out, e := r.run("", "login")
	if code != 0 || !strings.Contains(out, "Signed in as") {
		t.Fatalf("code=%d out=%q err=%q", code, out, e)
	}
	if !strings.Contains(e, "WXYZ-4821") || !strings.Contains(e, "/device") || !strings.Contains(e, "Pewter app") {
		t.Fatalf("code and link not shown: %q", e)
	}
	if r.mock.Seen.ClientID != "pewter-cli" || strings.Join(r.mock.Seen.Scopes, ",") != "deals:read,market:read,verification:read" {
		t.Fatalf("device body: id=%q scopes=%v name=%q", r.mock.Seen.ClientID, r.mock.Seen.Scopes, r.mock.Seen.ClientName)
	}
	if r.mock.Seen.ClientName == "" || len([]rune(r.mock.Seen.ClientName)) > 60 || strings.ContainsAny(r.mock.Seen.ClientName, "\x00\x1b") {
		t.Fatalf("client_name: %q", r.mock.Seen.ClientName)
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
	r.env["PEWTER_TOKEN"] = "omk_examplekey000000000000"
	if code, _, e := r.run("", "orders"); code != ExitAuth || !strings.Contains(e, "omk_") || strings.Contains(e, "omk_examplekey") {
		t.Fatalf("omk env: %d %q", code, e)
	}
	r.env["PEWTER_TOKEN"] = "pws_examplesession000000000"
	if code, _, e := r.run("", "orders"); code != ExitAuth || !strings.Contains(e, "pws_") {
		t.Fatalf("pws env: %d %q", code, e)
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
	for _, u := range []string{"https://api.omegacompute.com/api/v1", "https://evil.example/api/v1"} {
		before := len(r.mock.Seen.Requests)
		code, _, e := r.run("", "--api-url", u, "status")
		if code != ExitUsage || !strings.Contains(e, "staging API only") || !strings.Contains(e, "production is not enabled") {
			t.Fatalf("%s: code=%d err=%q", u, code, e)
		}
		if len(r.mock.Seen.Requests) != before {
			t.Fatalf("%s sent a request", u)
		}
	}
	// A saved production URL is refused the same way, with no env override.
	r.env["PEWTER_API_URL"] = ""
	if err := os.WriteFile(filepath.Join(r.dir, "config.json"), []byte(`{"api_url":"https://api.omegacompute.com/api/v1"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code, _, e := r.run("", "status"); code != ExitUsage || !strings.Contains(e, "staging API only") {
		t.Fatalf("saved config: code=%d err=%q", code, e)
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
	if code, out, _ := r.run("", "completion", "bash"); code != 0 || !strings.Contains(out, "--with-token") {
		t.Fatalf("bash completion missing --with-token: %d %q", code, out)
	}
	if code, _, _ := r.run("", "completion", "fish"); code != ExitUsage {
		t.Fatal("fish should be a usage error")
	}
}

func TestCleanClientName(t *testing.T) {
	if cleanClientName("") != "pewter-cli" || cleanClientName("\x1b\n\t") != "pewter-cli" {
		t.Fatal("empty name should fall back")
	}
	if got := cleanClientName("box\x1b[2Jhost"); got != "box[2Jhost" {
		t.Fatalf("got %q", got)
	}
	if n := len([]rune(cleanClientName(strings.Repeat("ü", 80)))); n != 60 {
		t.Fatalf("runes=%d", n)
	}
}

func TestWhoamiProjectsIdentityAway(t *testing.T) {
	r := newRig(t)
	r.signIn()
	code, out, e := r.run("", "whoami")
	if code != 0 || !strings.Contains(out, "Demo Seller") || !strings.Contains(out, mock.CredentialID) || !strings.Contains(out, "deals:read") || !strings.Contains(out, "127.0.0.1") {
		t.Fatalf("whoami: %d %q %q", code, out, e)
	}
	for _, hidden := range []string{"demo@example.invalid", "+10000000000", "Example Co", "PRN-MOCK", "principal_id", "AGT-MOCK"} {
		if strings.Contains(out, hidden) {
			t.Fatalf("whoami leaked %q in %q", hidden, out)
		}
	}
	code, out, _ = r.run("", "whoami", "--json")
	var v map[string]any
	if code != 0 || json.Unmarshal([]byte(out), &v) != nil {
		t.Fatalf("json: %d %q", code, out)
	}
	for _, k := range []string{"email", "phone", "company", "principal_id", "agent_id", "actor", "principal"} {
		if _, ok := v[k]; ok {
			t.Fatalf("json has %s: %s", k, out)
		}
	}
	if v["display_name"] != "Demo Seller" || v["credential_id"] != mock.CredentialID || v["sandbox"] != true || v["api_host"] != "127.0.0.1" || v["expires_at"] == "" {
		t.Fatalf("view: %#v", v)
	}
}

func TestJSONDropsPrincipalAndAgent(t *testing.T) {
	r := newRig(t)
	r.signIn()
	for _, args := range [][]string{{"listings", "--json"}, {"orders", "--json"}, {"balance", "--json"}, {"verify", "LST-MOCK0001", "--json"}} {
		_, out, _ := r.run("", args...)
		for _, hidden := range []string{"principal_id", "agent_id", "PRN-MOCK", "AGT-MOCK", "demo@example.invalid"} {
			if strings.Contains(out, hidden) {
				t.Fatalf("%v leaked %q in %s", args, hidden, out)
			}
		}
	}
	_, orders, _ := r.run("", "orders", "--json")
	if !strings.Contains(orders, "ORD-MOCK0001") {
		t.Fatal(orders)
	}
	_, listings, _ := r.run("", "listings", "--json")
	if !strings.Contains(listings, "LST-MOCK0001") {
		t.Fatal(listings)
	}
}

func TestLogoutRevokesThenDeletes(t *testing.T) {
	r := newRig(t)
	r.signIn()
	code, out, e := r.run("", "logout")
	if code != 0 || strings.Contains(out, "Prototype") || !strings.Contains(out, "revoked") {
		t.Fatalf("logout: %d %q %q", code, out, e)
	}
	if r.mock.Seen.Revokes != 1 {
		t.Fatalf("revokes=%d", r.mock.Seen.Revokes)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "tokens", "default")); err == nil {
		t.Fatal("token file remains")
	}
	if code, _, _ := r.run("", "orders"); code != ExitAuth {
		t.Fatalf("still signed in: %d", code)
	}
}

func TestLogout401StillDeletes(t *testing.T) {
	r := newRig(t)
	r.signIn()
	r.mock.RevokeUnauthorized = true
	code, out, e := r.run("", "logout")
	if code != 0 || !strings.Contains(out, "already revoked") {
		t.Fatalf("logout: %d %q %q", code, out, e)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "tokens", "default")); err == nil {
		t.Fatal("token file remains after 401")
	}
}

func TestLogoutEnvTokenDoesNotDeleteStored(t *testing.T) {
	r := newRig(t)
	r.signIn()
	r.env["PEWTER_TOKEN"] = mock.DeviceToken
	code, out, e := r.run("", "logout")
	if code != 0 || !strings.Contains(out, "Unset PEWTER_TOKEN") || !strings.Contains(out, "left in place") {
		t.Fatalf("logout: %d %q %q", code, out, e)
	}
	b, err := os.ReadFile(filepath.Join(r.dir, "tokens", "default"))
	if err != nil || strings.TrimSpace(string(b)) != mock.AgentToken {
		t.Fatalf("stored token changed: %v %q", err, b)
	}
	delete(r.env, "PEWTER_TOKEN")
	if code, _, _ := r.run("", "whoami"); code != 0 {
		t.Fatal("stored token should still work")
	}
}

func (r *rig) storedToken() string {
	r.t.Helper()
	b, err := os.ReadFile(filepath.Join(r.dir, "tokens", "default"))
	if err != nil {
		r.t.Fatalf("token file: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// assertStoredUntouched checks that a failed login neither revoked the
// credential on the mock nor replaced it on disk, and that whoami still works.
// from is the Seen.Requests length before the attempt. revokes is Seen.Revokes then.
func (r *rig) assertStoredUntouched(from, revokes int, token string) {
	r.t.Helper()
	if r.mock.Seen.Revokes != revokes {
		r.t.Fatalf("stored credential revoked: revokes %d -> %d requests %v", revokes, r.mock.Seen.Revokes, r.mock.Seen.Requests[from:])
	}
	for _, req := range r.mock.Seen.Requests[from:] {
		if strings.Contains(req, "revoke") {
			r.t.Fatalf("revoke during failed login: %v", r.mock.Seen.Requests[from:])
		}
	}
	if got := r.storedToken(); got != token {
		r.t.Fatalf("stored token changed: %q", got)
	}
	if code, out, e := r.run("", "whoami"); code != 0 || !strings.Contains(out, "Demo Seller") {
		r.t.Fatalf("whoami after failed login: %d %q %q", code, out, e)
	}
}

func TestWithTokenRefusalDoesNotRevokeStored(t *testing.T) {
	r := newRig(t)
	r.signIn()
	before := len(r.mock.Seen.Requests)
	revokes := r.mock.Seen.Revokes
	code, _, e := r.run("omk_examplekey000000000000\n", "login", "--with-token")
	if code == 0 || !strings.Contains(e, "omk_") {
		t.Fatalf("code=%d err=%q", code, e)
	}
	// Shape and prefix are refused locally. Nothing is sent, including revoke.
	if len(r.mock.Seen.Requests) != before {
		t.Fatalf("refused token touched the server: %v", r.mock.Seen.Requests[before:])
	}
	r.assertStoredUntouched(before, revokes, mock.AgentToken)
}

func TestWithTokenRejectedByMeDoesNotRevokeStored(t *testing.T) {
	r := newRig(t)
	r.signIn()
	before := len(r.mock.Seen.Requests)
	revokes := r.mock.Seen.Revokes
	code, _, e := r.run("pck_unknown_token_0000000000\n", "login", "--with-token")
	if code != ExitAuth {
		t.Fatalf("code=%d err=%q", code, e)
	}
	sawMe := false
	for _, req := range r.mock.Seen.Requests[before:] {
		if req == "GET /me" {
			sawMe = true
		}
	}
	if !sawMe {
		t.Fatalf("rejected pck_ was not checked with GET /me: %v", r.mock.Seen.Requests[before:])
	}
	r.assertStoredUntouched(before, revokes, mock.AgentToken)
}

func TestDeviceDeniedDoesNotRevokeStored(t *testing.T) {
	r := newRig(t)
	r.signIn()
	before := len(r.mock.Seen.Requests)
	revokes := r.mock.Seen.Revokes
	r.mock.Deny = true
	code, _, e := r.run("", "login")
	if code != ExitError || !strings.Contains(e, "declined") {
		t.Fatalf("deny: %d %q", code, e)
	}
	r.assertStoredUntouched(before, revokes, mock.AgentToken)
}

func TestDeviceMeFailureDropsMintedKeyOnly(t *testing.T) {
	r := newRig(t)
	r.signIn()
	before := len(r.mock.Seen.Requests)
	revokes := r.mock.Seen.Revokes
	r.mock.MeUnauthorized = true
	code, _, e := r.run("", "login")
	if code != ExitAuth {
		t.Fatalf("me: %d %q", code, e)
	}
	// The key the device flow just minted is dropped. The stored one is not.
	if r.mock.Seen.Revokes != revokes+1 {
		t.Fatalf("revokes=%d requests=%v", r.mock.Seen.Revokes, r.mock.Seen.Requests[before:])
	}
	revokeAt, meAt := -1, -1
	for i, req := range r.mock.Seen.Requests[before:] {
		switch req {
		case "GET /me":
			meAt = i
		case "POST /auth/credentials/self/revoke":
			revokeAt = i
		}
	}
	if meAt < 0 || revokeAt < meAt {
		t.Fatalf("minted key revoked before GET /me failed: %v", r.mock.Seen.Requests[before:])
	}
	if got := r.storedToken(); got != mock.AgentToken {
		t.Fatalf("stored token changed: %q", got)
	}
	r.mock.MeUnauthorized = false
	if code, out, e := r.run("", "whoami"); code != 0 || !strings.Contains(out, "Demo Seller") {
		t.Fatalf("stored credential broken: %d %q %q", code, out, e)
	}
	r.env["PEWTER_TOKEN"] = mock.DeviceToken
	if code, _, _ := r.run("", "whoami"); code == 0 {
		t.Fatal("minted credential was not revoked")
	}
}

func TestSuccessfulReloginRevokesOldKey(t *testing.T) {
	assertOrder := func(t *testing.T, reqs []string) {
		t.Helper()
		me, rev := -1, -1
		for i, req := range reqs {
			if req == "GET /me" {
				me = i
			}
			if req == "POST /auth/credentials/self/revoke" {
				rev = i
			}
		}
		if me < 0 || rev <= me {
			t.Fatalf("old key revoked before GET /me accepted the new one: %v", reqs)
		}
	}
	t.Run("device", func(t *testing.T) {
		r := newRig(t)
		r.signIn()
		r.mock.Seen.Requests = nil
		r.mock.Seen.Revokes = 0
		if code, _, e := r.run("", "login"); code != 0 {
			t.Fatalf("relogin: %d %q", code, e)
		}
		if r.mock.Seen.Revokes != 1 {
			t.Fatalf("revokes=%d requests=%v", r.mock.Seen.Revokes, r.mock.Seen.Requests)
		}
		assertOrder(t, r.mock.Seen.Requests)
		if got := r.storedToken(); got != mock.DeviceToken {
			t.Fatalf("new token not stored: %q", got)
		}
		if code, out, e := r.run("", "whoami"); code != 0 || !strings.Contains(out, "Demo Seller") {
			t.Fatalf("whoami: %d %q %q", code, out, e)
		}
		r.env["PEWTER_TOKEN"] = mock.AgentToken
		if code, _, _ := r.run("", "whoami"); code == 0 {
			t.Fatal("previous credential still accepted")
		}
	})
	t.Run("with-token", func(t *testing.T) {
		r := newRig(t)
		r.signIn()
		r.mock.Seen.Requests = nil
		r.mock.Seen.Revokes = 0
		if code, _, e := r.run(mock.DeviceToken+"\n", "login", "--with-token"); code != 0 {
			t.Fatalf("relogin: %d %q", code, e)
		}
		if r.mock.Seen.Revokes != 1 {
			t.Fatalf("revokes=%d requests=%v", r.mock.Seen.Revokes, r.mock.Seen.Requests)
		}
		assertOrder(t, r.mock.Seen.Requests)
		if got := r.storedToken(); got != mock.DeviceToken {
			t.Fatalf("new token not stored: %q", got)
		}
		if code, out, e := r.run("", "whoami"); code != 0 || !strings.Contains(out, "Demo Seller") {
			t.Fatalf("whoami: %d %q %q", code, out, e)
		}
		r.env["PEWTER_TOKEN"] = mock.AgentToken
		if code, _, _ := r.run("", "whoami"); code == 0 {
			t.Fatal("previous credential still accepted")
		}
	})
	t.Run("same token", func(t *testing.T) {
		r := newRig(t)
		r.signIn()
		r.mock.Seen.Requests = nil
		r.mock.Seen.Revokes = 0
		if code, _, e := r.run(mock.AgentToken+"\n", "login", "--with-token"); code != 0 {
			t.Fatalf("relogin: %d %q", code, e)
		}
		if r.mock.Seen.Revokes != 0 {
			t.Fatalf("same key was revoked: revokes=%d requests=%v", r.mock.Seen.Revokes, r.mock.Seen.Requests)
		}
		for _, req := range r.mock.Seen.Requests {
			if strings.Contains(req, "revoke") {
				t.Fatalf("revoke request for the key being stored: %v", r.mock.Seen.Requests)
			}
		}
		if got := r.storedToken(); got != mock.AgentToken {
			t.Fatalf("stored token changed: %q", got)
		}
		if code, out, e := r.run("", "whoami"); code != 0 || !strings.Contains(out, "Demo Seller") {
			t.Fatalf("whoami: %d %q %q", code, out, e)
		}
	})
}

func TestExpiredStoredTokenIsNotSent(t *testing.T) {
	r := newRig(t)
	r.signIn()
	cfgPath := filepath.Join(r.dir, "config.json")
	b, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["expires_at"] = "2000-01-01T00:00:00Z"
	out, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, out, 0o600); err != nil {
		t.Fatal(err)
	}
	r.mock.Seen.Requests = nil
	code, _, e := r.run("", "orders")
	if code != ExitAuth || !strings.Contains(e, "expired") {
		t.Fatalf("expired: %d %q", code, e)
	}
	if len(r.mock.Seen.Requests) != 0 {
		t.Fatalf("expired token was sent: %v", r.mock.Seen.Requests)
	}
}

func TestDeviceTokenRejectsNonPCK(t *testing.T) {
	const leaked = "omk_notaccepted0000000000"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/auth/device/code":
			w.Write([]byte(`{"data":{"device_code":"d","user_code":"ABCD-1234","verification_uri":"` + "http://" + r.Host + `/device","expires_in":60,"interval":1}}`))
		case "/api/v1/auth/device/token":
			w.Write([]byte(`{"data":{"access_token":"` + leaked + `","token_type":"Bearer","expires_in":100}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	r := newRig(t)
	r.env["PEWTER_API_URL"] = srv.URL + "/api/v1"
	code, out, e := r.run("", "login")
	if code == 0 || strings.Contains(out+e, leaked) {
		t.Fatalf("code=%d out=%q err=%q", code, out, e)
	}
	if _, err := os.Stat(filepath.Join(r.dir, "tokens", "default")); err == nil {
		t.Fatal("non-pck token stored")
	}
}

func TestStatusPrintsHost(t *testing.T) {
	r := newRig(t)
	code, out, e := r.run("", "status")
	if code != 0 || !strings.Contains(out, "127.0.0.1") || !strings.Contains(out, "ok") {
		t.Fatalf("status: %d %q %q", code, out, e)
	}
}
