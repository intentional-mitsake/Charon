package services

import (
	"bufio"
	"charon/pkg/config"
	"fmt"
	"io"
	"net"
	"strconv"
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
		// \r\n is a special char used by protocols like HTTP, SMTP to separate lines and headers
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

// from->client, to->upstream, forward the parsed http request to upstream
func ForwardRequest(req *config.HTTPRequest, from net.Conn, to net.Conn) error {
	// create a writer for the upstream connection
	writer := bufio.NewWriter(to) // again, net.Conn can also be io.Writer

	// write things to the writer( thing that is sent to the upstream server)

	// takes io.Writer, formated string and args, writes the formated string to the io.Writer
	// \r\n used to separate lines and headers in HTTP protocol
	if _, err := fmt.Fprintf(writer, "%s %s %s\r\n", req.Method, req.Path, req.Version); err != nil {
		return err
	}
	logger.Info("Forwarded request!", "Request", req.Method+" "+req.Path+" "+req.Version)

	// headers
	for key, val := range req.Headers {
		if _, err := fmt.Fprintf(writer, "%s: %s\r\n", key, val); err != nil {
			return err
		}
	}

	// inject a X-Forwarded-For header to the request
	// takes Host:Port string, returns IP, PORT, error
	clientIP, _, _ := net.SplitHostPort(from.RemoteAddr().String())
	fmt.Fprintf(writer, "X-Forwarded-For: %s\r\n", clientIP)
	logger.Info("Injected X-Forwarded-For header!", "IP", clientIP)
	// blank line to show end of headers
	if _, err := fmt.Fprint(writer, "\r\n"); err != nil {
		return err
	}
	//logger.Info("End of Headers")
	// Flush actually writes the dat to the upstream server
	// before it, all data is written to a buffer, and then sent(flushed ig) to the upstream all at once
	return writer.Flush() // returns only error, so either nil or err
}

// inverse fo ParseReqeust, returns http response status code
func ParseResponse(conn net.Conn) (*config.HTTPResponse, error) {
	//logger.Info("Parser Called")
	// NOTE: \r\n is used to separate lines and headers
	// \r\n means end of headers and start of body, careful about the diff
	reader := bufio.NewReader(conn)
	httpRes := config.CreateHttpResp()
	// response--> HTTP/1.1 200 OK\r\n
	response, err := reader.ReadString('\n')
	logger.Info("Received response!", "Response", response)
	if err != nil {
		return &config.HTTPResponse{}, err
	}
	words := strings.Fields(response)
	if len(words) < 2 {
		return &config.HTTPResponse{}, fmt.Errorf("invalid response line: %s", response)
	}
	// words[0]--> "HTTP/1.1", words[1]--> "200"
	code, err := strconv.Atoi(words[1]) // Atoi is pretth much ParseInt
	if err != nil {
		return &config.HTTPResponse{}, err
	}
	httpRes.StatusCode = code
	httpRes.Response = response
	// read headers
	for {
		header, err := reader.ReadString('\n')
		if err != nil {
			return &config.HTTPResponse{}, err
		}
		// if the next line is empty(\r\n)
		// \r\n is a special char used by protocols like HTTP, SMTP to separate lines and headers
		if header == "\r\n" {
			break
		}
		words := strings.SplitN(header, ":", 2)
		if len(words) == 2 {
			key := strings.TrimSpace(words[0]) // trim leading and trailing spaces
			val := strings.TrimSpace(words[1])
			httpRes.Headers[key] = val
		}
	}

	// read body
	contentLengthStr, exists := httpRes.Headers["Content-Length"]
	if !exists {
		return &config.HTTPResponse{}, fmt.Errorf("missing Content-Length header")
	}
	contentLength, err := strconv.Atoi(contentLengthStr)
	if err != nil {
		return &config.HTTPResponse{}, err
	}
	// allocating exact amount of memory
	bodyBytes := make([]byte, contentLength)
	// io.ReadFull reads exactly contentLength bytes from the reader
	_, err = io.ReadFull(reader, bodyBytes)
	if err != nil {
		return &config.HTTPResponse{}, err
	}
	httpRes.Body = string(bodyBytes)
	// never do this way, always take the content lenght form header
	//contentLength := len(httpRes.Body)
	//httpRes.Headers["Content-Length"] = fmt.Sprintf("%d", contentLength)

	return &httpRes, nil
}
