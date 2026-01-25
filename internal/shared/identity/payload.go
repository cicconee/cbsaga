package identity

import "errors"

type IdentityRequestCmdPayload struct {
	WithdrawalID string `json:"withdrawal_id"`
	UserID       string `json:"user_id"`
}

func (p *IdentityRequestCmdPayload) Validate() error {
	if p.WithdrawalID == "" {
		return errors.New("withdrawal_id is empty")
	}
	if p.UserID == "" {
		return errors.New("user_id is empty")
	}

	return nil
}

type IdentityRequestEvtPayload struct {
	WithdrawalID string  `json:"withdrawal_id"`
	UserID       string  `json:"user_id"`
	Reason       *string `json:"reason,omitempty"`
}

func (p *IdentityRequestEvtPayload) Validate() error {
	if p.WithdrawalID == "" {
		return errors.New("withdrawal_id is empty")
	}
	if p.UserID == "" {
		return errors.New("user_id is empty")
	}

	return nil
}
