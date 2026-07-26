package main

import (
	"charon/pkg/services"
	"log/slog"
	"net"
	"os"
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
	for {
		// 2. accepts connections
		conn, err := listener.Accept()
		if err != nil {
			logger.Error("Error accepting connection!", "error", err.Error())
		}
		go services.HandleConnection(conn)
	}
}
