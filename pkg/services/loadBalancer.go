package services

import (
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
)

var log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

// for least connections algo
type Upstream struct {
	Address     string // 127.0.0.1:8848
	ActiveConns int64  // 0, should never access this directly, cuz it can be changed concurrently
}

func CreateUpstream(address string) Upstream {
	return Upstream{Address: address, ActiveConns: 0}
}

func (u *Upstream) ACquire() {
	// func is same as using:
	// mu.Lock() -> counter++ -> mu.Unlock()
	// diff is mu can cover blocks of code(mutli var), wheras this is only for a single var(int, pointer)
	// also this is way faster & mu BLOCKS the thread, this doesnt
	atomic.AddInt64(&u.ActiveConns, 1)
}

func (u *Upstream) Release() {
	atomic.AddInt64(&u.ActiveConns, -1)
}

type UpstreamPool struct {
	Upstreams []Upstream
	mu        sync.Mutex
}
