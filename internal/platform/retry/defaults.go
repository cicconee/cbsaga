package retry

import "time"

const (
	DefaultMaxAttempts = 3
	DefaultBaseDelay   = 50 * time.Millisecond
	DefaultMaxDelay    = 500 * time.Millisecond
	DefaultJitter      = 25 * time.Millisecond
)

func DefaultConfig() Config {
	return Config{
		MaxAttempts: DefaultMaxAttempts,
		BaseDelay:   DefaultBaseDelay,
		MaxDelay:    DefaultMaxDelay,
		Jitter:      DefaultJitter,
	}
}
