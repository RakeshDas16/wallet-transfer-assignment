package domain

import "errors"

var (
	ErrMissingIdempotencyKey = errors.New("idempotencyKey is required")
	ErrMissingWalletID       = errors.New("fromWalletId and toWalletId are required")
	ErrSameWalletTransfer    = errors.New("fromWalletId and toWalletId must differ")
	ErrInvalidAmount         = errors.New("amount must be greater than zero")
	ErrWalletNotFound        = errors.New("wallet not found")
	ErrInsufficientFunds     = errors.New("insufficient funds")
	ErrIdempotencyConflict   = errors.New("idempotency key reused with different request payload")
)
