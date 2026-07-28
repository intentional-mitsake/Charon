package services

import (
	"log/slog"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

var log = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

// for least connections algo
type Upstream struct {
	Address     string // 127.0.0.1:8848
	ActiveConns int64  // 0, should never access this directly, cuz it can be changed concurrently
	Healthy     bool   // fro health checks later
}

func CreateUpstream(address string) Upstream {
	return Upstream{Address: address, ActiveConns: 0, Healthy: true}
}

func (u *Upstream) Acquire() {
	// func is same as using:
	// mu.Lock() -> counter++ -> mu.Unlock()
	// diff is mu can cover blocks of code(mutli var), wheras this is only for a single var(int, pointer)
	// also this is way faster & mu BLOCKS the thread, this doesnt
	// basically cuz we only upd a single var, mutex is too costly, can be done with atomic
	atomic.AddInt64(&u.ActiveConns, 1)
}

func (u *Upstream) Release() {
	atomic.AddInt64(&u.ActiveConns, -1)
}

type UpstreamPool struct {
	Upstreams []*Upstream // pointer cuz if use slice, the swap problem happens
	// Mutex doesnt allow concqurrent access, go routines wait
	// RWMutex allows concurrent access, mutliple can read simul, but only 1 can write, for writing wait
	mu sync.RWMutex // due to this being a slice, cant use atomic, must use mutex to keep it safe from concurrent access
}

func InitUpstreamPool() *UpstreamPool {
	addrs := os.Getenv("UPSTREAM_ADDRS")
	if addrs == "" {
		addrs = "127.0.0.1:8848,127.0.0.1:8849,127.0.0.1:8850"
	}
	pool := &UpstreamPool{}
	for _, addr := range strings.Split(addrs, ",") {
		pool.Upstreams = append(pool.Upstreams, &Upstream{Address: strings.TrimSpace(addr), ActiveConns: 0})
	}
	return pool
}

// ref for least conn from geeksforgeeks
func (p *UpstreamPool) SelectUpstream() *Upstream {
	p.mu.RLock() // RLock() means this data is only going to be read, its not going to be changed
	// so it can be read concurrently, but not changed
	defer p.mu.RUnlock()
	var leastConn = int64(math.MaxInt64) // a really large num so that it can be changed to min value later
	var selectedIndx = 0
	for indx, u := range p.Upstreams {
		conns := atomic.LoadInt64(&u.ActiveConns) // never directlu
		if conns < leastConn {
			leastConn = conns // never directly
			selectedIndx = indx
		}
	}

	return p.Upstreams[selectedIndx]
}
