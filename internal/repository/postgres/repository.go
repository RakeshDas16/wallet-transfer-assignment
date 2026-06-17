package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

var _ repository.TransferRepository = (*Repository)(nil)

func (r *Repository) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (r *Repository) ClaimIdempotency(ctx context.Context, tx pgx.Tx, key, requestHash string) (*domain.IdempotencyRecord, error) {
	_, err := tx.Exec(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash, status)
		VALUES ($1, $2, 'PROCESSING')
		ON CONFLICT (idempotency_key) DO NOTHING
	`, key, requestHash)
	if err != nil {
		return nil, fmt.Errorf("insert idempotency record: %w", err)
	}

	record, err := scanIdempotencyRecord(tx.QueryRow(ctx, `
		SELECT idempotency_key, request_hash, status, transfer_id, response_status, response_body, created_at
		FROM idempotency_records
		WHERE idempotency_key = $1
		FOR UPDATE
	`, key))
	if err != nil {
		return nil, fmt.Errorf("lock idempotency record: %w", err)
	}

	if record.RequestHash != requestHash {
		return nil, domain.ErrIdempotencyConflict
	}

	return record, nil
}

func (r *Repository) CompleteIdempotency(ctx context.Context, tx pgx.Tx, key string, transferID uuid.UUID, status int, body []byte) error {
	_, err := tx.Exec(ctx, `
		UPDATE idempotency_records
		SET status = 'COMPLETED',
		    transfer_id = $2,
		    response_status = $3,
		    response_body = $4
		WHERE idempotency_key = $1
	`, key, transferID, status, body)
	if err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}

	return nil
}

func (r *Repository) GetWallet(ctx context.Context, tx pgx.Tx, walletID string) (*domain.Wallet, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`, walletID)

	wallet, err := scanWallet(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet: %w", err)
	}

	return wallet, nil
}

func (r *Repository) LockWallets(ctx context.Context, tx pgx.Tx, fromID, toID string) (*domain.Wallet, *domain.Wallet, error) {
	firstID, secondID := fromID, toID
	if firstID > secondID {
		firstID, secondID = secondID, firstID
	}

	first, err := r.lockWallet(ctx, tx, firstID)
	if err != nil {
		return nil, nil, err
	}

	second, err := r.lockWallet(ctx, tx, secondID)
	if err != nil {
		return nil, nil, err
	}

	if fromID == firstID {
		return first, second, nil
	}

	return second, first, nil
}

func (r *Repository) lockWallet(ctx context.Context, tx pgx.Tx, walletID string) (*domain.Wallet, error) {
	row := tx.QueryRow(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
		FOR UPDATE
	`, walletID)

	wallet, err := scanWallet(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("lock wallet: %w", err)
	}

	return wallet, nil
}

func (r *Repository) UpdateWalletBalance(ctx context.Context, tx pgx.Tx, walletID string, delta int64) error {
	tag, err := tx.Exec(ctx, `
		UPDATE wallets
		SET balance = balance + $2,
		    updated_at = NOW()
		WHERE id = $1
		  AND balance + $2 >= 0
	`, walletID, delta)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}

	if tag.RowsAffected() == 0 {
		return domain.ErrInsufficientFunds
	}

	return nil
}

func (r *Repository) CreateTransfer(ctx context.Context, tx pgx.Tx, transfer domain.Transfer) (*domain.Transfer, error) {
	row := tx.QueryRow(ctx, `
		INSERT INTO transfers (idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, failure_reason, created_at, updated_at
	`, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount, transfer.Status, transfer.FailureReason)

	created, err := scanTransfer(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.ConstraintName == "transfers_idempotency_key_key" {
			return nil, fmt.Errorf("duplicate transfer idempotency key: %w", err)
		}
		return nil, fmt.Errorf("create transfer: %w", err)
	}

	return created, nil
}

func (r *Repository) UpdateTransferStatus(ctx context.Context, tx pgx.Tx, transferID uuid.UUID, status domain.TransferStatus, failureReason *string) error {
	_, err := tx.Exec(ctx, `
		UPDATE transfers
		SET status = $2,
		    failure_reason = $3,
		    updated_at = NOW()
		WHERE id = $1
	`, transferID, status, failureReason)
	if err != nil {
		return fmt.Errorf("update transfer status: %w", err)
	}

	return nil
}

func (r *Repository) CreateLedgerEntry(ctx context.Context, tx pgx.Tx, entry domain.LedgerEntry) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO ledger_entries (wallet_id, transfer_id, entry_type, amount)
		VALUES ($1, $2, $3, $4)
	`, entry.WalletID, entry.TransferID, entry.EntryType, entry.Amount)
	if err != nil {
		return fmt.Errorf("create ledger entry: %w", err)
	}

	return nil
}

func (r *Repository) GetTransferLedgerEntries(ctx context.Context, tx pgx.Tx, transferID uuid.UUID) ([]domain.LedgerEntry, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, wallet_id, transfer_id, entry_type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = $1
		ORDER BY id
	`, transferID)
	if err != nil {
		return nil, fmt.Errorf("get ledger entries: %w", err)
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		entry, err := scanLedgerEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, *entry)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate ledger entries: %w", err)
	}

	return entries, nil
}

func (r *Repository) GetWalletBalance(ctx context.Context, walletID string) (*domain.Wallet, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, balance, created_at, updated_at
		FROM wallets
		WHERE id = $1
	`, walletID)

	wallet, err := scanWallet(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, domain.ErrWalletNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get wallet balance: %w", err)
	}

	return wallet, nil
}

func (r *Repository) EnsureWallet(ctx context.Context, tx pgx.Tx, walletID string, balance int64) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO wallets (id, balance)
		VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE
		SET balance = EXCLUDED.balance,
		    updated_at = NOW()
	`, walletID, balance)
	if err != nil {
		return fmt.Errorf("ensure wallet: %w", err)
	}

	return nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanWallet(row scannable) (*domain.Wallet, error) {
	var wallet domain.Wallet
	if err := row.Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt); err != nil {
		return nil, err
	}
	return &wallet, nil
}

func scanTransfer(row scannable) (*domain.Transfer, error) {
	var transfer domain.Transfer
	if err := row.Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.Status,
		&transfer.FailureReason,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &transfer, nil
}

func scanLedgerEntry(row scannable) (*domain.LedgerEntry, error) {
	var entry domain.LedgerEntry
	if err := row.Scan(&entry.ID, &entry.WalletID, &entry.TransferID, &entry.EntryType, &entry.Amount, &entry.CreatedAt); err != nil {
		return nil, err
	}
	return &entry, nil
}

func scanIdempotencyRecord(row scannable) (*domain.IdempotencyRecord, error) {
	var record domain.IdempotencyRecord
	if err := row.Scan(
		&record.IdempotencyKey,
		&record.RequestHash,
		&record.Status,
		&record.TransferID,
		&record.ResponseStatus,
		&record.ResponseBody,
		&record.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &record, nil
}
