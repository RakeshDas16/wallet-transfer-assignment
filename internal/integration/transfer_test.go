package integration_test

import (
	"context"
	"sync"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

func newService(t *testing.T) (*service.TransferService, *postgres.Repository, *pgxpool.Pool) {
	t.Helper()

	testDB := testutil.StartPostgres(t)
	repo := postgres.NewRepository(testDB.Pool)
	return service.NewTransferService(repo), repo, testDB.Pool
}

func TestTransferSuccessUpdatesBalancesAndLedger(t *testing.T) {
	svc, repo, pool := newService(t)
	ctx := context.Background()

	testutil.SeedWallet(t, pool, "wallet_1", 1000)
	testutil.SeedWallet(t, pool, "wallet_2", 200)

	result, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "transfer-success",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	require.NoError(t, err)
	require.Equal(t, 201, result.HTTPStatus)
	require.Equal(t, domain.TransferStatusProcessed, result.Response.Status)

	from, err := repo.GetWalletBalance(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(900), from.Balance)

	to, err := repo.GetWalletBalance(ctx, "wallet_2")
	require.NoError(t, err)
	require.Equal(t, int64(300), to.Balance)

	var entries []domain.LedgerEntry
	err = repo.WithTx(ctx, func(tx pgx.Tx) error {
		var txErr error
		entries, txErr = repo.GetTransferLedgerEntries(ctx, tx, result.Response.TransferID)
		return txErr
	})
	require.NoError(t, err)
	require.Len(t, entries, 2)

	debits := 0
	credits := 0
	for _, entry := range entries {
		switch entry.EntryType {
		case domain.LedgerEntryTypeDebit:
			debits += int(entry.Amount)
			require.Equal(t, "wallet_1", entry.WalletID)
		case domain.LedgerEntryTypeCredit:
			credits += int(entry.Amount)
			require.Equal(t, "wallet_2", entry.WalletID)
		default:
			t.Fatalf("unexpected entry type: %s", entry.EntryType)
		}
	}
	require.Equal(t, debits, credits)
}

func TestIdempotentReplayReturnsOriginalResult(t *testing.T) {
	svc, repo, pool := newService(t)
	ctx := context.Background()

	testutil.SeedWallet(t, pool, "wallet_1", 500)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	req := domain.CreateTransferRequest{
		IdempotencyKey: "same-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         50,
	}

	first, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	require.False(t, first.Replayed)

	second, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	require.True(t, second.Replayed)
	require.Equal(t, first.Response.TransferID, second.Response.TransferID)
	require.Equal(t, first.HTTPStatus, second.HTTPStatus)

	from, err := repo.GetWalletBalance(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(450), from.Balance)
}

func TestInsufficientFundsMarksTransferFailed(t *testing.T) {
	svc, repo, pool := newService(t)
	ctx := context.Background()

	testutil.SeedWallet(t, pool, "wallet_1", 40)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	result, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "insufficient-funds",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	require.NoError(t, err)
	require.Equal(t, 422, result.HTTPStatus)
	require.Equal(t, domain.TransferStatusFailed, result.Response.Status)
	require.NotNil(t, result.Response.FailureReason)

	from, err := repo.GetWalletBalance(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(40), from.Balance)
}

func TestIdempotencyKeyConflictOnDifferentPayload(t *testing.T) {
	svc, _, pool := newService(t)
	ctx := context.Background()

	testutil.SeedWallet(t, pool, "wallet_1", 500)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	_, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "conflict-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         50,
	})
	require.NoError(t, err)

	_, err = svc.CreateTransfer(ctx, domain.CreateTransferRequest{
		IdempotencyKey: "conflict-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         75,
	})
	require.ErrorIs(t, err, domain.ErrIdempotencyConflict)
}

func TestConcurrentTransfersDoNotOverdraw(t *testing.T) {
	svc, repo, pool := newService(t)
	ctx := context.Background()

	testutil.SeedWallet(t, pool, "wallet_1", 100)
	testutil.SeedWallet(t, pool, "wallet_2", 0)
	testutil.SeedWallet(t, pool, "wallet_3", 0)

	const attempts = 10
	var wg sync.WaitGroup
	wg.Add(attempts)

	successes := 0
	failures := 0
	var mu sync.Mutex

	for i := 0; i < attempts; i++ {
		go func(i int) {
			defer wg.Done()

			toWallet := "wallet_2"
			if i%2 == 0 {
				toWallet = "wallet_3"
			}

			result, err := svc.CreateTransfer(ctx, domain.CreateTransferRequest{
				IdempotencyKey: uuid.NewString(),
				FromWalletID:   "wallet_1",
				ToWalletID:     toWallet,
				Amount:         60,
			})

			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				failures++
				return
			}

			if result.HTTPStatus == 201 {
				successes++
			} else {
				failures++
			}
		}(i)
	}

	wg.Wait()

	require.Equal(t, 1, successes)
	require.Equal(t, attempts-1, failures)

	from, err := repo.GetWalletBalance(ctx, "wallet_1")
	require.NoError(t, err)
	require.Equal(t, int64(40), from.Balance)
}
