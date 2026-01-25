package messaging

import "github.com/segmentio/kafka-go"

type Headers map[string][]byte

func NewHeaders(headers []kafka.Header) Headers {
	m := make(Headers, len(headers))
	for _, h := range headers {
		m[h.Key] = h.Value
	}
	return m
}

func (h Headers) String(key string) (string, bool) {
	v, ok := h[key]
	if !ok {
		return "", false
	}
	return string(v), true
}
