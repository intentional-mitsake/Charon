package services

import (
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"
	"time"

	_ "github.com/google/uuid"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

func HandleConnection(clientConn net.Conn, upstreamPool *UpstreamPool, headers map[string]any) {
	start := time.Now()                       // for latency
	upstream := upstreamPool.SelectUpstream() // select least conn
	if upstream == nil {
		// in case of som err
		logger.Error("No upstream available!")
		// Write func sends raw bytes direct to the client on the TCP conn
		// \r\n for end of status line, 2nd \r\n for end of headers
		// the client conn stops reading from this point cuz of \r\n\r\n(endof headers) adn no body
		// return closes the conn itself by triggering defer clientConn.Close()
		// a http parser on client side reads this and stops the conn cuz it got a respnse
		clientConn.Write([]byte("HTTP/1.1 503 No Upstream Available\r\n\r\n"))
		return
	}

	// Circuit Breaker
	allowed := upstream.CircuitBreaker()
	if !allowed {
		// 503 service unavailable
		clientConn.Write([]byte("HTTP/1.1 503 Service Unavailable\r\n\r\n"))
		return
	}

	upstream.Acquire()       // acquire the least conn, add 1
	defer upstream.Release() // release the least conn, sub 1
	// 1. Parse the connection
	logger.Info("Accepted connection!", "IP", clientConn.RemoteAddr().String())
	defer clientConn.Close()

	req, err := ParseRequest(clientConn)
	if err != nil {
		logger.Error("Error while parsing request!", "error", err.Error())
		// 400 bad request
		clientConn.Write([]byte("HTTP/1.1 400 Bad Request\r\n\r\n"))
		return // exit if cant parse
	}

	// 2. already got the tcp conn from client to the proxy(main.go)
	// now open another conn to the upstream server(actual backend server that processes the requests)
	// net.Dial creates a tcp connection as a client to the given address(upstreamAddr/backend server)
	/* upstreamConn, err := net.Dial("tcp", upstream.Address)
	if err != nil {
		logger.Error("Error connecting to upstream!", "error", err.Error())
		// 503 service unavailable
		clientConn.Write([]byte("HTTP/1.1 503 Failed to connect to Upstream\r\n\r\n"))
		return // exit if cnat connec t ot the upstream
	}
	defer upstreamConn.Close()
	// need to use the same reader for upstream cuz ForwardRequest and ParseResponse uses it
	*/
	upstreamConn, err := upstream.PullConn()
	if err != nil {
		logger.Error("Error connecting to upstream!", "error", err.Error())
		// 503 service unavailable
		clientConn.Write([]byte("HTTP/1.1 503 Failed to connect to Upstream\r\n\r\n"))
		// incr circuit breaker failure
		upstream.ChangeCBState(true)
		return // exit if cnat connect ot the upstream
	}
	//defer upstreamConn.Close() // close the upstream conn directly

	// 3. send and receive data to and fro the proxy and the upstream server

	// sending parsed req to upstream, first dir
	err = ForwardRequest(req, clientConn, upstreamConn) // from->client, to->upstream
	if err != nil {
		logger.Error("Error forwarding request!", "error", err.Error())
		// 502 bad gateway
		clientConn.Write([]byte("HTTP/1.1 502 Failed to forward request\r\n\r\n"))
		// incr circuit breaker failure
		upstream.ChangeCBState(true)
		// close the upstream conn directly
		upstreamConn.Close()
		return
	}
	//logger.Debug("Forwarding Complete-Response Parsing Started")

	httpResponse, err := ParseResponse(upstreamConn)
	if err != nil {
		// close the upstream conn directly
		upstreamConn.Close()
		logger.Error("Error parsing response!", "error", err.Error())
		if err == io.EOF {
			// when i killed backend 2, this happens, EOF,
			// its cuz of conn pool, b2 has some pooled conn whn it was up,
			// after it goes down, ParseResponse tries to read the data from one of these conn
			// this only happens if backend2 is selected after going down(which only happens for 3 fail cases consectuively)
			logger.Info("Dialing a new connection to the upstream", "Upstream", upstream.Address)
			newConn, freshErr := net.Dial("tcp", upstream.Address) // dial a new conn
			if freshErr != nil {
				logger.Error("Error connecting to the upstream", "Upstream", upstream.Address)
				clientConn.Write([]byte("HTTP/1.1 503 Failed to connect to Upstream\r\n\r\n"))
				upstream.ChangeCBState(true)
				return
			}
			logger.Info("Retrying request forwarding", "Upstream", upstream.Address)
			freshErr = ForwardRequest(req, clientConn, newConn) // retry the request
			if freshErr != nil {
				logger.Error("Error forwarding request!", "error", freshErr.Error())
				// 502 bad gateway
				clientConn.Write([]byte("HTTP/1.1 502 Failed to forward request\r\n\r\n"))
				// incr circuit breaker failure
				upstream.ChangeCBState(true)
				return
			}
			logger.Info("Retrying Response Parsing", "Upstream", upstream.Address)
			httpResponse, err = ParseResponse(newConn) // retry parsing
			if err != nil {
				logger.Error("Error parsing response!", "error", err.Error())
				// 502 bad gateway
				clientConn.Write([]byte("HTTP/1.1 502 Failed to parse response\r\n\r\n"))
				// incr circuit breaker failure
				upstream.ChangeCBState(true)
				return
			}
			upstreamConn = newConn // use the new conn
		} else { // NON EOF error
			// 502 bad gateway
			clientConn.Write([]byte("HTTP/1.1 502 Failed to parse response\r\n\r\n"))
			// incr circuit breaker failure
			upstream.ChangeCBState(true)
			return
		}
	}

	// passive health check
	errCode := strconv.Itoa(httpResponse.StatusCode)
	if len(errCode) == 3 {
		errCodeCategory := string(errCode[0]) + "XX" // 2xx, 3xx, 4xx, 5xx
		if errCodeCategory == "5XX" {
			upstream.PassiveHealthUpdate(false)
		} else {
			upstream.PassiveHealthUpdate(true)
		}
	}

	// close or push the conn to the pool
	keepAlive := httpResponse.Headers["Connection"]
	if keepAlive == "keep-alive" {
		// push the conn back to the pool if its kept alive
		upstream.PushConn(upstreamConn)
	} else {
		upstreamConn.Close()
	}

	response := httpResponse.Response // HTTP/1.1 200 OK\r\n --> already has \r\n, adding another \r\n wuld signal end of headers
	for key, val := range httpResponse.Headers {
		response += fmt.Sprintf("%s: %s\r\n", key, val) // Content-Type: text/html\r\n
	}
	for key, val := range headers {
		// %v is for value, it looks at the type of the value and uses it
		response += fmt.Sprintf("%s: %v\r\n", key, val) // Content-Type: text/html\r\n
	}
	response += "\r\n"            // blank line to show end of headers
	response += httpResponse.Body // HTML from upstream server
	// NO \r\n after the body, HTTP rule, content lenght is the length of the body, \r\n adds to that
	//"Connection: close\r\n\r\n" // header + separator + end of headers
	// second dir, send response from upstream to client

	// write the response line(first line, HTTP/1.1 200 OK) to the clientConn writer
	// pretty much sending the response line to the client
	fmt.Fprint(clientConn, response) // takes io.Writer and args, writes the args to the io.Writer
	//clientConn.Write([]byte(response))
	// after the response line, headers and body stll remain
	// headers(Content-Type) and body(HTML) from upstream to client stlil ramins
	// io.Copy is doing that
	// also before the two io.Copy wwre happening simultaneously
	// so two separate goroutines were used to make them run simultaneously
	// now the first one(client to upstream) is already done(ParseRequest, ForwardRequest),
	// now the second one(upstream to client) is the only one left, no need to run simul
	// only need to make sure this func(HandleConn) doesnt exit before sending a response to client
	// so no goroutine needed, io.Copy blocks and continously reads(from upstream) and writes(to client) until conn close or error
	//io.Copy(clientConn, upstreamConn) // BLOCKS the main goroutine till the connection is closed or error occurs
	// now that body is aslo read in ParseResponse, no need for io.Copy, it was there just to read the leftover(body)

	// log the entire req at the end
	logger.Info("Request completed!",
		"Method", req.Method,
		"Path", req.Path,
		"Status", httpResponse.StatusCode,
		"Latency", time.Since(start).String(), //  pretty much time.Now().Sub(start)
	)
	upstream.ChangeCBState(false)
	/*
		This was Layer 4 implementation, above is Layer 7 implementation
			// in GO, chan struct{} is used for synchronization, it covers 0 bytes of ram,
			// and is ussd for signaling
			// size 2(can hold up to 2 values(signals) at the same time witohut having to clear it)
			// it can be used for both dir(client->upstream, upstream->client) SIMULTANEOUSLY(no clear)
			done := make(chan struct{}, 2)
			// io.Copy is a synchronous, blocking function meaning it will not return until it's done
			// it is done if upstreamConn is closed or error occurs
			// so it BLOCKS the GOROUTINE untill eithre the connection closes or error occurs(keeps it alive)
			// also it keeps continuously reading and writing to adn fro
			go func() {
				io.Copy(upstreamConn, conn)
				// this next line doesnt execute immediately, io.Copy is BLOCKING the whole goroutine
				// so it executes only after io.Copy is done(conn close/error)
				// done <- struct{}{} just signals that the goroutine is done
				// it reports when io.Copy is done(conn close/error)
				// due to 0 bytes of ram, iits used here for signalling, int/bool etc are not used
				done <- struct{}{}
			}()
			go func() { io.Copy(conn, upstreamConn); done <- struct{}{} }()
			// thsi waits for both the goroutines to finish and recieves the signal(done <- struct{}{})
			// one for each goroutine, the prog doesnt hit these until the goroutines are done(BLCOKING)
			// in sum, the goroutines send empty structs(struct{}{}) as signal of completion to the done chan
			// done chan can hold up to 2 values at the same time, so it cna be used for bothe goroutines without having to clear
			// this also stops/BLOCKS the main goroutine(HandleConnection) right here untill it receives the two signals for each line
			// hence io.Copy blocks/keeps the goroutines alive, done <- struct{}{} signals that the goroutine is done
			//  <- done pauses the go HandleConnection in main.go untill the goroutines are done(till conn close/error)
			// at the end 3 separate goroutines are running(kept alive), one for each direction and one for handling those
			<-done
			<-done
	*/
}
