package main

import (
	"charon/pkg/middleware"
	"charon/pkg/services"
	"charon/pkg/utils"
	"context"
	"log/slog"
	"net"
	"os"
	"time"
)

// ref from: medium @diasmashikovnasa
// basically building a reverse proxy
func main() {
	var logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))
	// Start proxy on port 80
	port := os.Getenv("PORT")
	if port == "" {
		port = "80"
	}
	addr := ":" + port
	// 1. takes network and address(port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		logger.Error("Error creating listener!", "error", err.Error())
		// exit if error as that means no listening
		os.Exit(1)
	}
	logger.Info("Started listening", "PORT", port)

	// initilaze upstream pool
	upstreamPool := services.InitUpstreamPool()
	if upstreamPool == nil {
		logger.Error("Error initializing upstream pool")
		os.Exit(1)

	}

	// Health check setup
	// diff types of context, context.Background returns empty context, no cancellation, no value
	// pretty much just a placeholder context that is used for signaling when the main function is done
	// as its only done when this function returns
	ctx := context.Background()
	// checked the desc and it returns <-chan struct{}, goat for signaling
	defer ctx.Done() // close the context when the main function returns

	intervalStr := os.Getenv("HC_INTERVAL")
	if intervalStr == "" {
		intervalStr = "15s"
	}
	interval, err := time.ParseDuration(intervalStr) // takes a string liek '300ms', '5s' and returns a duration
	if err != nil {
		logger.Error("Error parsing health check interval!", "error", err.Error())
		os.Exit(1)
	}

	// if run as a normal func without goroutine, it blocks the main func from exiting
	// AND pauses execution at this line, meaning for loop accept connections will never run
	go utils.RunHealthCheck(ctx, upstreamPool, interval)

	// btw no waitgroups here cuz no need to wait for all connections to finish
	// each go routine(connection) is indep and doesnt need to wait for others, its not concurrent, its parallel
	for {
		// 2. accepts connections
		conn, err := listener.Accept()
		if err != nil {
			logger.Error("Error accepting connection!", "error", err.Error())
		}
		clientAddr := conn.RemoteAddr().String()
		ip, _, _ := net.SplitHostPort(clientAddr)

		// auto checks if ip has a bucket for it, if no creates new
		allowed, headers := middleware.AllowReq(ip)
		if !allowed {
			// if not allowed, close the conn immediately
			conn.Write([]byte("HTTP/1.1 429 Too Many Requests\r\n\r\n"))
			conn.Close()
			continue // skip to the next iteration of the for loop from here(next connection)
		}
		// due to continue, if not allowed, this line never runs
		// 3. handles the connection
		go services.HandleConnection(conn, upstreamPool, headers)
	}
}
