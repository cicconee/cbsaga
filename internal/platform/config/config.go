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

func GetEnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return def
	}

	b, err := strconv.ParseBool(v)
	if err != nil {
		return def
	}

	return b
}

func GetEnvFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}

	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return f
	}

	return f
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
