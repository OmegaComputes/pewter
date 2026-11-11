package cli

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/OmegaComputes/pewter/internal/render"
)

func str(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(t)
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func m(v any) map[string]any {
	if x, ok := v.(map[string]any); ok {
		return x
	}
	return map[string]any{}
}

func usd(v any) string {
	f, ok := v.(float64)
	if !ok {
		return ""
	}
	return fmt.Sprintf("%.2f", f)
}

func apiHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" {
		return raw
	}
	return u.Hostname()
}

func (a *app) cmdStatus() int {
	raw, err := a.get("/health", nil, false)
	if err != nil {
		return a.failErr(err)
	}
	var h map[string]any
	if err := json.Unmarshal(raw, &h); err != nil {
		return a.fail(ExitError, "bad_response", "server sent invalid JSON")
	}
	host := apiHost(a.apiURL)
	h["host"] = host
	return a.finish(h, func() error {
		fmt.Fprintf(a.Stdout, "host     %s\nAPI      %s\nstatus   %s\nversion  %s\ntime     %s\n", render.Clean(host, 120), render.Clean(a.apiURL, 200), render.Clean(str(h["status"]), 20), render.Clean(str(h["api_version"]), 20), render.Clean(str(h["time"]), 40))
		return nil
	})
}

type whoamiOut struct {
	DisplayName  string   `json:"display_name"`
	CredentialID string   `json:"credential_id"`
	Scopes       []string `json:"scopes"`
	ExpiresAt    string   `json:"expires_at"`
	Sandbox      bool     `json:"sandbox"`
	APIHost      string   `json:"api_host"`
}

func whoamiView(d map[string]any, host string) whoamiOut {
	pr := m(d["principal"])
	cr := m(d["credential"])
	var scopes []string
	if raw, ok := cr["scopes"].([]any); ok {
		for _, s := range raw {
			if t := render.Clean(str(s), 40); t != "" {
				scopes = append(scopes, t)
			}
		}
	}
	if scopes == nil {
		scopes = []string{}
	}
	sandbox := false
	if v, ok := cr["sandbox_only"].(bool); ok {
		sandbox = v
	} else if d["sandbox_only"] == true {
		sandbox = true
	}
	return whoamiOut{
		DisplayName:  render.Clean(str(pr["display_name"]), 80),
		CredentialID: render.Clean(str(cr["id"]), 80),
		Scopes:       scopes,
		ExpiresAt:    render.Clean(str(cr["expires_at"]), 40),
		Sandbox:      sandbox,
		APIHost:      host,
	}
}

func (a *app) cmdWhoami() int {
	raw, err := a.get("/me", nil, true)
	if err != nil {
		return a.failErr(err)
	}
	var d map[string]any
	if err := json.Unmarshal(raw, &d); err != nil {
		return a.fail(ExitError, "bad_response", "server sent invalid JSON")
	}
	view := whoamiView(d, apiHost(a.apiURL))
	return a.finish(view, func() error {
		exp := view.ExpiresAt
		if exp == "" {
			exp = "-"
		}
		scopes := strings.Join(view.Scopes, ", ")
		if scopes == "" {
			scopes = "-"
		}
		fmt.Fprintf(a.Stdout, "name        %s\ncredential  %s\nscopes      %s\nexpires     %s\nsandbox     %t\nhost        %s\n", view.DisplayName, view.CredentialID, scopes, exp, view.Sandbox, render.Clean(view.APIHost, 120))
		return nil
	})
}

func (a *app) cmdInstruments(args []string) int {
	q := url.Values{}
	limit := 0
	for i := 0; i < len(args); i++ {
		k := args[i]
		take := func() (string, bool) {
			if strings.Contains(k, "=") {
				return k[strings.Index(k, "=")+1:], true
			}
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}
		switch {
		case strings.HasPrefix(k, "--gpu"):
			v, ok := take()
			if !ok {
				return a.fail(ExitUsage, "usage_error", "--gpu needs a value")
			}
			q.Set("gpu", v)
		case strings.HasPrefix(k, "--term"):
			v, ok := take()
			if !ok {
				return a.fail(ExitUsage, "usage_error", "--term needs a value")
			}
			q.Set("term", v)
		case strings.HasPrefix(k, "--region"):
			v, ok := take()
			if !ok {
				return a.fail(ExitUsage, "usage_error", "--region needs a value")
			}
			q.Set("region", v)
		case strings.HasPrefix(k, "--limit"):
			v, ok := take()
			n, err := strconv.Atoi(v)
			if !ok || err != nil || n < 1 {
				return a.fail(ExitUsage, "usage_error", "--limit needs a positive number")
			}
			limit = n
		default:
			return a.fail(ExitUsage, "usage_error", "unknown option "+render.Clean(k, 40))
		}
	}
	raw, err := a.get("/instruments", q, false)
	if err != nil {
		return a.failErr(err)
	}
	return a.emit(raw, func() error {
		var rows []map[string]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			return err
		}
		if limit > 0 && len(rows) > limit {
			rows = rows[:limit]
		}
		var out [][]string
		for _, r := range rows {
			out = append(out, []string{str(r["id"]), str(r["kind"]), str(r["canonical"]), str(r["term"]), str(r["region"]), str(r["suggested_block_gpus"])})
		}
		render.Table(a.Stdout, []string{"INSTRUMENT", "KIND", "MODEL", "TERM", "REGION", "BLOCK"}, out)
		return nil
	})
}

func (a *app) cmdListings(args []string) int {
	status := ""
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--status") {
			if strings.Contains(args[i], "=") {
				status = args[i][strings.Index(args[i], "=")+1:]
			} else if i+1 < len(args) {
				i++
				status = args[i]
			}
		} else {
			return a.fail(ExitUsage, "usage_error", "unknown option "+render.Clean(args[i], 40))
		}
	}
	raw, err := a.get("/listings", nil, true)
	if err != nil {
		return a.failErr(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return a.fail(ExitError, "bad_response", "server sent invalid JSON")
	}
	views := make([]listingOut, 0, len(rows))
	var table [][]string
	for _, r := range rows {
		if status != "" && str(r["status"]) != status {
			continue
		}
		v := listingView(r)
		views = append(views, v)
		table = append(table, []string{v.ID, v.Status, v.Title, v.GPUs, usd(v.USDPerGPUHr), v.Tier, v.Validation})
	}
	return a.finish(views, func() error {
		render.Table(a.Stdout, []string{"ID", "STATUS", "TITLE", "GPUS", "USD/GPU-HR", "TIER", "VALIDATION"}, table)
		return nil
	})
}

type listingOut struct {
	ID          string `json:"id"`
	Status      string `json:"status"`
	Title       string `json:"title"`
	GPUs        string `json:"gpus"`
	USDPerGPUHr any    `json:"usd_per_gpu_hr"`
	Tier        string `json:"tier"`
	Validation  string `json:"validation"`
}

func listingView(r map[string]any) listingOut {
	gpu, price, as := m(m(r["capability"])["gpu"]), m(m(r["capability"])["price"]), m(r["assurance"])
	count := 0.0
	if c, ok := gpu["count_per_node"].(float64); ok {
		count = c
		if n, ok := gpu["nodes"].(float64); ok {
			count = c * n
		}
	}
	model := str(gpu["model"])
	if count > 0 {
		model = fmt.Sprintf("%s x%d", model, int(count))
	}
	var priceOut any
	if _, ok := price["amount_usd"].(float64); ok {
		priceOut = price["amount_usd"]
	}
	return listingOut{
		ID: render.Clean(str(r["id"]), 80), Status: render.Clean(str(r["status"]), 40), Title: render.Clean(str(r["title"]), 120),
		GPUs: render.Clean(model, 40), USDPerGPUHr: priceOut, Tier: render.Clean(str(as["tier"]), 20), Validation: render.Clean(str(as["validation"]), 40),
	}
}

func (a *app) cmdOrders(args []string) int {
	q := url.Values{}
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "--status") {
			v := ""
			if strings.Contains(args[i], "=") {
				v = args[i][strings.Index(args[i], "=")+1:]
			} else if i+1 < len(args) {
				i++
				v = args[i]
			}
			if v == "" {
				return a.fail(ExitUsage, "usage_error", "--status needs a value")
			}
			q.Set("status", v)
		} else {
			return a.fail(ExitUsage, "usage_error", "unknown option "+render.Clean(args[i], 40))
		}
	}
	raw, err := a.get("/orders", q, true)
	if err != nil {
		return a.failErr(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(raw, &rows); err != nil {
		return a.fail(ExitError, "bad_response", "server sent invalid JSON")
	}
	views := make([]orderOut, 0, len(rows))
	var table [][]string
	for _, r := range rows {
		v := orderView(r)
		views = append(views, v)
		table = append(table, []string{v.ID, v.Side, v.Status, v.InstrumentID, usd(v.USDPerGPUHr), str(v.GPUs), str(v.Hours), v.Book})
	}
	return a.finish(views, func() error {
		render.Table(a.Stdout, []string{"ORDER", "SIDE", "STATUS", "INSTRUMENT", "USD/GPU-HR", "GPUS", "HOURS", "BOOK"}, table)
		return nil
	})
}

type orderOut struct {
	ID           string `json:"id"`
	Side         string `json:"side"`
	Status       string `json:"status"`
	InstrumentID string `json:"instrument_id"`
	USDPerGPUHr  any    `json:"usd_per_gpu_hr"`
	GPUs         any    `json:"gpus"`
	Hours        any    `json:"hours"`
	Book         string `json:"book"`
}

func orderView(r map[string]any) orderOut {
	return orderOut{
		ID: render.Clean(str(r["id"]), 80), Side: render.Clean(str(r["side"]), 20), Status: render.Clean(str(r["status"]), 40),
		InstrumentID: render.Clean(str(r["instrument_id"]), 80), USDPerGPUHr: numOrNil(r["price_usd_per_unit"]),
		GPUs: numOrNil(r["gpus"]), Hours: numOrNil(r["hours"]), Book: render.Clean(str(r["book"]), 20),
	}
}

func numOrNil(v any) any {
	switch v.(type) {
	case float64, json.Number:
		return v
	default:
		return nil
	}
}

func (a *app) cmdBalance() int {
	raw, err := a.get("/wallet", nil, true)
	if err != nil {
		return a.failErr(err)
	}
	var w map[string]any
	if err := json.Unmarshal(raw, &w); err != nil {
		return a.fail(ExitError, "bad_response", "server sent invalid JSON")
	}
	view := balanceView(w)
	return a.finish(view, func() error {
		label := "Live balance"
		if view.Sandbox {
			label = "Sandbox balance (simulated funds)"
		}
		cur := firstNonEmpty(view.Currency, "USD")
		fmt.Fprintf(a.Stdout, "%s\n", label)
		fmt.Fprintf(a.Stdout, "  balance    %s %s\n  reserved   %s %s\n  available  %s %s\n", usd(view.BalanceUSD), cur, usd(view.ReservedUSD), cur, usd(view.AvailableUSD), cur)
		if view.Entries != nil {
			fmt.Fprintf(a.Stdout, "  entries    %d (use --json for the list)\n", len(view.Entries))
		}
		if view.Note != "" {
			fmt.Fprintf(a.Stdout, "\n%s\n", view.Note)
		}
		return nil
	})
}

type balanceOut struct {
	Sandbox      bool   `json:"sandbox"`
	Currency     string `json:"currency"`
	BalanceUSD   any    `json:"balance_usd"`
	ReservedUSD  any    `json:"reserved_usd"`
	AvailableUSD any    `json:"available_usd"`
	Entries      []any  `json:"entries,omitempty"`
	Note         string `json:"note,omitempty"`
}

func balanceView(w map[string]any) balanceOut {
	out := balanceOut{
		Sandbox: w["sandbox"] == true, Currency: render.Clean(str(w["currency"]), 12),
		BalanceUSD: numOrNil(w["balance_usd"]), ReservedUSD: numOrNil(w["reserved_usd"]), AvailableUSD: numOrNil(w["available_usd"]),
		Note: render.Clean(str(w["note"]), 300),
	}
	if es, ok := w["entries"].([]any); ok {
		out.Entries = make([]any, 0, len(es))
		for _, e := range es {
			out.Entries = append(out.Entries, dropIdentity(e))
		}
	}
	return out
}

// cmdVerify is read-only: it shows the latest validation runs of one of your listings.
func (a *app) cmdVerify(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return a.fail(ExitUsage, "usage_error", "usage: pewter verify <listing-id>   (shows validation status)")
	}
	id := args[0]
	if !validID(id) {
		return a.fail(ExitUsage, "usage_error", "invalid listing id")
	}
	raw, err := a.get("/listings/"+url.PathEscape(id)+"/verification", nil, true)
	if err != nil {
		return a.failErr(err)
	}
	var d struct {
		Listing map[string]any   `json:"listing"`
		Runs    []map[string]any `json:"runs"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		return a.fail(ExitError, "bad_response", "server sent invalid JSON")
	}
	view := verifyView(d.Listing, d.Runs)
	return a.finish(view, func() error {
		fmt.Fprintf(a.Stdout, "listing    %s (%s)\ntier       %s\nvalidation %s\n", view.ID, view.Status, view.Tier, view.Validation)
		if view.Simulated {
			fmt.Fprintln(a.Stdout, "note       simulated validation (sandbox)")
		}
		var table [][]string
		for _, r := range view.Runs {
			table = append(table, []string{r.ID, r.Kind, r.State, r.Tier, r.Finished})
		}
		if len(table) > 0 {
			fmt.Fprintln(a.Stdout)
			render.Table(a.Stdout, []string{"RUN", "KIND", "STATE", "TIER", "FINISHED"}, table)
		}
		return nil
	})
}

type verifyOut struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	Tier       string   `json:"tier"`
	Validation string   `json:"validation"`
	Simulated  bool     `json:"simulated"`
	Runs       []runOut `json:"runs"`
}

type runOut struct {
	ID       string `json:"id"`
	Kind     string `json:"kind"`
	State    string `json:"state"`
	Tier     string `json:"tier"`
	Finished string `json:"finished_at"`
}

func verifyView(listing map[string]any, runs []map[string]any) verifyOut {
	as := m(listing["assurance"])
	out := verifyOut{
		ID: render.Clean(str(listing["id"]), 40), Status: render.Clean(str(listing["status"]), 20),
		Tier: render.Clean(str(as["tier"]), 10), Validation: render.Clean(str(as["validation"]), 30),
		Simulated: as["simulated"] == true, Runs: []runOut{},
	}
	for _, r := range runs {
		out.Runs = append(out.Runs, runOut{
			ID: render.Clean(str(r["id"]), 40), Kind: render.Clean(str(r["kind"]), 40), State: render.Clean(str(r["state"]), 40),
			Tier: render.Clean(str(r["tier_after"]), 10), Finished: render.Clean(str(r["finished_at"]), 40),
		})
	}
	return out
}

// dropIdentity removes account identifiers from a JSON value, recursively.
func dropIdentity(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			switch k {
			case "principal_id", "agent_id", "email", "phone", "company":
				continue
			default:
				out[k] = dropIdentity(val)
			}
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = dropIdentity(val)
		}
		return out
	case string:
		return render.Clean(t, 300)
	default:
		return v
	}
}

func validID(s string) bool {
	if s == "" || len(s) > 80 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}
