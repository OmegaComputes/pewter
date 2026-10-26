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

func (a *app) cmdStatus() int {
	raw, err := a.get("/health", nil, false)
	if err != nil {
		return a.failErr(err)
	}
	return a.emit(raw, func() error {
		var h map[string]any
		if err := json.Unmarshal(raw, &h); err != nil {
			return err
		}
		fmt.Fprintf(a.Stdout, "API      %s\nstatus   %s\nversion  %s\ntime     %s\n", render.Clean(a.apiURL, 120), render.Clean(str(h["status"]), 20), render.Clean(str(h["api_version"]), 20), render.Clean(str(h["time"]), 40))
		return nil
	})
}

func (a *app) cmdWhoami() int {
	raw, err := a.get("/me", nil, true)
	if err != nil {
		return a.failErr(err)
	}
	return a.emit(raw, func() error {
		var d map[string]any
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		actor, pr, ag := m(d["actor"]), m(d["principal"]), m(d["agent"])
		name := firstNonEmpty(str(pr["display_name"]), str(d["name"]), str(d["id"]))
		fmt.Fprintf(a.Stdout, "signed in  %s\n", render.Clean(name, 80))
		if v := str(actor["via"]); v != "" {
			fmt.Fprintf(a.Stdout, "via        %s (%s)\n", render.Clean(v, 20), render.Clean(str(actor["role"]), 20))
		}
		if v := str(ag["name"]); v != "" {
			fmt.Fprintf(a.Stdout, "agent      %s\n", render.Clean(v, 80))
		}
		if d["sandbox_only"] == true {
			fmt.Fprintln(a.Stdout, "mode       sandbox")
		}
		fmt.Fprintf(a.Stdout, "api        %s\n", render.Clean(a.apiURL, 120))
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
	return a.emit(raw, func() error {
		var rows []map[string]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			return err
		}
		var out [][]string
		for _, r := range rows {
			if status != "" && str(r["status"]) != status {
				continue
			}
			cap, gpu, price, as := m(r["capability"]), m(m(r["capability"])["gpu"]), m(m(r["capability"])["price"]), m(r["assurance"])
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
			_ = cap
			out = append(out, []string{str(r["id"]), str(r["status"]), str(r["title"]), model, usd(price["amount_usd"]), str(as["tier"]), str(as["validation"])})
		}
		render.Table(a.Stdout, []string{"ID", "STATUS", "TITLE", "GPUS", "USD/GPU-HR", "TIER", "VALIDATION"}, out)
		return nil
	})
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
	return a.emit(raw, func() error {
		var rows []map[string]any
		if err := json.Unmarshal(raw, &rows); err != nil {
			return err
		}
		var out [][]string
		for _, r := range rows {
			out = append(out, []string{str(r["id"]), str(r["side"]), str(r["status"]), str(r["instrument_id"]), usd(r["price_usd_per_unit"]), str(r["gpus"]), str(r["hours"]), str(r["book"])})
		}
		render.Table(a.Stdout, []string{"ORDER", "SIDE", "STATUS", "INSTRUMENT", "USD/GPU-HR", "GPUS", "HOURS", "BOOK"}, out)
		return nil
	})
}

func (a *app) cmdBalance() int {
	raw, err := a.get("/wallet", nil, true)
	if err != nil {
		return a.failErr(err)
	}
	return a.emit(raw, func() error {
		var w map[string]any
		if err := json.Unmarshal(raw, &w); err != nil {
			return err
		}
		label := "Live balance"
		if w["sandbox"] == true {
			label = "Sandbox balance (simulated funds)"
		}
		cur := firstNonEmpty(str(w["currency"]), "USD")
		fmt.Fprintf(a.Stdout, "%s\n", label)
		fmt.Fprintf(a.Stdout, "  balance    %s %s\n  reserved   %s %s\n  available  %s %s\n", usd(w["balance_usd"]), cur, usd(w["reserved_usd"]), cur, usd(w["available_usd"]), cur)
		if es, ok := w["entries"].([]any); ok {
			fmt.Fprintf(a.Stdout, "  entries    %d (use --json for the list)\n", len(es))
		}
		if n := str(w["note"]); n != "" {
			fmt.Fprintf(a.Stdout, "\n%s\n", render.Clean(n, 300))
		}
		return nil
	})
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
	return a.emit(raw, func() error {
		var d struct {
			Listing map[string]any   `json:"listing"`
			Runs    []map[string]any `json:"runs"`
			Avail   bool             `json:"validator_available"`
		}
		if err := json.Unmarshal(raw, &d); err != nil {
			return err
		}
		as := m(d.Listing["assurance"])
		fmt.Fprintf(a.Stdout, "listing    %s (%s)\ntier       %s\nvalidation %s\n", render.Clean(str(d.Listing["id"]), 40), render.Clean(str(d.Listing["status"]), 20), render.Clean(str(as["tier"]), 10), render.Clean(str(as["validation"]), 30))
		if sim := as["simulated"]; sim == true {
			fmt.Fprintln(a.Stdout, "note       simulated validation (sandbox)")
		}
		var out [][]string
		for _, r := range d.Runs {
			out = append(out, []string{str(r["id"]), str(r["kind"]), str(r["state"]), str(r["tier_after"]), str(r["finished_at"])})
		}
		if len(out) > 0 {
			fmt.Fprintln(a.Stdout)
			render.Table(a.Stdout, []string{"RUN", "KIND", "STATE", "TIER", "FINISHED"}, out)
		}
		return nil
	})
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
