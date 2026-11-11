package mock

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func post(t *testing.T, srv *httptest.Server, path, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1"+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-test-0001")
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestDeviceCodeRequiresNameAndScopes(t *testing.T) {
	m := New()
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	for _, body := range []string{
		`{"client_id":"pewter-cli"}`,
		`{"client_id":"pewter-cli","client_name":"box"}`,
		`{"client_id":"pewter-cli","scopes":["market:read"]}`,
		`{"client_id":"pewter-cli","client_name":"  ","scopes":["market:read"]}`,
		`{"client_id":"pewter-cli","client_name":"box","scopes":[]}`,
	} {
		resp := post(t, srv, "/auth/device/code", body)
		b, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 422 || !strings.Contains(string(b), "validation_error") {
			t.Fatalf("body %s: %d %s", body, resp.StatusCode, b)
		}
	}
	resp := post(t, srv, "/auth/device/code", `{"client_id":"pewter-cli","client_name":"box","scopes":["deals:read","market:read","verification:read"]}`)
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 || !strings.Contains(string(b), "WXYZ-4821") || !strings.HasPrefix(DeviceToken, "pck_") {
		t.Fatalf("accepted body: %d %s", resp.StatusCode, b)
	}
}

func TestSelfRevoke(t *testing.T) {
	m := New()
	srv := httptest.NewServer(m)
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/auth/credentials/self/revoke", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", "idem-revoke-1")
	req.Header.Set("Authorization", "Bearer "+AgentToken)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || m.Seen.Revokes != 1 {
		t.Fatalf("revoke: %d revokes=%d", resp.StatusCode, m.Seen.Revokes)
	}
	req, _ = http.NewRequest(http.MethodGet, srv.URL+"/api/v1/me", nil)
	req.Header.Set("Authorization", "Bearer "+AgentToken)
	resp, err = srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("revoked token still accepted: %d", resp.StatusCode)
	}
}
