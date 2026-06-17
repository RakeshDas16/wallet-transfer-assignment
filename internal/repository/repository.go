package repository

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type TransferRepository interface {
	WithTx(ctx context.Context, fn func(pgx.Tx) error) error
	ClaimIdempotency(ctx context.Context, tx pgx.Tx, key, requestHash string) (*domain.IdempotencyRecord, error)
	CompleteIdempotency(ctx context.Context, tx pgx.Tx, key string, transferID uuid.UUID, status int, body []byte) error
	GetWallet(ctx context.Context, tx pgx.Tx, walletID string) (*domain.Wallet, error)
	LockWallets(ctx context.Context, tx pgx.Tx, firstID, secondID string) (from, to *domain.Wallet, err error)
	UpdateWalletBalance(ctx context.Context, tx pgx.Tx, walletID string, delta int64) error
	CreateTransfer(ctx context.Context, tx pgx.Tx, transfer domain.Transfer) (*domain.Transfer, error)
	UpdateTransferStatus(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, status domain.TransferStatus, failureReason *string) error
	CreateLedgerEntry(ctx context.Context, tx pgx.Tx, entry domain.LedgerEntry) error
	GetTransferLedgerEntries(ctx context.Context, tx pgx.Tx, transferID uuid.UUID) ([]domain.LedgerEntry, error)
	GetWalletBalance(ctx context.Context, walletID string) (*domain.Wallet, error)
	EnsureWallet(ctx context.Context, tx pgx.Tx, walletID string, balance int64) error
}
