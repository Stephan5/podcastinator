package feedrss

import "time"

// SetFetchRetryWait shortens the wait between HEAD retries for a test.
func SetFetchRetryWait(d time.Duration) (restore func()) {
	old := fetchRetryWait
	fetchRetryWait = d
	return func() { fetchRetryWait = old }
}
