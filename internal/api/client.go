// Package api is a small client for the Pewter REST API (/api/v1).
//
// Rules it enforces: https only (loopback may use http), no redirects (so a
// token is never replayed to another host), bounded response size, and a
// token that never appears in an error message.
package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const maxBody = 4 << 20 // 4 MiB

// Error is an API error envelope: {"error":{"code","message","details","request_id"}}.
type Error struct {
	Status    int
	Code      string
	Message   string
	RequestID string
	Details   json.RawMessage
}

func (e *Error) Error() string {
	if e.Code == "" {
		return fmt.Sprintf("HTTP %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("%s (HTTP %d): %s", e.Code, e.Status, e.Message)
}

// Client talks to one API base URL such as https://host/api/v1.
type Client struct {
	Base      string
	Token     string
	UserAgent string
	HTTP      *http.Client
}

// ValidateBase checks the base URL: https, or http only for loopback hosts.
func ValidateBase(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid API URL %q", raw)
	}
	if u.User != nil {
		return nil, errors.New("API URL must not contain credentials")
	}
	switch u.Scheme {
	case "https":
	case "http":
		h := u.Hostname()
		ip := net.ParseIP(h)
		if h != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return nil, fmt.Errorf("refusing plain http to %q: use https (http is allowed only for localhost)", h)
		}
	default:
		return nil, fmt.Errorf("unsupported URL scheme %q", u.Scheme)
	}
	return u, nil
}

// New builds a client. The base URL is validated.
func New(base, token, userAgent string) (*Client, error) {
	u, err := ValidateBase(base)
	if err != nil {
		return nil, err
	}
	return &Client{
		Base:      u.String(),
		Token:     token,
		UserAgent: userAgent,
		HTTP: &http.Client{
			Timeout: 20 * time.Second,
			// Never follow redirects: a redirect must not carry the bearer token elsewhere.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}, nil
}

func newKey() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Do sends a request and decodes {"data": ...}. out may be nil. query may be nil.
// POST and PUT get a fresh Idempotency-Key (required by the API).
func (c *Client) Do(ctx context.Context, method, path string, query url.Values, body any, auth bool, out any) error {
	full := c.Base + path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, full, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.UserAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if method == http.MethodPost || method == http.MethodPut {
		req.Header.Set("Idempotency-Key", newKey())
	}
	if auth {
		if c.Token == "" {
			return &Error{Status: 401, Code: "unauthorized", Message: "not signed in (run `pewter login`)"}
		}
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		// url.Error can include the URL; it never includes headers. Keep only the cause.
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return fmt.Errorf("network error: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}
	if len(raw) > maxBody {
		return errors.New("response too large")
	}
	if resp.StatusCode >= 300 {
		return parseError(resp, raw)
	}
	if out == nil {
		return nil
	}
	var env struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("unexpected response (not JSON) from %s", path)
	}
	if env.Data == nil {
		// Some public routes (health) answer without an envelope.
		return json.Unmarshal(raw, out)
	}
	return json.Unmarshal(env.Data, out)
}

func parseError(resp *http.Response, raw []byte) error {
	e := &Error{Status: resp.StatusCode, RequestID: resp.Header.Get("X-Request-Id")}
	var env struct {
		Error struct {
			Code      string          `json:"code"`
			Message   string          `json:"message"`
			Details   json.RawMessage `json:"details"`
			RequestID string          `json:"request_id"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &env) == nil && env.Error.Code != "" {
		e.Code, e.Message, e.Details = env.Error.Code, env.Error.Message, env.Error.Details
		if env.Error.RequestID != "" {
			e.RequestID = env.Error.RequestID
		}
		return e
	}
	e.Message = http.StatusText(resp.StatusCode)
	if resp.StatusCode == 404 {
		e.Code = "not_found"
		e.Message = "not found (this server may not have this route yet)"
	}
	return e
}
