package identity

import "errors"

type IdentityRequestPayload struct {
	WithdrawalID string `json:"withdrawal_id"`
	UserID       string `json:"user_id"`
}

func (p *IdentityRequestPayload) Validate() error {
	if p.WithdrawalID == "" {
		return errors.New("withdrawal_id is empty")
	}
	if p.UserID == "" {
		return errors.New("user_id is empty")
	}

	return nil
}
