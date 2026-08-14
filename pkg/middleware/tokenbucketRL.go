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

type Buckets struct {
	IP     string
	Bucket *TokenBucket
	Mu     sync.Mutex
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

var logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))

// after some research, sync.Map is a thread safe map meaning it can be used concurrently
// BUT its not good for write heavy operations, for those, use standard mutex
// sync.Map is good for read heavy operations which is what the rate limiter needs
// it wrties once(if no bycket for the ip), but reads multiple times(if there is a bucket fo the ip)
// was going to use another struct like UpstreamPool to store the bucket for each ip
// but this seemed simpler
var BucketForIp sync.Map

func (b *TokenBucket) allow(ip string) bool {
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
		// to see how many tokens WHEN allowed, NOT after currenttokens--
		logger.Info("Request Allowed!", "IP", ip, "Tokens", b.CurrTokens, "Last Refill", b.LastRefill)
		// if enough tokens(+ve) have been accumulated, allow the request
		b.CurrTokens--
		return true
	} else {
		// if not enough tokens have been accumulated, deny the request
		logger.Info("Request Denied!", "IP", ip, "Tokens", b.CurrTokens, "Last Refill", b.LastRefill)
		return false
	}
}

func AllowReq(ip string) (bool, map[string]any) {
	// LoadOrStore(key, value) as the name suggests, loads the value for the key if present in the map,
	// else it stores and returns the value provides as arg
	// it returns two things: the value loaded and a bool indicating whether the key was present
	// the value loaded is of type any(interface{}), so need to typecast
	// eg if value was a string, value.(string), if value was int, value.(int),
	// and if value was struct, value.(*struct), ptr cuz it shouldnt be copied to a new variable, instead it should be used
	// in normal LoadOrStore, if no key, it returns nil and stores val
	// but in LoadOrStore of sync.Map, if no key, it returns the value provided as arg and stores it
	val, loaded := BucketForIp.LoadOrStore(ip, CreateTokenBucket())
	if !loaded {
		logger.Info("New IP!", "IP", ip)
	}
	bucket := val.(*TokenBucket)
	// for headers
	header := make(map[string]any)
	header["X-RateLimit-Limit"] = bucket.Capacity
	header["X-RateLimit-Remaining"] = bucket.CurrTokens
	header["X-RateLimit-Reset"] = bucket.LastRefill.Unix()

	return bucket.allow(ip), header
}
