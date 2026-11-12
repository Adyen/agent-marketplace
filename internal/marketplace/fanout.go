package marketplace

import "sync"

// MaxPinConcurrency bounds concurrent per-pin Marketplace resolution.
const MaxPinConcurrency = 8

// ResolveConcurrently calls fn for every index and returns results by index.
// It does not cancel sibling calls after an error so callers retain ordered results.
func ResolveConcurrently[R any](n int, fn func(int) R) []R {
	results := make([]R, n)
	semaphore := make(chan struct{}, MaxPinConcurrency)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			results[i] = fn(i)
		}(i)
	}
	wg.Wait()
	return results
}
