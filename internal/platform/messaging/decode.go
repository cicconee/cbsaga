package messaging

import (
	"encoding/json"

	"github.com/cicconee/cbsaga/internal/platform/codec"
)

func DecodeConnectEnvelopeValid(b []byte, v codec.Validater) error {
	var env struct {
		Payload json.RawMessage `json:"payload"`
	}

	if err := json.Unmarshal(b, &env); err != nil {
		return err
	}

	return codec.DecodeValid(env.Payload, v)
}
