package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

func GetEnv(key string, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return def
}

func GetEnvDuration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}

	d, err := time.ParseDuration(v)
	if err == nil {
		return d
	}

	if secs, secsErr := strconv.Atoi(v); secsErr == nil {
		return time.Duration(secs) * time.Second
	}

	return def
}

func SplitCSV(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}

	return out
}
