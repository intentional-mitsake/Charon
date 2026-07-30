package utils

import (
	"charon/pkg/services"
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

var logger = slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{}))

// context is used for signaling
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
			wg := &sync.WaitGroup{}
			mu := &sync.Mutex{}
			for _, u := range upstreamPool.Upstreams {
				localURL := "http://" + u.Address + "/health"
				wg.Add(1)
				go func(url string) {
					defer wg.Done()
					healthy := HealthCheck(url)
					// better way is to just read the latest value
					// AND the BEST way would be to use mutex for the whole thing
					//failCount := atomic.LoadInt64(&u.SuccessiveFails)
					//passCount := atomic.LoadInt64(&u.SuccessivePasses)
					if !healthy {
						// INSTEAD of mixing atomic and mutex, just gonna use mutex for the whole op
						// is slower than atomic, but not that bad
						// not using atomic as need to change multiple vars and bool
						mu.Lock()
						// if unhealthy, first incr successive fails before checking, or its stuck at 0
						//failCount := atomic.AddInt64(&u.SuccessiveFails, 1) // updates AND returns the new value
						//atomic.StoreInt64(&u.SuccessivePasses, 0)           // reset
						u.SuccessiveFails++    // incr successive fails
						u.SuccessivePasses = 0 // reset successive passes
						//logger.Error("Upstream is not healthy", "url", url)
						if u.SuccessiveFails >= 3 { // check if the fail count is greater than 3
							logger.Error("Upstream is not healthy", "url", url, "Successive Fails", u.SuccessiveFails)
							u.Healthy = false // if crossed fail threshold, set healthy to false
						}
						mu.Unlock()
					} else {
						mu.Lock()
						//passCount := atomic.AddInt64(&u.SuccessivePasses, 1) // same as fail
						//atomic.StoreInt64(&u.SuccessiveFails, 0)
						u.SuccessivePasses++  // incr successive passes
						u.SuccessiveFails = 0 // reset successive fails
						//logger.Info("Upstream responded", "url", url)
						if u.SuccessivePasses >= 2 { // check if the success count is greater than 2
							logger.Info("Upstream is healthy", "url", url, "Successive Passes", u.SuccessivePasses)
							u.Healthy = true // if healthy, set healthy to true
							//u.SuccessiveFails = 0 // reset successive fails
							//u.SuccessivePasses++  // incr successive passes
						}
						mu.Unlock()
					}
				}(localURL)
			}
			wg.Wait()
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
