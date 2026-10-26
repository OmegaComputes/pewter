// Command pewter is the Pewter command-line client (prototype).
package main

import (
	"os"

	"github.com/OmegaComputes/pewter/internal/cli"
)

func main() { os.Exit(cli.Run(os.Args[1:], cli.DefaultEnv())) }
