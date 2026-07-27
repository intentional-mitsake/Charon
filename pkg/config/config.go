package config

import "sync/atomic"

/*
GET /auth?params HTTP/1.1
Host: api.example.com
User-Agent: Go-http-client/1.1
Accept: application/json
Accept-Encoding: gzip
*/
type HTTPRequest struct {
	Method  string            // GET, POST
	Path    string            // /auth, /users, /
	Version string            // HTTP/1.1
	Headers map[string]string // Authorization: Bearer XXXXXXXX(key: value), Accept: application/json
	Body    string            // json
}

func CreateHttpReq() HTTPRequest {
	return HTTPRequest{
		// panics if not initialized
		Headers: make(map[string]string),
	}
}

// for least connections algo
type Upstream struct {
	Address     string // 127.0.0.1:8848
	ActiveConns int64  // 0
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
