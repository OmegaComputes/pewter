package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/OmegaComputes/pewter/internal/api"
	"github.com/OmegaComputes/pewter/internal/config"
	"github.com/OmegaComputes/pewter/internal/keystore"
	"github.com/OmegaComputes/pewter/internal/render"
)

type deviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type deviceToken struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

func (a *app) cmdLogin(args []string) int {
	withToken := false
	for _, x := range args {
		switch x {
		case "--with-token":
			withToken = true
		default:
			return a.fail(ExitUsage, "usage_error", "unknown option "+render.Clean(x, 40)+" (try `pewter help login`)")
		}
	}
	var token string
	var expires *time.Time
	if withToken {
		t, err := a.readTokenStdin()
		if err != nil {
			return a.fail(ExitUsage, "usage_error", err.Error())
		}
		token = t
	} else {
		t, exp, code, err := a.deviceFlow()
		if code != 0 {
			return code
		}
		if err != nil {
			return a.failErr(err)
		}
		token, expires = t, exp
	}
	// Prove the token works before keeping it.
	c, err := a.client(false)
	if err != nil {
		return a.failErr(err)
	}
	c.Token = token
	var me map[string]any
	if err := c.Do(a.Ctx, http.MethodGet, "/me", nil, nil, true, &me); err != nil {
		return a.failErr(err)
	}
	if err := a.store.Set(a.profile, token); err != nil {
		return a.fail(ExitError, "store_error", err.Error())
	}
	if a.storeWarn != "" {
		fmt.Fprintf(a.Stderr, "pewter: note: %s\n", render.Clean(a.storeWarn, 200))
	}
	now := time.Now().UTC()
	a.cfg.APIURL, a.cfg.Profile, a.cfg.SignedAt, a.cfg.ExpiresAt = a.apiURL, a.profile, &now, expires
	if err := config.Save(a.dir, a.cfg); err != nil {
		return a.fail(ExitError, "config_error", err.Error())
	}
	name := firstNonEmpty(str(m(me["principal"])["display_name"]), "your account")
	if a.g.json {
		out := map[string]any{"signed_in": true, "api_url": a.apiURL, "profile": a.profile, "stored_in": a.store.Name()}
		if expires != nil {
			out["expires_at"] = expires.Format(time.RFC3339)
		}
		_ = render.JSON(a.Stdout, out)
		return ExitOK
	}
	fmt.Fprintf(a.Stdout, "Signed in as %s. Token kept in %s.\n", render.Clean(name, 80), a.store.Name())
	return ExitOK
}

// readTokenStdin reads one token from stdin (never from argv). The server
// decides whether the token is valid; the CLI only checks that it is well formed.
func (a *app) readTokenStdin() (string, error) {
	r := bufio.NewReader(io.LimitReader(a.Stdin, 1024))
	line, err := r.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", fmt.Errorf("reading token from stdin: %w", err)
	}
	t := strings.TrimSpace(line)
	switch {
	case t == "":
		return "", fmt.Errorf("no token on stdin (example: pewter login --with-token < token.txt)")
	case !keystore.ValidToken(t):
		return "", fmt.Errorf("token has an unexpected shape")
	}
	return t, nil
}

// deviceFlow runs an RFC 8628 style device-code login against /auth/device/*.
// If the server does not offer these routes, it stops with a clear message
// and points to `pewter login --with-token`.
func (a *app) deviceFlow() (token string, exp *time.Time, exit int, err error) {
	c, err := a.client(false)
	if err != nil {
		return "", nil, 0, err
	}
	var dc deviceCode
	err = c.Do(a.Ctx, http.MethodPost, "/auth/device/code", nil, map[string]string{"client_id": "pewter-cli"}, false, &dc)
	if err != nil {
		if ae, ok := err.(*api.Error); ok && ae.Status == 404 {
			return "", nil, a.fail(ExitError, "device_login_unavailable",
				"this server does not offer browser sign-in. Create an API key in your account and run: pewter login --with-token < key.txt"), nil
		}
		return "", nil, 0, err
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" {
		return "", nil, a.fail(ExitError, "bad_response", "server sent an incomplete device code"), nil
	}
	if err := a.checkVerificationURL(dc.VerificationURI); err != nil {
		return "", nil, a.fail(ExitError, "bad_response", err.Error()), nil
	}
	fmt.Fprintf(a.Stderr, "\nTo sign in, open:\n  %s\nand enter this code:\n  %s\n\nWaiting for approval (Ctrl-C to cancel)...\n", render.Clean(dc.VerificationURI, 200), render.Clean(dc.UserCode, 20))

	interval := dc.Interval
	if interval < 1 {
		interval = 5
	}
	expiresIn := dc.ExpiresIn
	if expiresIn < 1 || expiresIn > 3600 {
		expiresIn = 600
	}
	waited := 0
	for waited <= expiresIn {
		if a.Ctx.Err() != nil {
			return "", nil, a.fail(ExitError, "cancelled", "login cancelled"), nil
		}
		var tok deviceToken
		err := c.Do(a.Ctx, http.MethodPost, "/auth/device/token", nil, map[string]string{
			"client_id": "pewter-cli", "device_code": dc.DeviceCode, "grant_type": "urn:ietf:params:oauth:grant-type:device_code",
		}, false, &tok)
		if err == nil {
			if !keystore.ValidToken(tok.AccessToken) {
				return "", nil, a.fail(ExitError, "bad_response", "server sent a token in an unexpected form"), nil
			}
			var e *time.Time
			if tok.ExpiresIn > 0 {
				t := time.Now().UTC().Add(time.Duration(tok.ExpiresIn) * time.Second)
				e = &t
			}
			return tok.AccessToken, e, 0, nil
		}
		ae, ok := err.(*api.Error)
		if !ok {
			return "", nil, 0, err
		}
		switch ae.Code {
		case "authorization_pending":
		case "slow_down":
			interval += 5
		case "expired_token":
			return "", nil, a.fail(ExitError, "expired_token", "the code expired. Run `pewter login` again"), nil
		case "access_denied":
			return "", nil, a.fail(ExitError, "access_denied", "sign-in was declined in the app"), nil
		default:
			return "", nil, 0, err
		}
		a.Sleep(time.Duration(interval) * time.Second)
		waited += interval
	}
	return "", nil, a.fail(ExitError, "expired_token", "timed out waiting for approval. Run `pewter login` again"), nil
}

// checkVerificationURL only lets the user be sent to the API's own host or an
// omegacompute.com host, so a bad server response cannot point them elsewhere.
func (a *app) checkVerificationURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("server sent an invalid verification URL")
	}
	base, _ := url.Parse(a.apiURL)
	h := u.Hostname()
	okHost := h == base.Hostname() || h == "omegacompute.com" || strings.HasSuffix(h, ".omegacompute.com")
	if !okHost {
		return fmt.Errorf("verification URL host %q is not the API host or omegacompute.com; refusing", render.Clean(h, 80))
	}
	if _, err := api.ValidateBase(u.Scheme + "://" + u.Host); err != nil {
		return err
	}
	return nil
}

func (a *app) cmdLogout() int {
	if err := a.store.Delete(a.profile); err != nil {
		return a.fail(ExitError, "store_error", err.Error())
	}
	a.cfg.SignedAt, a.cfg.ExpiresAt = nil, nil
	if err := config.Save(a.dir, a.cfg); err != nil {
		return a.fail(ExitError, "config_error", err.Error())
	}
	if a.g.json {
		_ = render.JSON(a.Stdout, map[string]any{"signed_in": false, "profile": a.profile, "revoked_on_server": false})
		return ExitOK
	}
	fmt.Fprintln(a.Stdout, "Signed out on this machine. (Prototype: the token is not revoked on the server yet; revoke it in the Pewter app.)")
	return ExitOK
}

var _ = context.Background
