package middleware

import (
	"log/slog"
	"math"
	"os"
	"sync"
	"time"
)

type TokenBucket struct {
	Capacity   float64   // max number of tokens
	Rate       float64   // refil rate in tokens per second
	CurrTokens float64   // current number of tokens
	LastRefill time.Time // last time tokens were refilled
	Mu         sync.Mutex
}

func CreateTokenBucket() *TokenBucket {
	return &TokenBucket{
		Capacity:   100,
		Rate:       10,
		CurrTokens: 100,        // initially set to max
		LastRefill: time.Now(), // initially set to now
		Mu:         sync.Mutex{},
	}
}

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

func (b *TokenBucket) AllowReq(ip string) bool {
	b.Mu.Lock()
	defer b.Mu.Unlock()
	// 1. Check how many tokens have accumulated since last chekc:
	// tokens = min(capacity, current tokens + refil rate * time since last chekc)
	// formula from Medium
	// min cuz if the num fo tokens grows to more than capacity, min wiil limit it to the max capacity
	// so it will stay btwn 0 and capacity and update with each request as per refil rate and last refill time
	b.CurrTokens = math.Min(b.Capacity, b.CurrTokens+(b.Rate*time.Since(b.LastRefill).Seconds()))
	// token is refilled above regardless of whether the request was allowed or not
	// so last refill time is always updated also
	b.LastRefill = time.Now()
	// 2. Decison
	if b.CurrTokens >= 1 { // using float so 0.8 is >0 and allowed, cant have that, so >=1
		// if enough tokens(+ve) have been accumulated, allow the request
		b.CurrTokens--
		logger.Info("Request Allowed!", "IP", ip, "Tokens", b.CurrTokens, "Last Refill", b.LastRefill)
		return true
	} else {
		// if not enough tokens have been accumulated, deny the request
		logger.Info("Request Denied!", "IP", ip, "Tokens", b.CurrTokens, "Last Refill", b.LastRefill)
		return false
	}
}
