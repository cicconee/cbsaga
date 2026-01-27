package app

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

type validatedCreateWithdrawal struct {
	UserID          string
	Asset           string
	AmountMinor     int64
	DestinationAddr string
	IdempotencyKey  string
	TraceID         string
	RequestHash     string
}

func NewValidatedCreateWithdrawal(p CreateWithdrawalParams) (validatedCreateWithdrawal, error) {
	userID := strings.TrimSpace(p.UserID)
	asset := strings.ToUpper(strings.TrimSpace(p.Asset))
	dest := strings.TrimSpace(p.DestinationAddr)
	idemKey := strings.TrimSpace(p.IdempotencyKey)
	traceID := p.TraceID

	if userID == "" || asset == "" || dest == "" || idemKey == "" {
		return validatedCreateWithdrawal{}, errors.New("invalid input: missing required fields")
	}
	if p.AmountMinor <= 0 {
		return validatedCreateWithdrawal{}, errors.New("invalid input: amount_minor must be > 0")
	}
	if traceID == "" {
		traceID = uuid.NewString()
	}

	canonical := fmt.Sprintf("user_id=%s|asset=%s|amount_minor=%d|destination_addr=%s",
		userID,
		asset,
		p.AmountMinor,
		dest,
	)
	sum := sha256.Sum256([]byte(canonical))
	reqHash := hex.EncodeToString(sum[:])

	return validatedCreateWithdrawal{
		UserID:          userID,
		Asset:           asset,
		AmountMinor:     p.AmountMinor,
		DestinationAddr: dest,
		IdempotencyKey:  idemKey,
		TraceID:         traceID,
		RequestHash:     reqHash,
	}, nil
}
