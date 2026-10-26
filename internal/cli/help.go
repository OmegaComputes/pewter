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
  login           Sign in (device code in your browser, or --with-token < key.txt)
  logout          Remove the token from this machine
  whoami          Show who you are signed in as
  status          Check that the API is reachable

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
  --api-url URL   API base (env PEWTER_API_URL). https only; http for localhost
  --profile NAME  Credential profile (env PEWTER_PROFILE, default "default")

Environment
  PEWTER_TOKEN    API key for CI and scripts (overrides the stored token)
  PEWTER_KEYSTORE auto | keychain | file
  PEWTER_CONFIG_DIR  Where non-secret settings live (default ~/.config/pewter)

Exit codes  0 ok, 1 error, 2 usage, 3 not signed in, 4 network

Contact: devs@h3ir.com   Site: omegacompute.com
`

func (a *app) printHelp(w io.Writer) { fmt.Fprint(w, topHelp) }

var cmdHelp = map[string]string{
	"login": `pewter login [--with-token]

Without options: starts a device-code sign-in. The CLI shows a short code and a
link on omegacompute.com; you approve it in your browser or in the Pewter app.
The token is stored in your OS keychain (macOS Keychain, or Secret Service on
Linux) and falls back to a 0600 file. It is never printed.

--with-token   Read an API key from stdin instead:
               pewter login --with-token < key.txt
`,
	"orders":      "pewter orders [--status open|matched|cancelled|...]\n\nLists your own orders. Read-only. --json prints the API data as is.\n",
	"listings":    "pewter listings [--status live|draft|...]\n\nLists your own seller listings with tier and validation state. Read-only.\n",
	"balance":     "pewter balance\n\nShows your balance as reported by the API. Read-only.\n",
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
    COMPREPLY=( $(compgen -W "--json --api-url --profile --help" -- "$cur") )
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
    _arguments '--json[machine-readable output]' '--api-url[API base URL]:url:' '--profile[credential profile]:name:'
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
