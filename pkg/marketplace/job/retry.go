package job

import (
	"math/rand"
	"time"
)

// Backoff constants.
const (
	BaseBackoff = time.Second
	MaxBackoff  = time.Hour
)

// NextRetryDelay returns an exponential backoff with full jitter, capped at
// [MaxBackoff]. The returned duration is in [0, delay] where delay doubles
// with each attempt.
func NextRetryDelay(attempt int) time.Duration {
	delay := baseDelay(attempt)
	if delay <= 0 {
		return 0
	}
	return time.Duration(rand.Int63n(int64(delay)))
}

func baseDelay(attempt int) time.Duration {
	if attempt <= 0 {
		return 0
	}
	delay := BaseBackoff
	for i := 1; i < attempt; i++ {
		if delay > MaxBackoff/2 {
			return MaxBackoff
		}
		delay *= 2
	}
	if delay > MaxBackoff {
		return MaxBackoff
	}
	return delay
}
