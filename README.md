# Wallet Transfer Service

Go implementation of the wallet transfer coding assignment: idempotent transfers, double-entry ledger, stored balances, and concurrency-safe debits using PostgreSQL.

## Architecture

```text
cmd/server          HTTP entrypoint
internal/handler    Request validation and transport mapping
internal/service    Transfer workflow and idempotency orchestration
internal/repository PostgreSQL persistence
internal/domain     Entities, validation, and state rules
```

See [`docs/design.md`](docs/design.md) for API contract, failure modes, and consistency strategy.

## Prerequisites

- Go 1.24+
- Docker (for local PostgreSQL)
- `golangci-lint` (optional, for linting)

## Quick Start

```bash
# Start PostgreSQL
docker compose up -d

# Seed sample wallets
make seed

# Run the API server
make run
```

Server listens on `:8080` by default.

Environment variables:

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_URL` | `postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable` | PostgreSQL connection string |
| `HTTP_ADDR` | `:8080` | HTTP listen address |

## API

### POST /transfers

```bash
curl -s -X POST http://localhost:8080/transfers \
  -H 'Content-Type: application/json' \
  -d '{
    "idempotencyKey": "abc123",
    "fromWalletId": "wallet_1",
    "toWalletId": "wallet_2",
    "amount": 100
  }'
```

### GET /wallets/{walletId}/balance

```bash
curl -s http://localhost:8080/wallets/wallet_1/balance
```

## Testing

```bash
make test
```

Integration tests spin up embedded PostgreSQL automatically and cover:

- successful transfers and ledger balancing
- idempotent replay
- insufficient funds (`FAILED` transfer, unchanged balance)
- idempotency key conflict on different payload
- concurrent debits without overdraw

## Design Highlights

### Schema

- `wallets` — stored balance with non-negative constraint
- `transfers` — state machine (`PENDING` → `PROCESSED` / `FAILED`), unique `idempotency_key`
- `ledger_entries` — exactly one DEBIT and one CREDIT per transfer
- `idempotency_records` — durable request/response cache

### Idempotency

1. Insert idempotency row (`PROCESSING`) or wait on existing row (`FOR UPDATE`).
2. Reject same key with different payload (409).
3. On completion, persist HTTP status + JSON body for safe replay.

### Concurrency

- Single transaction per transfer.
- Wallets locked in ascending ID order (`SELECT … FOR UPDATE`) to prevent deadlocks and double spend.
- Balance update uses `balance + delta >= 0` guard at the database level.

## Assignment Submission

Branch: `solution/radas2502`

Open a PR into `main` with schema, idempotency, and concurrency notes filled in using `.github/pull_request_template.md`.

AI usage disclosure: [`docs/ai-usage.md`](docs/ai-usage.md)
