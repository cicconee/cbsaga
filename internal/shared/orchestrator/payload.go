package orchestrator

import "errors"

type WithdrawalRequestPayload struct {
	WithdrawalID string `json:"withdrawal_id"`
	UserID       string `json:"user_id"`
}

func (p *WithdrawalRequestPayload) Validate() error {
	if p.WithdrawalID == "" {
		return errors.New("withdrawal_id is empty")
	}
	if p.UserID == "" {
		return errors.New("user_id is empty")
	}

	return nil
}
