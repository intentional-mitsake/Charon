package utils

import (
	"charon/pkg/services"
	"context"
	"log/slog"
	"math/rand"
	"net/http"
	"os"
	"time"
)

var logger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{}))

// context is used for signaling
// pointer passing of upstream pool and upstream is very imp
// makes sure mutex dont break
func RunHealthCheck(ctx context.Context, upstreamPool *services.UpstreamPool, interval time.Duration) {
	ticker := time.NewTicker(interval) // create a ticker that ticks every intervalseconds
	defer ticker.Stop()                // stop the ticker when the function returns
	logger.Info("Started health check service", "interval", interval.String())
	// infinite looop that runs until the context is done
	// the context is done when the main function returns
	// the main function returns when i shut down the whole thing
	// so pretty much, this wil run all the tim e and periodically check the health
	// the context passed is the one from the main function
	for {
		select {
		// if the ticker ticks .i.e every interval this runs
		case <-ticker.C:
			logger.Info("Running health check")

			// the problem after adding delay is that all the connections will be checked at the same time
			// so if one connection has failed multiple times, its delay accumulates
			// when healthchekc runs, it fires multi goroutines for each upstream
			// BUT theres a waitgroup that waits for all the goroutines to finish
			// hence due to delay of one upstream, the whole ticker is blocked till it finishes
			// meaning the whole RunHealthCheck func is delayed, so all upstreams get chekcd after the delay
			// before it was here to ensure all upstreams get checked before the ticker ticks again(blocked the ticker)
			// but now the soln is to remove it and make the goroutines run indep of each other and not block the ticker
			//wg := &sync.WaitGroup{}

			// NOTE: after the wg problem fixed, theres another prob, found in logs
			// upstream fails, delay becomes say 17.09, now no block any more, good,
			// BUT next tick is in 15 seconds, health check runs, for ohter servers delay is 1s so they get checked
			// for the failed one, delay again becomes 17.09 because the previous one wiht delay 17.09 is still running
			// hence two chekcs for the single failed upstream is happening at the same time
			// not inherently bad, but still a problem cuz check happens each 15 sec
			// if delay becomes smth like 500 or more, it will fire many many checks for that single failed upstream
			// Whats the sole? the most obvious ans: check if the upstream is getting checked
			// if it is, skip it, if not, check it
			// will use a flag in the upstream struct to check if the upstream is getting checked

			for _, u := range upstreamPool.Upstreams {
				localURL := "http://" + u.Address + "/health"
				//wg.Add(1)
				// local var capture bug, basically cuz its a loop, by the time the loop finishes, the var can be already out of scope
				go func(u *services.Upstream, url string) {
					// check if its getting checked, if it is, skip it, if not, check it
					// so atomic.bool has 4 methods: load, store, swap, compareAndSwap(old, new)
					// this one checks curr val, if its old val change it to new val returns TRUE, if not retruns FALSE
					if !u.BeingChecked.CompareAndSwap(false, true) {
						// if not getting checked(false), check it(chnge to true), return true
						// if getting checked(true) do nothing, return false
						// hence if false--> its getting checked
						logger.Info("Skipping: upstream already getting checked", "address", u.Address)
						return
					}
					// if here, means it was not getting checked(already set to TRUE), so check it AND set it to FALSE once done
					defer u.BeingChecked.Store(false)
					//defer wg.Done()

					// delay before checking health again
					u.Mu.Lock()
					delay := u.CheckInterval
					u.Mu.Unlock()
					logger.Info("Exponential delay with jitter:", "delay", delay.String())
					time.Sleep(delay)

					// check health after delay
					healthy := HealthCheck(url)
					// better way is to just read the latest value
					// AND the BEST way would be to use mutex for the whole thing
					//failCount := atomic.LoadInt64(&u.SuccessiveFails)
					//passCount := atomic.LoadInt64(&u.SuccessivePasses)
					if !healthy {
						// INSTEAD of mixing atomic and mutex, just gonna use mutex for the whole op
						// is slower than atomic, but not that bad
						// not using atomic as need to change multiple vars and bool
						u.Mu.Lock()
						// if unhealthy, first incr successive fails before checking, or its stuck at 0
						//failCount := atomic.AddInt64(&u.SuccessiveFails, 1) // updates AND returns the new value
						//atomic.StoreInt64(&u.SuccessivePasses, 0)           // reset
						u.SuccessiveFails++                               // incr successive fails
						u.SuccessivePasses = 0                            // reset successive passes
						jitterV := time.Duration(rand.Intn(3) + 1)        // random jitter between 1 and 3 seconds, Intn returns [0, n-1]
						u.CheckInterval = (u.CheckInterval * 2) + jitterV // exponential backoff
						//logger.Error("Upstream is not healthy", "url", url)
						if u.SuccessiveFails >= 3 { // check if the fail count is greater than 3
							logger.Error("Upstream is not healthy", "url", url, "Successive Fails", u.SuccessiveFails)
							u.Healthy = false // if crossed fail threshold, set healthy to false
						}
						u.Mu.Unlock()
					} else {
						u.Mu.Lock()
						//passCount := atomic.AddInt64(&u.SuccessivePasses, 1) // same as fail
						//atomic.StoreInt64(&u.SuccessiveFails, 0)
						u.SuccessivePasses++              // incr successive passes
						u.SuccessiveFails = 0             // reset successive fails
						u.CheckInterval = 1 * time.Second // reset check interval
						//logger.Info("Upstream responded", "url", url)
						if u.SuccessivePasses >= 2 { // check if the success count is greater than 2
							logger.Info("Upstream is healthy", "url", url, "Successive Passes", u.SuccessivePasses)
							u.Healthy = true // if healthy, set healthy to true
							//u.SuccessiveFails = 0 // reset successive fails
							//u.SuccessivePasses++  // incr successive passes
						}
						u.Mu.Unlock()
					}
				}(u, localURL) // pass the local upstream and the url
			}
			//wg.Wait()
		case <-ctx.Done(): // if the context is done, stop the loop
			return
		}
	}
}

func HealthCheck(url string) bool {
	client := &http.Client{ // create a http client with a timeout of 2 seconds
		// meaning if the server takes more than 2 seconds to respond, the client will timeout
		// health check for now is: check if the server is up and running AND responds within a given tiem
		Timeout: 2 * time.Second,
	}

	res, err := client.Get(url) // GET /health
	if err != nil {
		// if the request fails, print the error
		// this happens if either the server is down(cant connect, doesnt respond) or it takes more than 2 seconds to respond
		logger.Error("Upstream did not respond | Failed to connect", "error", err.Error())
		return false
	}
	defer res.Body.Close() // close the response body

	if res.StatusCode != http.StatusOK { // if the status code is not 200, print the status code
		logger.Error("Health check failed", "status code", res.StatusCode)
		return false
	}
	logger.Info("Health check passed", "status code", res.StatusCode)
	return true

}
