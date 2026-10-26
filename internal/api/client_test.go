package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestValidateBase(t *testing.T) {
	ok := []string{"https://a.example.com/api/v1", "http://localhost:3000/api/v1", "http://127.0.0.1:8799/api/v1", "http://[::1]:80/api/v1"}
	bad := []string{"http://a.example.com/api/v1", "http://127.0.0.1.evil.com/x", "file:///etc/passwd", "https://u:p@a.com/", "", "a.com/api"}
	for _, u := range ok {
		if _, err := ValidateBase(u); err != nil {
			t.Errorf("%s rejected: %v", u, err)
		}
	}
	for _, u := range bad {
		if _, err := ValidateBase(u); err == nil {
			t.Errorf("%s accepted", u)
		}
	}
}

func TestRedirectNotFollowedAndTokenNotLeaked(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			leaked.Store(true)
		}
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()
	c, err := New(srv.URL+"/api/v1", "tok_secretsecretsecret1", "t")
	if err != nil {
		t.Fatal(err)
	}
	err = c.Do(context.Background(), "GET", "/me", nil, nil, true, new(any))
	if err == nil {
		t.Fatal("redirect should be an error")
	}
	if leaked.Load() {
		t.Fatal("bearer token was sent to the redirect target")
	}
	if strings.Contains(err.Error(), "secretsecret") {
		t.Fatal("token in error message")
	}
}

func TestErrorEnvelopeAndNoEnvelopeHealth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/health" {
			w.Write([]byte(`{"status":"ok"}`))
			return
		}
		w.Header().Set("X-Request-Id", "rid1")
		w.WriteHeader(403)
		w.Write([]byte(`{"error":{"code":"forbidden","message":"nope"}}`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL+"/api/v1", "", "t")
	var h map[string]string
	if err := c.Do(context.Background(), "GET", "/health", nil, nil, false, &h); err != nil || h["status"] != "ok" {
		t.Fatalf("health: %v %v", err, h)
	}
	err := c.Do(context.Background(), "GET", "/x", nil, nil, false, nil)
	ae, ok := err.(*Error)
	if !ok || ae.Code != "forbidden" || ae.RequestID != "rid1" || ae.Status != 403 {
		t.Fatalf("err=%#v", err)
	}
}

func TestOversizeResponseRefused(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(strings.Repeat("x", 5<<20)))
	}))
	defer srv.Close()
	c, _ := New(srv.URL+"/api/v1", "", "t")
	if err := c.Do(context.Background(), "GET", "/x", nil, nil, false, new(any)); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("err=%v", err)
	}
}

func TestPostGetsIdempotencyKey(t *testing.T) {
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("Idempotency-Key")
		w.Write([]byte(`{"data":{}}`))
	}))
	defer srv.Close()
	c, _ := New(srv.URL+"/api/v1", "", "t")
	_ = c.Do(context.Background(), "POST", "/x", nil, map[string]string{"a": "b"}, false, new(any))
	if len(key) < 16 {
		t.Fatalf("key=%q", key)
	}
}
