package testserver

import (
	"fmt"
	"net/http"
	"os"
)

func main() {
	upstreamPort := os.Getenv("UPSTREAM_PORT")
	if upstreamPort == "" {
		upstreamPort = "8848"
	}
	upstreamAddr := fmt.Sprintf(":%s", upstreamPort)
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// HandleFunc takes a pattern(string) and a handler function
		// the handler func takes a writer and a request, this writes the response to the writer
		// when a requseet is made to port 8848 with pattern "/",
		// it will do this, .i.e write this response to the ResponseWriter
		fmt.Fprintf(w, "This is the response from PORT: %s", upstreamPort)
	})
	http.ListenAndServe(upstreamAddr, nil)
}
