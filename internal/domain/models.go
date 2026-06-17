package domain

import (
	"time"

	"github.com/google/uuid"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

type LedgerEntryType string

const (
	LedgerEntryTypeDebit  LedgerEntryType = "DEBIT"
	LedgerEntryTypeCredit LedgerEntryType = "CREDIT"
)

type IdempotencyStatus string

const (
	IdempotencyStatusProcessing IdempotencyStatus = "PROCESSING"
	IdempotencyStatusCompleted  IdempotencyStatus = "COMPLETED"
)

type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type LedgerEntry struct {
	ID         int64
	WalletID   string
	TransferID uuid.UUID
	EntryType  LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

type IdempotencyRecord struct {
	IdempotencyKey string
	RequestHash    string
	Status         IdempotencyStatus
	TransferID     *uuid.UUID
	ResponseStatus *int
	ResponseBody   []byte
	CreatedAt      time.Time
}

type CreateTransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type TransferResponse struct {
	TransferID    uuid.UUID      `json:"transferId"`
	Status        TransferStatus `json:"status"`
	FromWalletID  string         `json:"fromWalletId"`
	ToWalletID    string         `json:"toWalletId"`
	Amount        int64          `json:"amount"`
	FailureReason *string        `json:"failureReason,omitempty"`
}

type WalletBalanceResponse struct {
	WalletID string `json:"walletId"`
	Balance  int64  `json:"balance"`
}

func (s TransferStatus) CanTransitionTo(next TransferStatus) bool {
	switch s {
	case TransferStatusPending:
		return next == TransferStatusProcessed || next == TransferStatusFailed
	case TransferStatusProcessed, TransferStatusFailed:
		return false
	default:
		var status TransferStatus = s
		_ = status
		return false
	}
}

func ValidateCreateTransferRequest(req CreateTransferRequest) error {
	if req.IdempotencyKey == "" {
		return ErrMissingIdempotencyKey
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return ErrMissingWalletID
	}
	if req.FromWalletID == req.ToWalletID {
		return ErrSameWalletTransfer
	}
	if req.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
