package services

import (
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// for least connections algo
type Upstream struct {
	Address         string // 127.0.0.1:8848
	ActiveConns     int64  // 0, should never access this directly, cuz it can be changed concurrently
	Healthy         bool   // fro health checks later
	SuccessiveFails int64  // to calc consecutive fails to determine health
}

func CreateUpstream(address string) Upstream {
	return Upstream{Address: address,
		ActiveConns:     0,
		Healthy:         true,
		SuccessiveFails: 0,
	}
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
	Upstreams         []*Upstream // pointer cuz if use slice, the swap problem happens
	RoundRobinCounter int64       // for fallback
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
		pool.Upstreams = append(pool.Upstreams, &Upstream{
			Address:         strings.TrimSpace(addr),
			ActiveConns:     0,
			Healthy:         true,
			SuccessiveFails: 0,
		})
	}
	return pool
}

// ref for least conn from geeksforgeeks
func (p *UpstreamPool) SelectUpstream() *Upstream {
	//p.mu.RLock() // RLock() means this data is only going to be read, its not going to be changed
	// so it can be read concurrently, but not changed
	//defer p.mu.RUnlock()
	p.mu.Lock()
	defer p.mu.Unlock()                  // adding RoundRobin means it needs to be modified(written to)
	var leastConn = int64(math.MaxInt64) // a really large num so that it can be changed to min value later
	//var selectedIndx = math.MaxInt       // to check whether any upstream is healthy(if no change, no select)
	var selectOptions []int
	for indx, u := range p.Upstreams {
		if !u.Healthy {
			continue
		}
		conns := atomic.LoadInt64(&u.ActiveConns) // never directlu
		if conns < leastConn {
			leastConn = conns           // never directly
			selectOptions = []int{indx} // reset and add current index
		} else if conns == leastConn { // a conn has same val as leastConn, means tie, multi with same,
			// round robin time
			selectOptions = append(selectOptions, indx) // if tie, append, already has prev indx(leastConn)
		}
	}

	if len(selectOptions) == 0 { // if none were selected/healthy
		logger.Info("No healthy upstreams available!")
		return nil
	}
	var selectedIndx int
	if len(selectOptions) == 1 { // if only one option
		selectedIndx = selectOptions[0]
	} else { // if more than one
		// (56+1) % 3 = 0, (1983+1) % 2 = 1
		p.RoundRobinCounter++
		// to make this really unpredictable, i could do this:
		// p.RoundRobinCounter = (p.RoundRobinCounter + 1) % int64(len(selectOptions))
		// p.RoundRobinCounter always stays btwn 0-len(selectOptions)-1, so this is fine
		// in the version used, p.RoundRobinCounter grows by 1 every time
		// it will become a very large num eventually, so prob need to reset
		// or mayba apply the unpredictable method
		option := (p.RoundRobinCounter + 1) % int64(len(selectOptions)) // round robin
		// [0,1,2] -> selectOptions[1]-->1
		selectedIndx = selectOptions[option]
	}
	logger.Info("Selected upstream", "Upstream", p.Upstreams[selectedIndx].Address, "Connections", leastConn)
	return p.Upstreams[selectedIndx]
}
