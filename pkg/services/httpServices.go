package services

import (
	"charon/pkg/config"
	"net"
)

// takes a connection and returns the http request
func ParseRequest(conn *net.Conn) (*config.HTTPRequest, error) {
	return nil, nil
}
