// Command pewter-mock serves the CLI's mock API on 127.0.0.1 for local demos.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/OmegaComputes/pewter/internal/mock"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8799", "listen address (loopback only)")
	flag.Parse()
	mux := http.NewServeMux()
	mux.Handle("/api/v1/", mock.New())
	fmt.Printf("pewter-mock on http://%s/api/v1\nfake agent key for this mock only: %s\n", *addr, mock.AgentToken)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
