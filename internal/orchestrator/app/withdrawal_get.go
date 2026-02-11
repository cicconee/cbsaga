package app

import (
	"context"
	"time"

	"github.com/cicconee/cbsaga/internal/orchestrator/repo"
)

type GetWithdrawalParams struct {
	WithdrawalID string
}

type GetWithdrawalResult struct {
	WithdrawalID    string
	UserID          string
	Asset           string
	AmountMinor     int64
	DestinationAddr string
	Status          string
	FailureReason   *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

func (s *Service) GetWithdrawal(
	ctx context.Context,
	p GetWithdrawalParams,
) (GetWithdrawalResult, error) {
	row, err := s.repo.GetWithdrawal(
		ctx,
		s.db,
		repo.GetWithdrawalParams{WithdrawalID: p.WithdrawalID},
	)
	if err != nil {
		return GetWithdrawalResult{}, err
	}

	return GetWithdrawalResult{
		WithdrawalID:    row.WithdrawalID,
		UserID:          row.UserID,
		Asset:           row.Asset,
		AmountMinor:     row.AmountMinor,
		DestinationAddr: row.DestinationAddr,
		Status:          row.Status,
		FailureReason:   row.FailureReason,
		CreatedAt:       row.CreatedAt,
		UpdatedAt:       row.UpdatedAt,
	}, nil
}
