package main

import (
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
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
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

	utils.RunHealthCheck(ctx, upstreamPool, 5*time.Second)

	// btw no waitgroups here cuz no need to wait for all connections to finish
	// each go routine(connection) is indep and doesnt need to wait for others, its not concurrent, its parallel
	for {
		// 2. accepts connections
		conn, err := listener.Accept()
		if err != nil {
			logger.Error("Error accepting connection!", "error", err.Error())
		}
		go services.HandleConnection(conn, upstreamPool)
	}
}
