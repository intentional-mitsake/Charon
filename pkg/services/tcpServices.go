package services

import (
	"io"
	"log/slog"
	"net"
	"os"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))
var upstreamAddr = os.Getenv("UPSTREAM_ADDR")

// init() is called when the package is imported automatically, its like a constructor
func init() {
	// cant have this insdiee HandleConnection
	// its a goroutine, so reading and writing to upstreamAddr var becomes a race
	if upstreamAddr == "" {
		// backend default
		upstreamAddr = "127.0.0.1:8848"
	}
}

func HandleConnection(conn net.Conn) {
	// 1. Log the connection
	logger.Info("Accepted connection!", "IP", conn.RemoteAddr().String())
	defer conn.Close()

	// 2. already got the tcp conn from client to the proxy(main.go)
	// now open another conn to the upstream server(actual backend server that processes the requests)
	// net.Dial creates a tcp connection as a client to the given address(upstreamAddr/backend server)
	upstreamConn, err := net.Dial("tcp", upstreamAddr)
	if err != nil {
		logger.Error("Error connecting to upstream!", "error", err.Error())
		return
	}
	defer upstreamConn.Close()
	// 3. send and receive data to and fro the proxy and the upstream server
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
}
