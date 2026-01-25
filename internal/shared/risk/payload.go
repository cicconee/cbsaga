package risk

import "errors"

type RiskCheckRequestPayload struct {
	WithdrawalID    string `json:"withdrawal_id"`
	UserID          string `json:"user_id"`
	Asset           string `json:"asset"`
	AmountMinor     int64  `json:"amount_minor"`
	DestinationAddr string `json:"destination_addr"`
}

func (p *RiskCheckRequestPayload) Validate() error {
	if p.WithdrawalID == "" {
		return errors.New("withdrawal_id is empty")
	}
	if p.UserID == "" {
		return errors.New("user_id is empty")
	}
	if p.Asset == "" {
		return errors.New("asset is empty")
	}
	if p.AmountMinor <= 0 {
		return errors.New("asset_minor not greater than zero")
	}
	if p.DestinationAddr == "" {
		return errors.New("destination_addr is empty")
	}

	return nil
}
