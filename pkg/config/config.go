package config

import (
	"sync"
	"time"
)

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

type HTTPResponse struct {
	StatusCode int
	Response   string
	Headers    map[string]string
	Body       string
}

type TokenBucket struct {
	Capacity   float64   // max number of tokens
	Rate       float64   // refil rate in tokens per second
	CurrTokens float64   // current number of tokens
	LastRefill time.Time // last time tokens were refilled
	Mu         sync.Mutex
}

func CreateHttpReq() HTTPRequest {
	return HTTPRequest{
		// panics if not initialized
		Headers: make(map[string]string),
	}
}

func CreateHttpResp() HTTPResponse {
	return HTTPResponse{
		Headers: make(map[string]string),
	}
}

func CreateTokenBucket(capacity float64, rate float64) *TokenBucket {
	return &TokenBucket{
		Capacity:   capacity,
		Rate:       rate,
		CurrTokens: capacity,
		LastRefill: time.Now(),
		Mu:         sync.Mutex{},
	}
}
