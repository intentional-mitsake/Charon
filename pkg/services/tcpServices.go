package services

import (
	"log/slog"
	"net"
	"os"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
var upstreamAddr = os.Getenv("UPSTREAM_ADDR")

func HandleConnection(conn net.Conn) {
	defer conn.Close()
	if upstreamAddr == "" {
		upstreamAddr = "127.0.0.1:8848"
	}
	// 1. already got the tcp conn from client to the proxy(main.go)
	// now open another conn to the upstream server(actual backend server that processes the requests)
	// net.Dial creates a tcp connection as a client to the given address(upstreamAddr/backend server)
	upstreamConn, err := net.Dial("tcp", upstreamAddr)
	if err != nil {
		logger.Error("Error connecting to upstream!", "error", err.Error())
		return
	}
	defer upstreamConn.Close()
	logger.Info("Accepted connection!", "IP", conn.RemoteAddr().String())
}
