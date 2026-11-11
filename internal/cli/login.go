package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/OmegaComputes/pewter/internal/api"
	"github.com/OmegaComputes/pewter/internal/config"
	"github.com/OmegaComputes/pewter/internal/keystore"
	"github.com/OmegaComputes/pewter/internal/render"
)

const deviceClientID = "pewter-cli"

func readOnlyScopes() []string {
	return []string{"deals:read", "market:read", "verification:read"}
}

type deviceCode struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type deviceToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	CredentialID string `json:"credential_id"`
}

type deviceCodeBody struct {
	ClientID   string   `json:"client_id"`
	ClientName string   `json:"client_name"`
	Scopes     []string `json:"scopes"`
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
	if !strings.HasPrefix(token, "pck_") || !keystore.ValidToken(token) {
		return a.fail(ExitError, "bad_response", "refusing to store a token that is not a Pewter CLI key (pck_)")
	}
	// Prove the new key with GET /me before revoking or replacing the stored one.
	c, err := a.client(false)
	if err != nil {
		return a.failErr(err)
	}
	c.Token = token
	var me map[string]any
	if err := c.Do(a.Ctx, http.MethodGet, "/me", nil, nil, true, &me); err != nil {
		// A device login already minted this credential. Drop that new key so
		// a failed login does not count toward the account's active-key cap.
		// The key already stored for this profile stays in place.
		if !withToken {
			stored, serr := a.store.Get(a.profile)
			if serr != nil || stored != token {
				_ = a.postRevoke(token)
			}
		}
		return a.failErr(err)
	}
	// The new key works. Revoke a different stored key now, then store this one.
	a.revokePrevious(token)
	if exp := credentialExpiry(me); exp != nil {
		expires = exp
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

// readTokenStdin reads one token from stdin (never from argv).
// The token must be a pck_ key. It is not stored until GET /me succeeds.
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
	case !strings.HasPrefix(t, "pck_"):
		return "", errors.New(pckError(t))
	}
	return t, nil
}

// cleanClientName strips control characters and keeps at most 60 runes.
// An empty result becomes "pewter-cli".
func cleanClientName(raw string) string {
	var b strings.Builder
	for _, r := range raw {
		if r == utf8.RuneError || unicode.IsControl(r) || r == '\u2028' || r == '\u2029' {
			continue
		}
		b.WriteRune(r)
	}
	s := strings.TrimSpace(b.String())
	rs := []rune(s)
	if len(rs) > 60 {
		s = strings.TrimSpace(string(rs[:60]))
	}
	if s == "" {
		return "pewter-cli"
	}
	return s
}

func clientName() string {
	h, err := os.Hostname()
	if err != nil {
		return cleanClientName("")
	}
	return cleanClientName(h)
}

// deviceFlow runs RFC 8628 device login against /auth/device/*.
// The access token is returned to the caller and is never printed.
func (a *app) deviceFlow() (token string, exp *time.Time, exit int, err error) {
	c, err := a.client(false)
	if err != nil {
		return "", nil, 0, err
	}
	var dc deviceCode
	body := deviceCodeBody{ClientID: deviceClientID, ClientName: clientName(), Scopes: readOnlyScopes()}
	err = c.Do(a.Ctx, http.MethodPost, "/auth/device/code", nil, body, false, &dc)
	if err != nil {
		if ae, ok := err.(*api.Error); ok && ae.Status == 404 {
			return "", nil, a.fail(ExitError, "device_login_unavailable",
				"this server does not offer browser sign-in. A Pewter account approval in the app is required. If you already have a pck_ key, run: pewter login --with-token < key.txt"), nil
		}
		return "", nil, 0, err
	}
	if dc.DeviceCode == "" || dc.UserCode == "" || dc.VerificationURI == "" {
		return "", nil, a.fail(ExitError, "bad_response", "server sent an incomplete device code"), nil
	}
	if err := a.checkVerificationURL(dc.VerificationURI); err != nil {
		return "", nil, a.fail(ExitError, "bad_response", err.Error()), nil
	}
	fmt.Fprintf(a.Stderr, "\nTo sign in, open:\n  %s\nand enter this code:\n  %s\n\nApprove it in the Pewter app.\nWaiting for approval (Ctrl-C to cancel)...\n", render.Clean(dc.VerificationURI, 200), render.Clean(dc.UserCode, 20))

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
			"client_id": deviceClientID, "device_code": dc.DeviceCode, "grant_type": "urn:ietf:params:oauth:grant-type:device_code",
		}, false, &tok)
		if err == nil {
			if !keystore.ValidToken(tok.AccessToken) || !strings.HasPrefix(tok.AccessToken, "pck_") {
				return "", nil, a.fail(ExitError, "bad_response", "server sent a token that is not a Pewter CLI key (pck_)"), nil
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

func credentialExpiry(me map[string]any) *time.Time {
	return parseExpiry(str(m(me["credential"])["expires_at"]))
}

func parseExpiry(s string) *time.Time {
	if s == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			u := t.UTC()
			return &u
		}
	}
	return nil
}

// revokePrevious revokes the credential already stored for this profile when
// it differs from next. A 401 (already revoked or expired) and any other
// revoke error are ignored so a dead key cannot block the new login.
// The env token is not used here. Revoking next itself would destroy the key
// this login is about to store.
func (a *app) revokePrevious(next string) {
	tok, err := a.store.Get(a.profile)
	if err != nil || tok == "" || tok == next {
		return
	}
	if !keystore.ValidToken(tok) || !strings.HasPrefix(tok, "pck_") {
		return
	}
	_ = a.postRevoke(tok)
}

func (a *app) postRevoke(token string) error {
	c, err := api.New(a.apiURL, token, a.userAgent())
	if err != nil {
		return err
	}
	if a.HTTP != nil {
		c.HTTP = a.HTTP
	}
	return c.Do(a.Ctx, http.MethodPost, "/auth/credentials/self/revoke", nil, map[string]any{}, true, nil)
}

func (a *app) cmdLogout() int {
	envTok := a.Getenv("PEWTER_TOKEN")
	fromEnv := envTok != ""
	stored, storeErr := a.store.Get(a.profile)
	hasStored := storeErr == nil && stored != ""

	var token string
	if fromEnv {
		if !keystore.ValidToken(envTok) {
			return a.fail(ExitAuth, "unauthorized", "PEWTER_TOKEN has an unexpected shape")
		}
		if !strings.HasPrefix(envTok, "pck_") {
			return a.fail(ExitAuth, "unauthorized", pckError(envTok))
		}
		token = envTok
	} else if storeErr != nil && !errors.Is(storeErr, keystore.ErrNotFound) {
		return a.fail(ExitError, "store_error", storeErr.Error())
	} else if !hasStored {
		a.cfg.SignedAt, a.cfg.ExpiresAt = nil, nil
		if err := config.Save(a.dir, a.cfg); err != nil {
			return a.fail(ExitError, "config_error", err.Error())
		}
		return a.logoutDone(false, false, false)
	} else {
		if !keystore.ValidToken(stored) || !strings.HasPrefix(stored, "pck_") {
			if err := a.store.Delete(a.profile); err != nil {
				return a.fail(ExitError, "store_error", err.Error())
			}
			a.cfg.SignedAt, a.cfg.ExpiresAt = nil, nil
			_ = config.Save(a.dir, a.cfg)
			return a.fail(ExitAuth, "unauthorized", "stored token is not a Pewter CLI key (pck_) and was removed from this machine")
		}
		token = stored
	}

	already := false
	if err := a.postRevoke(token); err != nil {
		var ae *api.Error
		if errors.As(err, &ae) && ae.Status == 401 {
			already = true
		} else {
			return a.failErr(err)
		}
	}
	if !fromEnv {
		if err := a.store.Delete(a.profile); err != nil {
			return a.fail(ExitError, "store_error", err.Error())
		}
		a.cfg.SignedAt, a.cfg.ExpiresAt = nil, nil
		if err := config.Save(a.dir, a.cfg); err != nil {
			return a.fail(ExitError, "config_error", err.Error())
		}
	}
	return a.logoutDone(fromEnv, already, hasStored)
}

func (a *app) logoutDone(fromEnv, already, hasStored bool) int {
	if a.g.json {
		_ = render.JSON(a.Stdout, map[string]any{
			"signed_in":         false,
			"profile":           a.profile,
			"revoked_on_server": !already && (fromEnv || hasStored),
			"already_revoked":   already,
			"from_env":          fromEnv,
		})
		return ExitOK
	}
	switch {
	case fromEnv && already && hasStored:
		fmt.Fprintln(a.Stdout, "The credential for PEWTER_TOKEN was already revoked or expired. Unset PEWTER_TOKEN. The token stored on this machine was left in place.")
	case fromEnv && already:
		fmt.Fprintln(a.Stdout, "The credential for PEWTER_TOKEN was already revoked or expired. Unset PEWTER_TOKEN; there is nothing on disk to delete.")
	case fromEnv && hasStored:
		fmt.Fprintln(a.Stdout, "Revoked the credential for PEWTER_TOKEN on the server. Unset PEWTER_TOKEN. The token stored on this machine was left in place.")
	case fromEnv:
		fmt.Fprintln(a.Stdout, "Revoked the credential for PEWTER_TOKEN on the server. Unset PEWTER_TOKEN; there is nothing on disk to delete.")
	case already:
		fmt.Fprintln(a.Stdout, "Signed out on this machine. The credential was already revoked or expired.")
	case !hasStored:
		fmt.Fprintln(a.Stdout, "Not signed in on this machine.")
	default:
		fmt.Fprintln(a.Stdout, "Signed out. The credential was revoked on the server and removed from this machine.")
	}
	return ExitOK
}
