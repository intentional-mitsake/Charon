package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

func main() {
	upstreamPort := os.Getenv("UPSTREAM_PORT")
	if upstreamPort == "" {
		upstreamPort = "8848"
	}
	upstreamAddr := fmt.Sprintf(":%s", upstreamPort)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain") // to test later
		w.Header().Set("Connection", "close")        // force the connection to close

		response := "This is the response from PORT: " + upstreamPort
		// HandleFunc takes a pattern(string) and a handler function
		// the handler func takes a writer and a request, this writes the response to the writer
		// when a requseet is made to port 8848 with pattern "/",
		// it will do this, .i.e write this response to the ResponseWriter
		fmt.Fprint(w, response)
		logger.Info("Responded to request", "Response", response)
	})
	logger.Info("Started listening", "PORT", upstreamPort)
	http.ListenAndServe(upstreamAddr, nil)
}
