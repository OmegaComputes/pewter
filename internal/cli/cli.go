// Package cli implements the `pewter` command. It is read-only by design in
// this prototype: the only writes are the device-code login handshake and the
// local token store.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"time"

	"github.com/OmegaComputes/pewter/internal/api"
	"github.com/OmegaComputes/pewter/internal/config"
	"github.com/OmegaComputes/pewter/internal/keystore"
	"github.com/OmegaComputes/pewter/internal/render"
	"github.com/OmegaComputes/pewter/internal/version"
)

// Exit codes. Stable, so scripts can branch on them.
const (
	ExitOK      = 0
	ExitError   = 1
	ExitUsage   = 2
	ExitAuth    = 3
	ExitNetwork = 4
)

// Env is everything the CLI touches outside itself, so tests can replace it.
type Env struct {
	Stdin    io.Reader
	Stdout   io.Writer
	Stderr   io.Writer
	Getenv   func(string) string
	Sleep    func(time.Duration)
	Keystore func(mode, dir string) (keystore.Store, string)
	HTTP     *http.Client // optional override (tests)
	Ctx      context.Context
}

// DefaultEnv wires the real process.
func DefaultEnv() Env {
	ctx, _ := signal.NotifyContext(context.Background(), os.Interrupt)
	return Env{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Getenv: os.Getenv, Sleep: time.Sleep, Keystore: keystore.Default, Ctx: ctx,
	}
}

type globals struct {
	json    bool
	apiURL  string
	profile string
	help    bool
}

type app struct {
	Env
	g       globals
	dir     string
	cfg     config.File
	store   keystore.Store
	profile string
	apiURL  string
	// storeWarn is shown once by `login` when the token lands in a file.
	storeWarn string
}

// Run executes one command line (without argv[0]) and returns the exit code.
func Run(args []string, env Env) int {
	if env.Ctx == nil {
		env.Ctx = context.Background()
	}
	if env.Sleep == nil {
		env.Sleep = time.Sleep
	}
	a := &app{Env: env}
	rest, err := a.parseGlobals(args)
	if err != nil {
		return a.fail(ExitUsage, "usage_error", err.Error())
	}
	if len(rest) == 0 {
		a.printHelp(a.Stdout)
		if a.g.help {
			return ExitOK
		}
		return ExitUsage
	}
	cmd, cargs := rest[0], rest[1:]
	if a.g.help || cmd == "help" {
		if cmd == "help" && len(cargs) > 0 {
			cmd = cargs[0]
		}
		return a.help(cmd)
	}
	switch cmd {
	case "version", "--version", "-v":
		return a.cmdVersion()
	case "completion":
		return a.cmdCompletion(cargs)
	}
	if err := a.init(); err != nil {
		return a.fail(ExitError, "config_error", err.Error())
	}
	switch cmd {
	case "login":
		return a.cmdLogin(cargs)
	case "logout":
		return a.cmdLogout()
	case "status":
		return a.cmdStatus()
	case "whoami", "me":
		return a.cmdWhoami()
	case "instruments":
		return a.cmdInstruments(cargs)
	case "listings":
		return a.cmdListings(cargs)
	case "orders":
		return a.cmdOrders(cargs)
	case "balance":
		return a.cmdBalance()
	case "verify":
		return a.cmdVerify(cargs)
	case "buy", "ask", "sell", "rfq", "accept", "approve", "send":
		return a.cmdUnavailable(cmd)
	}
	return a.fail(ExitUsage, "unknown_command", fmt.Sprintf("unknown command %q. Run `pewter help`.", cmd))
}

func (a *app) parseGlobals(args []string) ([]string, error) {
	var rest []string
	for i := 0; i < len(args); i++ {
		x := args[i]
		if x == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		val := func(name string) (string, error) {
			if strings.HasPrefix(x, name+"=") {
				return strings.TrimPrefix(x, name+"="), nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		switch {
		case x == "--json":
			a.g.json = true
		case x == "-h" || x == "--help":
			a.g.help = true
		case x == "--no-color":
		case x == "--api-url" || strings.HasPrefix(x, "--api-url="):
			v, err := val("--api-url")
			if err != nil {
				return nil, err
			}
			a.g.apiURL = v
		case x == "--profile" || strings.HasPrefix(x, "--profile="):
			v, err := val("--profile")
			if err != nil {
				return nil, err
			}
			a.g.profile = v
		default:
			rest = append(rest, x)
		}
	}
	return rest, nil
}

func (a *app) init() error {
	dir, err := config.Dir(a.Getenv)
	if err != nil {
		return err
	}
	a.dir = dir
	if a.cfg, err = config.Load(dir); err != nil {
		return fmt.Errorf("reading config: %w", err)
	}
	a.profile = firstNonEmpty(a.g.profile, a.Getenv("PEWTER_PROFILE"), a.cfg.Profile, "default")
	if !keystore.ValidProfile(a.profile) {
		return fmt.Errorf("invalid profile name %q", a.profile)
	}
	a.apiURL = firstNonEmpty(a.g.apiURL, a.Getenv("PEWTER_API_URL"), a.cfg.APIURL, config.DefaultAPI)
	if _, err := api.ValidateBase(a.apiURL); err != nil {
		return err
	}
	ks := a.Keystore
	if ks == nil {
		ks = keystore.Default
	}
	a.store, a.storeWarn = ks(firstNonEmpty(a.Getenv("PEWTER_KEYSTORE"), "auto"), dir)
	return nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// client builds an API client. needAuth resolves a token (env wins over the store).
func (a *app) client(needAuth bool) (*api.Client, error) {
	token := ""
	if needAuth {
		if t := a.Getenv("PEWTER_TOKEN"); t != "" {
			if !keystore.ValidToken(t) {
				return nil, &api.Error{Status: 401, Code: "unauthorized", Message: "PEWTER_TOKEN has an unexpected shape"}
			}
			token = t
		} else {
			t, err := a.store.Get(a.profile)
			if err != nil {
				return nil, &api.Error{Status: 401, Code: "unauthorized", Message: "not signed in (run `pewter login`)"}
			}
			token = t
		}
	}
	c, err := api.New(a.apiURL, token, fmt.Sprintf("pewter-cli/%s (%s/%s)", version.Version, runtime.GOOS, runtime.GOARCH))
	if err != nil {
		return nil, err
	}
	if a.HTTP != nil {
		c.HTTP = a.HTTP
	}
	return c, nil
}

// fail prints an error (JSON on stderr with --json) and returns code.
func (a *app) fail(code int, errCode, msg string) int {
	return a.failReq(code, errCode, msg, "")
}

func (a *app) failReq(code int, errCode, msg, reqID string) int {
	msg = render.Clean(msg, 400)
	if a.g.json {
		e := map[string]any{"code": errCode, "message": msg}
		if reqID != "" {
			e["request_id"] = reqID
		}
		_ = render.JSON(a.Stderr, map[string]any{"error": e})
		return code
	}
	if reqID != "" {
		fmt.Fprintf(a.Stderr, "pewter: %s (request %s)\n", msg, render.Clean(reqID, 64))
	} else {
		fmt.Fprintf(a.Stderr, "pewter: %s\n", msg)
	}
	return code
}

// failErr maps an error from the client to an exit code.
func (a *app) failErr(err error) int {
	var ae *api.Error
	if errors.As(err, &ae) {
		code := ExitError
		if ae.Status == 401 {
			code = ExitAuth
		}
		msg := ae.Message
		if ae.Status == 401 && ae.Code == "unauthorized" {
			msg += " — run `pewter login`"
			if strings.Contains(ae.Message, "run `pewter login`") {
				msg = ae.Message
			}
		}
		return a.failReq(code, firstNonEmpty(ae.Code, "api_error"), msg, ae.RequestID)
	}
	if strings.HasPrefix(err.Error(), "network error") {
		return a.fail(ExitNetwork, "network_error", err.Error())
	}
	return a.fail(ExitError, "error", err.Error())
}

// get fetches path into raw JSON.
func (a *app) get(path string, q url.Values, auth bool) (json.RawMessage, error) {
	c, err := a.client(auth)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := c.Do(a.Ctx, http.MethodGet, path, q, nil, auth, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// emit prints raw JSON (--json) or calls human().
func (a *app) emit(raw json.RawMessage, human func() error) int {
	if a.g.json {
		var v any
		if err := json.Unmarshal(raw, &v); err != nil {
			return a.fail(ExitError, "bad_response", "server sent invalid JSON")
		}
		if err := render.JSON(a.Stdout, v); err != nil {
			return a.fail(ExitError, "error", err.Error())
		}
		return ExitOK
	}
	if err := human(); err != nil {
		return a.fail(ExitError, "bad_response", err.Error())
	}
	return ExitOK
}

func (a *app) cmdVersion() int {
	info := map[string]string{"version": version.Version, "commit": version.Commit, "date": version.Date, "go": runtime.Version(), "os": runtime.GOOS, "arch": runtime.GOARCH}
	if a.g.json {
		_ = render.JSON(a.Stdout, info)
		return ExitOK
	}
	fmt.Fprintf(a.Stdout, "pewter %s (%s, %s/%s)\n", version.Version, version.Commit, runtime.GOOS, runtime.GOARCH)
	return ExitOK
}

func (a *app) cmdUnavailable(name string) int {
	msg := fmt.Sprintf("`pewter %s` is unavailable in this release.", name)
	return a.fail(ExitUsage, "not_available", msg)
}
