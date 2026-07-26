package services

import (
	"bufio"
	"charon/pkg/config"
	"fmt"
	"net"
	"strings"
)

// takes a connection and returns the http request
func ParseRequest(conn net.Conn) (*config.HTTPRequest, error) {
	// bufio is used for parsing
	// io funcs treat data as stream of bytes and dump everything from A to B
	// no allow to read, split or see parts of it inidividually
	reader := bufio.NewReader(conn) // net.Conn is also an io.Reader
	// read the first line .i.e the request--> "GET /auth?params HTTP/1.1 \n"
	request, err := reader.ReadString('\n')
	if err != nil {
		return nil, err
	}
	words := strings.Fields(request) // split the request string into words
	httpRequest := config.CreateHttpReq()
	if len(words) < 3 {
		return nil, fmt.Errorf("invalid request line: %s", request)
	}
	httpRequest.Method = words[0]
	httpRequest.Path = words[1]
	httpRequest.Version = words[2]

	// read headers
	for {
		header, err := reader.ReadString('\n')
		if err != nil {
			return nil, err
		}
		// if the next line is empty(\r\n)
		if header == "\r\n" {
			break
		}
		// header--> Content-Type: application/json
		// words--> ["Content-Type:", "application/json"]
		// words[0], words[1] always as headers are in this format
		// using SplintN instead of Fields cuz no need to split into individual words
		// headers format is always key: value, so only need to split at :
		words := strings.SplitN(header, ":", 2)
		if len(words) == 2 {
			key := strings.TrimSpace(words[0]) // trim leading and trailing spaces
			val := strings.TrimSpace(words[1])
			httpRequest.Headers[key] = val
		}
	}
	return &httpRequest, nil
}
