package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

func main() {
	upstreamPort := os.Getenv("UPSTREAM_PORT")
	serverName := os.Getenv("SERVER_NAME")
	if upstreamPort == "" {
		upstreamPort = "8848"
	}
	upstreamAddr := fmt.Sprintf(":%s", upstreamPort)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain") // to test later
		w.Header().Set("Connection", "close")        // force the connection to close

		response := "This is the response from backend: " + serverName
		contentLength := len(response)
		w.Header().Set("Content-Length", fmt.Sprintf("%d", contentLength))
		// 200 ms delay on each request so that servers can ACQUIRE for longer to test the load balancer
		time.Sleep(200 * time.Millisecond)
		// HandleFunc takes a pattern(string) and a handler function
		// the handler func takes a writer and a request, this writes the response to the writer
		// when a requseet is made to port 8848 with pattern "/",
		// it will do this, .i.e write this response to the ResponseWriter
		fmt.Fprint(w, response)
		logger.Info("Responded to request", "Upstream", serverName, "Response", response)
	})

	// health endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// set headeers
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Connection", "close")
		w.WriteHeader(http.StatusOK) // HTTP 200 OK
		logger.Info("Responded to health check request")
	})
	logger.Info("Started listening", "PORT", upstreamPort)
	http.ListenAndServe(upstreamAddr, nil)
}
