package codec

import "encoding/json"

type Validater interface {
	Validate() error
}

func EncodeValid(v Validater) ([]byte, error) {
	if err := v.Validate(); err != nil {
		return nil, err
	}

	return json.Marshal(v)
}

func DecodeValid(b []byte, v Validater) error {
	if err := json.Unmarshal(b, v); err != nil {
		return err
	}
	return v.Validate()
}
