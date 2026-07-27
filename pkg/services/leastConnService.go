package services

import (
	"charon/pkg/config"
	"log/slog"
	"os"
)

var log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

// algo ref from geeksforgeeks
func SelectUpstream(upstreamList []config.Upstream) config.Upstream {
	var leastConn = 1000 // let it be max initially, so it can be immediately replaced on first itertation
	var selectedIndx int
	for indx, upstream := range upstreamList {
		if upstream.ActiveConns < leastConn {
			leastConn = upstream.ActiveConns
			selectedIndx = indx
		}
	}
	return upstreamList[selectedIndx]
}
