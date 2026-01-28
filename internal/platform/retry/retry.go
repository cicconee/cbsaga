package retry

import (
	"context"
	"math/rand"
	"time"
)

type Config struct {
	MaxAttempts int
	BaseDelay   time.Duration
	MaxDelay    time.Duration
	Jitter      time.Duration
	IsRetryable func(error) bool
}

func normalizeConfig(cfg Config) Config {
	def := DefaultConfig()

	if cfg.MaxAttempts > 0 {
		def.MaxAttempts = cfg.MaxAttempts
	}
	if cfg.BaseDelay > 0 {
		def.BaseDelay = cfg.BaseDelay
	}
	if cfg.MaxDelay > 0 {
		def.MaxDelay = cfg.MaxDelay
	}
	if cfg.Jitter > 0 {
		def.Jitter = cfg.Jitter
	}
	if cfg.IsRetryable != nil {
		def.IsRetryable = cfg.IsRetryable
	}

	return def
}

func Do(ctx context.Context, cfg Config, fn func() error) error {
	cfg = normalizeConfig(cfg)

	var attempt int
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		attempt++
		err := fn()
		if err == nil {
			return nil
		}
		if attempt >= cfg.MaxAttempts || !cfg.IsRetryable(err) {
			return err
		}

		sleep := backoffDelay(cfg.BaseDelay, cfg.MaxDelay, attempt)
		if cfg.Jitter > 0 {
			sleep = applyJitter(sleep, cfg.Jitter)
		}

		timer := time.NewTimer(sleep)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

func backoffDelay(baseDelay time.Duration, maxDelay time.Duration, attempt int) time.Duration {
	mul := 1 << (attempt - 1)
	backoff := baseDelay * time.Duration(mul)
	if backoff > maxDelay {
		return maxDelay
	}
	return backoff
}

func applyJitter(delay time.Duration, jitter time.Duration) time.Duration {
	// Int63n returns a value in [0, n). Int63n(n*2+1) returns a value in [0, 2n]. Therefore
	// Int63n(n*2+1) - n returns a value in [-n, n]
	delta := time.Duration(rand.Int63n(int64(jitter)*2+1)) - jitter
	res := delay + delta
	if res < 0 {
		return 0
	}
	return res
}
