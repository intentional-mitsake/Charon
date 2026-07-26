package services

import (
	"log/slog"
	"net"
	"os"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

func HandleConnection(conn net.Conn) {
	// auto close connection
	defer conn.Close()
	logger.Info("Accepted connection!", "IP", conn.RemoteAddr().String())
}
