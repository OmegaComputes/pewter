// Package mock is a small in-memory stand-in for the API routes the CLI uses,
// for tests and local trials. All data is made up. Tokens here are fake and
// only valid against this mock.
package mock

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

const (
	// AgentToken is accepted as a Bearer token by the mock.
	AgentToken = "mock_agent_token_0000000000"
	// DeviceToken is what the mock device flow issues.
	DeviceToken = "mock_device_token_000000000"
)

// Server is the mock. Zero value is not usable; use New.
type Server struct {
	mu            sync.Mutex
	PollsToAccept int    // number of "authorization_pending" answers before approval
	Deny          bool   // device flow answers access_denied
	Expire        bool   // device flow answers expired_token
	SlowDownOnce  bool   // first poll answers slow_down
	VerifyURI     string // override verification_uri (tests)
	polls         int
	slowed        bool
	// Seen records what the mock received, for assertions. Tokens are never stored.
	Seen struct {
		Requests    []string
		NoRedirect  bool
		LastAuthOK  bool
		IdemMissing []string
	}
}

// New returns a mock that approves the device code after 2 polls.
func New() *Server { return &Server{PollsToAccept: 2} }

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

func (s *Server) authed(r *http.Request) bool {
	h := r.Header.Get("Authorization")
	t := strings.TrimPrefix(h, "Bearer ")
	return h != "" && (t == AgentToken || t == DeviceToken)
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
			data(w, map[string]any{"access_token": DeviceToken, "token_type": "Bearer", "expires_in": 2592000})
		}
	case !s.authed(r):
		w.Header().Set("WWW-Authenticate", "Bearer")
		errJSON(w, 401, "unauthorized", "Missing or invalid token")
	case r.Method == "GET" && path == "/me":
		data(w, map[string]any{
			"actor":        map[string]any{"via": "api_key", "role": "agent", "principal_id": "PRN-MOCK", "agent_id": "AGT-MOCK"},
			"principal":    map[string]any{"id": "PRN-MOCK", "display_name": "Demo Seller", "email": "demo@example.invalid", "country": "PT"},
			"agent":        map[string]any{"id": "AGT-MOCK", "name": "Pewter CLI (mock)", "kind": "mcp_client", "status": "active"},
			"sandbox_only": true,
		})
	case r.Method == "GET" && path == "/wallet":
		data(w, map[string]any{"sandbox": true, "currency": "USD", "balance_usd": 100070.4, "reserved_usd": 0, "available_usd": 100070.4,
			"entries": []any{map[string]any{"deal_id": "DL-MOCK1", "kind": "sandbox_trade", "side": "sell", "status": "settled", "amount_usd": 80, "net_usd": 70.4}},
			"note":    "Simulated funds."})
	case r.Method == "GET" && path == "/orders":
		rows := []map[string]any{
			{"id": "ORD-MOCK0001", "side": "bid", "book": "sandbox", "instrument_id": "gpu:H100:od:US:now", "price_usd_per_unit": 2.1, "gpus": 8, "hours": 24, "status": "open"},
			{"id": "ORD-MOCK0002", "side": "ask", "book": "sandbox", "instrument_id": "gpu:H200:od:EU:now", "price_usd_per_unit": 3.4, "gpus": 16, "hours": 24, "status": "matched"},
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
			{"id": "LST-MOCK0001", "status": "live", "title": "8x H100 SXM, Lisbon",
				"capability": map[string]any{"gpu": map[string]any{"model": "H100", "count_per_node": 8, "nodes": 1}, "price": map[string]any{"amount_usd": 2.4}},
				"assurance":  map[string]any{"tier": "S1", "validation": "passed", "simulated": true}},
			// Hostile title: terminal escape sequences from untrusted text must not reach a terminal.
			{"id": "LST-MOCK0002", "status": "draft", "title": "evil \x1b[2J\x1b]0;pwned\x07 title",
				"capability": map[string]any{"gpu": map[string]any{"model": "L40S", "count_per_node": 4, "nodes": 1}, "price": map[string]any{"amount_usd": 0.9}},
				"assurance":  map[string]any{"tier": "S0", "validation": "unbenchmarked"}},
		})
	case r.Method == "GET" && path == "/listings/LST-MOCK0001/verification":
		data(w, map[string]any{
			"listing":             map[string]any{"id": "LST-MOCK0001", "status": "live", "assurance": map[string]any{"tier": "S1", "validation": "passed", "simulated": true}},
			"runs":                []any{map[string]any{"id": "RUN-MOCK1", "kind": "onboarding", "state": "passed", "tier_after": "S1", "finished_at": "2026-10-25T10:00:00Z"}},
			"validator_available": true,
		})
	default:
		errJSON(w, 404, "not_found", "Not found")
	}
}
