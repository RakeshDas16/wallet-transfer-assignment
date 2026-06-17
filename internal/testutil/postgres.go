package testutil

import (
	"context"
	"fmt"
	"testing"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
)

type TestDB struct {
	Postgres *embeddedpostgres.EmbeddedPostgres
	Pool     *pgxpool.Pool
	URL      string
}

func StartPostgres(t *testing.T) *TestDB {
	t.Helper()

	port := uint32(15432 + time.Now().UnixNano()%1000)
	dbName := "wallet_transfer_test"
	user := "wallet"
	password := "wallet"

	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().
		Username(user).
		Password(password).
		Database(dbName).
		Version(embeddedpostgres.V16).
		Port(port).
		StartTimeout(45 * time.Second))

	if err := pg.Start(); err != nil {
		t.Fatalf("start embedded postgres: %v", err)
	}

	url := fmt.Sprintf("postgres://%s:%s@localhost:%d/%s?sslmode=disable", user, password, port, dbName)
	ctx := context.Background()

	if err := postgres.RunMigrations(ctx, url); err != nil {
		_ = pg.Stop()
		t.Fatalf("run migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		_ = pg.Stop()
		t.Fatalf("create pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		if err := pg.Stop(); err != nil {
			t.Logf("stop embedded postgres: %v", err)
		}
	})

	return &TestDB{
		Postgres: pg,
		Pool:     pool,
		URL:      url,
	}
}

func SeedWallet(t *testing.T, pool *pgxpool.Pool, walletID string, balance int64) {
	t.Helper()

	_, err := pool.Exec(context.Background(), `
		INSERT INTO wallets (id, balance)
		VALUES ($1, $2)
		ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance
	`, walletID, balance)
	if err != nil {
		t.Fatalf("seed wallet %s: %v", walletID, err)
	}
}
