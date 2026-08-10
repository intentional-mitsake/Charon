package services

import (
	"math"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// this is the stnadard way to create enum in GO
type CircuitBreakerState int

const (
	// ref: learn.microsoft/en-us/azure/patterns/circuit-breaker
	// by defalut, circuit breaker is closed
	CLOSED    CircuitBreakerState = iota // ioat acts as an auto-incr counter starting at 0
	OPEN                                 // 1
	HALF_OPEN                            // 2 -->incr with each line by one from the line of iota
)

const timeout = 3 * time.Second
const threshold = 5

// for least connections algo
type Upstream struct {
	Address          string        // 127.0.0.1:8848
	ActiveConns      int64         // 0, should never access this directly, cuz it can be changed concurrently
	Healthy          bool          // fro health checks later
	SuccessiveFails  int64         // to calc consecutive fails to determine health
	SuccessivePasses int64         // to calc consecutive passes
	CheckInterval    time.Duration // for failure backoff
	// also prev way of using mu by creating a new one for each ticker trigger was heavily flawed
	// it was creating a new mu for each run, then using that same mu for each upstream in the for loop
	// instead of that, now, each upstream has its own mu to use whenever needed,
	// same instance of mutex transfer thruout the whole project by passing upstream as a pointer everywehre
	// is the right way
	Mu sync.RWMutex // will prob use mu for all except single acccess things now
	// instead of dialing a new tcp conn for each, each upstream will have a buffer channel
	// this channel will have a collection of net.Conn, upstream can pull from here instead
	Pool chan net.Conn

	// for the check delay problem after wg
	BeingChecked atomic.Bool // found out theres a way to use atomic for bools
	// for cicuit breaker
	// ref: learn.microsoft/en-us/azure/patterns/circuit-breaker
	CBState        CircuitBreakerState // to check if circuit breaker is open
	CBFailureCount int64               // default should be 0
	CBSuccessCount int64               // default should be 0
	CBLastCheck    time.Time           // from ref, to reset the circuit breaker
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
	// pointer here makes sure mutex doesnt break, it doesnt get copied and always uses the same mutex instance
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
			Address:          strings.TrimSpace(addr),
			ActiveConns:      0,
			Healthy:          true,
			SuccessiveFails:  0,
			SuccessivePasses: 0,
			CheckInterval:    1 * time.Second,
			Pool:             make(chan net.Conn, 10),
			Mu:               sync.RWMutex{},
			CBFailureCount:   0,
			CBSuccessCount:   0,
			CBLastCheck:      time.Now(),
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
		u.Mu.RLock() // can be read concurrentlyl, but cant be changed here
		healthy := u.Healthy
		u.Mu.RUnlock()
		if !healthy {
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

func (u *Upstream) PassiveHealthUpdate(pass bool) {
	u.Mu.Lock() // lock the upstream to prevent concurrent access
	defer u.Mu.Unlock()
	if pass { // status code was 2xx
		u.SuccessiveFails = 0
		u.SuccessivePasses++
		if u.SuccessivePasses > 2 && !u.Healthy {
			u.Healthy = true // restore
			logger.Info("Restored upstream", "Upstream", u.Address)
		}
	} else {
		u.SuccessiveFails++ // status code was not 2xx, fail
		u.SuccessivePasses = 0
		if u.SuccessiveFails > 3 && u.Healthy {
			u.Healthy = false // mark unhealthy
			logger.Info("Marked upstream unhealthy", "Upstream", u.Address)
		}
	}
}

// if is synchronous and immediate, it checks the lcoal mem, can lead to ToCTou issues cuz if mutli threads concurrent access
// select solves thsi with atomicity, it is asynchronous and threadsafe cuz:
// atoimicity means check and action happens as a single operation(GO does this behind the scens auto for select)
// basically: if cannot handle concurrent stuff(ToCToU issue), its immediate and only checks local mem
// select is atomic(check and use happens as one op), async and can be both blocking and non-blocking and checks the real time state
func (u *Upstream) PullConn() (net.Conn, error) {
	// chan are already threadsafe, so no need for mutex

	// <-chanVar : read only channel, can only read from it, not write to it
	// chan<-var : write only channel, can only write to it, not read from it
	//conn := <-u.Pool : this is a blocking statement
	// chan blocks the whole func till it recieves a value, it doesnt send a nil
	// just waits forever till it recieves a value, so usig if is bad, select is the way
	//if conn == nil {
	// if is synchronous and immediate, it chechks local mem,
	// meaning it doesnt wait for the chan to recieve a val, no other thread/goroutine is involved
	// it is purely synchronous, cant use for chan
	// select is fit for chan cause it blocks the entire goroutine till one of the cases is true
	// it is asynchronous and can block or not block(if therss a default case)
	// here the select is non-blocking, if there is no conn in the pool, it will immediately go to the default case
	// without default, it would be blocking, i.e it would wait for a conn to be added to the pool
	select {
	case conn := <-u.Pool:
		logger.Info("Got connection from the pool", "Upstream", u.Address)
		return conn, nil
	default:
		// already have an address and ohter info abt the upstream in the struct
		// only need to dial an actual connection now, before it was done in tcpServices.go
		logger.Info("No connections available in the pool, dialing upstream", "Upstream", u.Address)
		conn, err := net.Dial("tcp", u.Address)
		if err != nil {
			return nil, err
		}
		return conn, nil
	}
}

func (u *Upstream) PushConn(conn net.Conn) {
	// similar to the use for pull
	// here it tries to push the conn into the chan
	// if it fails, instead fo blocking, .ie waiting till there is space in pool to push, it goes to default immediately
	// without defalut, this wuld wait(block the goroutine) till there is space
	select {
	case u.Pool <- conn:
		// this will happen if the pool is not full
		// the conn will be added to the pool
		logger.Info("Added connection to the pool", "Upstream", u.Address)
	default:
		// else this will happen if the pool is full
		logger.Info("No space in the pool, closing connection", "Upstream", u.Address)
		conn.Close()
	}
}

func (u *Upstream) CircuitBreaker() bool {
	u.Mu.Lock()
	defer u.Mu.Unlock()
	// check open
	if u.CBState == OPEN {
		// check timeout
		if time.Since(u.CBLastCheck) > timeout {
			// if its timed out, reset
			logger.Info("Circuit breaker reset to half open", "Upstream", u.Address)
			// reset to HALF_OPEN
			u.CBState = HALF_OPEN
			u.CBLastCheck = time.Now()
			return true // let the request pass
		} else {
			// if its not timed out, close the conn
			logger.Info("Circuit breaker is open", "Upstream", u.Address)
			return false
		}

	} else if u.CBState == HALF_OPEN {
		if u.BeingChecked.CompareAndSwap(false, true) {
			// only one goroutine can get it
			return true
		}
		return false
	} else {
		// if its not open, do nothing, let the tcpServices handle it
		logger.Info("Circuit breaker is closed", "Upstream", u.Address)
		return true
	}
}

func (u *Upstream) ChangeCBState(reqFail bool) {
	u.Mu.Lock()
	defer u.Mu.Unlock()
	// if request fails
	if reqFail {
		// increment failure count
		u.CBFailureCount++
		// if either fc passes threshold or state is half open, set state to open
		if u.CBFailureCount > threshold || u.CBState == HALF_OPEN {
			u.CBSuccessCount = 0 // reset success count
			u.CBState = OPEN
			logger.Info("Circuit breaker opened", "Upstream", u.Address)
			u.CBLastCheck = time.Now() // set last check to now
			// this will make suer being checked is false so that others dont get blocked
			u.BeingChecked.Store(false)
		}
	} else if !reqFail {
		// if request passes, increment success count, reset failure count
		u.CBFailureCount = 0
		u.CBSuccessCount++
		if u.CBSuccessCount > threshold {
			// if success count passes threshold, set state to closed
			u.CBState = CLOSED
			logger.Info("Circuit breaker closed", "Upstream", u.Address)
			// set last check to now
			u.CBLastCheck = time.Now()
			u.BeingChecked.Store(false)
		}
	}
}
