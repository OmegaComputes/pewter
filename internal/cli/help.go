package cli

import (
	"fmt"
	"io"
	"strings"
)

const topHelp = `pewter: orchestration for GPU compute, from the terminal.
Short alias: pew (identical; use either).

Usage:
  pewter <command> [options]
  pew <command> [options]

Account
  login           Sign in (approve a code in the Pewter app, or --with-token)
  logout          Revoke the credential, then remove it from this machine
  whoami          Show name, credential, scopes, expiry, sandbox flag and host
  status          Check that the API is reachable and print its host

Read
  instruments     List instruments you can trade (--gpu H100 --term od --region US)
  listings        Your seller listings (--status live)
  orders          Your orders (--status open)
  balance         Your balance
  verify <id>     Validation status of one of your listings

Unavailable in this release
  rfq  buy  ask  sell  accept

Other
  version         Print the version (--json for details)
  completion      Print a shell completion script (bash or zsh)
  help [command]

Global options
  --json          Machine-readable output on stdout; errors as JSON on stderr
  --api-url URL   API base (env PEWTER_API_URL). v0.1.0 allows the Pewter staging API only, plus loopback for tests
  --profile NAME  Credential profile (env PEWTER_PROFILE, default "default")

Environment
  PEWTER_TOKEN       A pck_ key for CI and scripts (overrides the stored token; never written)
  PEWTER_KEYSTORE    auto | keychain | file
  PEWTER_CONFIG_DIR  Where non-secret settings live (default ~/.config/pewter)

Exit codes  0 ok, 1 error, 2 usage, 3 not signed in, 4 network

HEIR PTE. LTD., trading as Omega Compute. Contact: devs@h3ir.com   Site: omegacompute.com
`

func (a *app) printHelp(w io.Writer) { fmt.Fprint(w, topHelp) }

var cmdHelp = map[string]string{
	"login": `pewter login [--with-token]

Without options: starts a device-code sign-in. The CLI prints a verification
URL and a short code. Approve it in the Pewter app. The credential is a pck_
key, stored in your OS keychain (macOS Keychain, or Secret Service on Linux)
or a 0600 file. It is never printed.

--with-token   Read a pck_ key from stdin instead:
               pewter login --with-token < key.txt
               omk_ keys and pws_ sessions are refused and not stored.
`,
	"logout":      "pewter logout\n\nRevokes the credential on the server, then removes the local token. A 401 (already revoked or expired) still removes the local token. When the key comes from PEWTER_TOKEN, the server credential is revoked and you are told to unset the variable.\n",
	"whoami":      "pewter whoami\n\nShows your display name, credential id, scopes, expiry, sandbox flag and API host. Email, phone, company and principal id are not printed.\n",
	"orders":      "pewter orders [--status open|matched|cancelled|...]\n\nLists your own orders. Read-only. --json prints those columns and the order id. principal_id and agent_id are omitted.\n",
	"listings":    "pewter listings [--status live|draft|...]\n\nLists your own seller listings with tier and validation state. Read-only. --json prints those columns and the listing id.\n",
	"balance":     "pewter balance\n\nShows your balance as reported by the API. Read-only. --json includes the entry list without principal_id or agent_id.\n",
	"instruments": "pewter instruments [--gpu H100] [--term od|spot|1w|1m] [--region US] [--limit N]\n\nPublic list of instruments. No sign-in needed.\n",
	"verify":      "pewter verify <listing-id>\n\nShows the latest validation runs and tier for one of your listings. Read-only.\n",
}

func (a *app) help(cmd string) int {
	if s, ok := cmdHelp[cmd]; ok {
		fmt.Fprint(a.Stdout, s)
		return ExitOK
	}
	a.printHelp(a.Stdout)
	return ExitOK
}

const bashCompletion = `# bash completion for pewter and its alias pew
_pewter() {
  local cur="${COMP_WORDS[COMP_CWORD]}"
  if [ "$COMP_CWORD" -eq 1 ]; then
    COMPREPLY=( $(compgen -W "login logout whoami status instruments listings orders balance verify version completion help" -- "$cur") )
  else
    COMPREPLY=( $(compgen -W "--json --api-url --profile --help --with-token" -- "$cur") )
  fi
}
complete -F _pewter pewter pew
`

const zshCompletion = `#compdef pewter pew
_pewter() {
  local -a cmds
  cmds=(login logout whoami status instruments listings orders balance verify version completion help)
  if (( CURRENT == 2 )); then
    _describe 'command' cmds
  else
    _arguments '--json[machine-readable output]' '--api-url[API base URL]:url:' '--profile[credential profile]:name:' '--with-token[read a pck_ key from stdin]'
  fi
}
compdef _pewter pewter pew
`

func (a *app) cmdCompletion(args []string) int {
	if len(args) != 1 {
		return a.fail(ExitUsage, "usage_error", "usage: pewter completion bash|zsh")
	}
	switch strings.ToLower(args[0]) {
	case "bash":
		fmt.Fprint(a.Stdout, bashCompletion)
	case "zsh":
		fmt.Fprint(a.Stdout, zshCompletion)
	default:
		return a.fail(ExitUsage, "usage_error", "supported shells: bash, zsh")
	}
	return ExitOK
}
