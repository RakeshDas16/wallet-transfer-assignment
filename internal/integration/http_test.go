package integration_test

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

type httpTestEnv struct {
	Server *httptest.Server
	Pool   *pgxpool.Pool
}

func newHTTPTestEnv(t *testing.T) *httpTestEnv {
	t.Helper()

	testDB := testutil.StartPostgres(t)
	repo := postgres.NewRepository(testDB.Pool)
	svc := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(svc, slog.New(slog.NewTextHandler(os.Stderr, nil)))

	router := chi.NewRouter()
	router.Post("/transfers", transferHandler.CreateTransfer)
	router.Get("/wallets/{walletId}/balance", transferHandler.GetWalletBalance)

	server := httptest.NewServer(router)
	t.Cleanup(server.Close)

	return &httpTestEnv{Server: server, Pool: testDB.Pool}
}

func TestHTTPEndToEndTransferFlow(t *testing.T) {
	env := newHTTPTestEnv(t)

	testutil.SeedWallet(t, env.Pool, "wallet_1", 1000)
	testutil.SeedWallet(t, env.Pool, "wallet_2", 0)

	first := postTransfer(t, env.Server, map[string]any{
		"idempotencyKey": "e2e-key-1",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	})
	require.Equal(t, http.StatusCreated, first.StatusCode)

	var transfer domain.TransferResponse
	require.NoError(t, json.NewDecoder(first.Body).Decode(&transfer))
	require.Equal(t, domain.TransferStatusProcessed, transfer.Status)
	firstTransferID := transfer.TransferID
	_ = first.Body.Close()

	second := postTransfer(t, env.Server, map[string]any{
		"idempotencyKey": "e2e-key-1",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         100,
	})
	require.Equal(t, http.StatusCreated, second.StatusCode)

	var replay domain.TransferResponse
	require.NoError(t, json.NewDecoder(second.Body).Decode(&replay))
	require.Equal(t, firstTransferID, replay.TransferID)
	_ = second.Body.Close()

	balanceResp, err := http.Get(env.Server.URL + "/wallets/wallet_1/balance")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, balanceResp.StatusCode)

	var balance domain.WalletBalanceResponse
	require.NoError(t, json.NewDecoder(balanceResp.Body).Decode(&balance))
	require.Equal(t, int64(900), balance.Balance)
	_ = balanceResp.Body.Close()

	failed := postTransfer(t, env.Server, map[string]any{
		"idempotencyKey": "e2e-key-fail",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_2",
		"amount":         5000,
	})
	require.Equal(t, http.StatusUnprocessableEntity, failed.StatusCode)

	var failedTransfer domain.TransferResponse
	require.NoError(t, json.NewDecoder(failed.Body).Decode(&failedTransfer))
	require.Equal(t, domain.TransferStatusFailed, failedTransfer.Status)
	_ = failed.Body.Close()

	bad := postTransfer(t, env.Server, map[string]any{
		"idempotencyKey": "e2e-key-bad",
		"fromWalletId":   "wallet_1",
		"toWalletId":     "wallet_1",
		"amount":         10,
	})
	require.Equal(t, http.StatusBadRequest, bad.StatusCode)
	_ = bad.Body.Close()
}

func postTransfer(t *testing.T, server *httptest.Server, payload map[string]any) *http.Response {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	resp, err := http.Post(server.URL+"/transfers", "application/json", bytes.NewReader(body))
	require.NoError(t, err)
	return resp
}
