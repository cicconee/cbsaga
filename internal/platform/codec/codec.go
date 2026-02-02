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

func EncodeJSONPtr[T any](v T) (*string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

func DecodeValid(b []byte, v Validater) error {
	if err := json.Unmarshal(b, v); err != nil {
		return err
	}
	return v.Validate()
}

func DecodeJSONPtr[T any](s *string, v T) error {
	b := []byte(*s)
	if err := json.Unmarshal(b, v); err != nil {
		return err
	}
	return nil
}
