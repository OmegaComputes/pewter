// Package mock is a small in-memory stand-in for the API routes the CLI uses,
// for tests and local trials. All data is made up. Tokens here are fake and
// only valid against this mock.
package mock

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
)

const (
	// AgentToken is accepted as a Bearer token by the mock. It is a fake pck_ key.
	AgentToken = "pck_mock_agent_000000000000"
	// DeviceToken is what the mock device flow issues. It is a fake pck_ key.
	DeviceToken = "pck_mock_device_00000000000"
	// CredentialID is the credential id returned by /me and device token.
	CredentialID = "key_mock_cli"
)

// Server is the mock. Zero value is not usable; use New.
type Server struct {
	mu                 sync.Mutex
	PollsToAccept      int    // number of "authorization_pending" answers before approval
	Deny               bool   // device flow answers access_denied
	Expire             bool   // device flow answers expired_token
	SlowDownOnce       bool   // first poll answers slow_down
	VerifyURI          string // override verification_uri (tests)
	RevokeUnauthorized bool   // self-revoke answers 401 without forgetting the token
	polls              int
	slowed             bool
	revoked            map[string]bool
	// Seen records what the mock received, for assertions. Tokens are never stored.
	Seen struct {
		Requests    []string
		NoRedirect  bool
		LastAuthOK  bool
		IdemMissing []string
		ClientID    string
		ClientName  string
		Scopes      []string
		Revokes     int
	}
}

// New returns a mock that approves the device code after 2 polls.
func New() *Server { return &Server{PollsToAccept: 2, revoked: map[string]bool{}} }

func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Request-Id", "req_mock_0001")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func data(w http.ResponseWriter, v any) { write(w, 200, map[string]any{"data": v}) }

func errJSON(w http.ResponseWriter, status int, code, msg string) {
	write(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg, "request_id": "req_mock_0001"}})
}

func bearer(r *http.Request) string {
	return strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
}

func (s *Server) authed(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	t := bearer(r)
	if h == "" || s.revoked[t] {
		return false
	}
	return t == AgentToken || t == DeviceToken
}

// ServeHTTP serves /api/v1/*.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := strings.TrimPrefix(r.URL.Path, "/api/v1")
	s.Seen.Requests = append(s.Seen.Requests, r.Method+" "+path)
	if (r.Method == "POST" || r.Method == "PUT") && r.Header.Get("Idempotency-Key") == "" {
		s.Seen.IdemMissing = append(s.Seen.IdemMissing, path)
		errJSON(w, 400, "idempotency_key_required", "Idempotency-Key is required")
		return
	}
	switch {
	case r.Method == "GET" && path == "/health":
		write(w, 200, map[string]any{"status": "ok", "api_version": "v1", "time": "2026-10-26T12:00:00.000Z", "uptime_s": 1})
	case r.Method == "GET" && path == "/instruments":
		all := []map[string]any{
			{"id": "gpu:H100:od:US:now", "kind": "gpu", "canonical": "H100", "term": "od", "region": "US", "suggested_block_gpus": 8},
			{"id": "gpu:H200:od:EU:now", "kind": "gpu", "canonical": "H200", "term": "od", "region": "EU", "suggested_block_gpus": 8},
			{"id": "gpu:B200:1w:US:now", "kind": "gpu", "canonical": "B200", "term": "1w", "region": "US", "suggested_block_gpus": 8},
		}
		var out []map[string]any
		for _, i := range all {
			if g := r.URL.Query().Get("gpu"); g != "" && !strings.EqualFold(g, i["canonical"].(string)) {
				continue
			}
			if rg := r.URL.Query().Get("region"); rg != "" && rg != i["region"] {
				continue
			}
			out = append(out, i)
		}
		data(w, out)
	case r.Method == "POST" && path == "/auth/device/code":
		var body struct {
			ClientID   string   `json:"client_id"`
			ClientName string   `json:"client_name"`
			Scopes     []string `json:"scopes"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
			errJSON(w, 400, "bad_request", "Body is not valid JSON")
			return
		}
		s.Seen.ClientID = body.ClientID
		s.Seen.ClientName = body.ClientName
		s.Seen.Scopes = append([]string(nil), body.Scopes...)
		if strings.TrimSpace(body.ClientName) == "" || len(body.Scopes) == 0 {
			errJSON(w, 422, "validation_error", "client_name and scopes are required")
			return
		}
		uri := s.VerifyURI
		if uri == "" {
			uri = "http://" + r.Host + "/device"
		}
		s.polls, s.slowed = 0, false
		data(w, map[string]any{"device_code": "dev_mock_code_0001", "user_code": "WXYZ-4821", "verification_uri": uri,
			"verification_uri_complete": uri + "?code=WXYZ-4821", "expires_in": 600, "interval": 1})
	case r.Method == "POST" && path == "/auth/device/token":
		s.polls++
		switch {
		case s.Expire:
			errJSON(w, 400, "expired_token", "code expired")
		case s.Deny:
			errJSON(w, 400, "access_denied", "declined")
		case s.SlowDownOnce && !s.slowed:
			s.slowed = true
			errJSON(w, 400, "slow_down", "slow down")
		case s.polls <= s.PollsToAccept:
			errJSON(w, 400, "authorization_pending", "waiting for approval")
		default:
			data(w, map[string]any{"access_token": DeviceToken, "token_type": "Bearer", "expires_in": 7776000, "scope": "deals:read market:read verification:read", "credential_id": CredentialID})
		}
	case r.Method == "POST" && path == "/auth/credentials/self/revoke":
		if !s.authed(r) || s.RevokeUnauthorized {
			w.Header().Set("WWW-Authenticate", "Bearer")
			errJSON(w, 401, "unauthorized", "Invalid, expired or revoked token")
			return
		}
		if s.revoked == nil {
			s.revoked = map[string]bool{}
		}
		s.revoked[bearer(r)] = true
		s.Seen.Revokes++
		data(w, map[string]any{"revoked": true, "credential": map[string]any{"id": CredentialID, "kind": "cli"}})
	case !s.authed(r):
		w.Header().Set("WWW-Authenticate", "Bearer")
		errJSON(w, 401, "unauthorized", "Missing or invalid token")
	case r.Method == "GET" && path == "/me":
		data(w, map[string]any{
			"actor": map[string]any{"via": "api_key", "role": "agent", "principal_id": "PRN-MOCK", "agent_id": "AGT-MOCK"},
			"credential": map[string]any{
				"id": CredentialID, "kind": "cli",
				"scopes":       []string{"deals:read", "market:read", "verification:read"},
				"expires_at":   "2027-01-25T00:00:00.000Z",
				"last_used_at": nil,
				"sandbox_only": true,
			},
			"principal":    map[string]any{"id": "PRN-MOCK", "display_name": "Demo Seller", "email": "demo@example.invalid", "phone": "+10000000000", "company": "Example Co", "country": "PT"},
			"agent":        map[string]any{"id": "AGT-MOCK", "name": "Pewter CLI (mock)", "kind": "cli", "status": "active"},
			"sandbox_only": true,
		})
	case r.Method == "GET" && path == "/wallet":
		data(w, map[string]any{"sandbox": true, "currency": "USD", "balance_usd": 100070.4, "reserved_usd": 0, "available_usd": 100070.4,
			"principal_id": "PRN-MOCK", "agent_id": "AGT-MOCK",
			"entries": []any{map[string]any{"deal_id": "DL-MOCK1", "kind": "sandbox_trade", "side": "sell", "status": "settled", "amount_usd": 80, "net_usd": 70.4, "principal_id": "PRN-MOCK"}},
			"note":    "Simulated funds."})
	case r.Method == "GET" && path == "/orders":
		rows := []map[string]any{
			{"id": "ORD-MOCK0001", "principal_id": "PRN-MOCK", "agent_id": "AGT-MOCK", "side": "bid", "book": "sandbox", "instrument_id": "gpu:H100:od:US:now", "price_usd_per_unit": 2.1, "gpus": 8, "hours": 24, "status": "open"},
			{"id": "ORD-MOCK0002", "principal_id": "PRN-MOCK", "agent_id": "AGT-MOCK", "side": "ask", "book": "sandbox", "instrument_id": "gpu:H200:od:EU:now", "price_usd_per_unit": 3.4, "gpus": 16, "hours": 24, "status": "matched"},
		}
		var out []map[string]any
		for _, o := range rows {
			if st := r.URL.Query().Get("status"); st != "" && st != o["status"] {
				continue
			}
			out = append(out, o)
		}
		data(w, out)
	case r.Method == "GET" && path == "/listings":
		data(w, []map[string]any{
			{"id": "LST-MOCK0001", "principal_id": "PRN-MOCK", "agent_id": "AGT-MOCK", "status": "live", "title": "8x H100 SXM, Lisbon",
				"capability": map[string]any{"gpu": map[string]any{"model": "H100", "count_per_node": 8, "nodes": 1}, "price": map[string]any{"amount_usd": 2.4}},
				"assurance":  map[string]any{"tier": "S1", "validation": "passed", "simulated": true}},
			// Hostile title: terminal escape sequences from untrusted text must not reach a terminal.
			{"id": "LST-MOCK0002", "status": "draft", "title": "evil \x1b[2J\x1b]0;pwned\x07 title",
				"capability": map[string]any{"gpu": map[string]any{"model": "L40S", "count_per_node": 4, "nodes": 1}, "price": map[string]any{"amount_usd": 0.9}},
				"assurance":  map[string]any{"tier": "S0", "validation": "unbenchmarked"}},
		})
	case r.Method == "GET" && path == "/listings/LST-MOCK0001/verification":
		data(w, map[string]any{
			"listing":             map[string]any{"id": "LST-MOCK0001", "principal_id": "PRN-MOCK", "agent_id": "AGT-MOCK", "status": "live", "assurance": map[string]any{"tier": "S1", "validation": "passed", "simulated": true}},
			"runs":                []any{map[string]any{"id": "RUN-MOCK1", "kind": "onboarding", "state": "passed", "tier_after": "S1", "finished_at": "2026-10-25T10:00:00Z"}},
			"validator_available": true,
		})
	default:
		errJSON(w, 404, "not_found", "Not found")
	}
}
