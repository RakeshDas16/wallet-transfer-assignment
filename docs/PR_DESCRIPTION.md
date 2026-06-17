## Summary

Implements a Go wallet transfer service with PostgreSQL, covering the core assignment requirements:

- `POST /transfers` with idempotent, atomic wallet-to-wallet transfers
- Double-entry ledger (DEBIT + CREDIT per transfer)
- Transfer state machine (`PENDING` → `PROCESSED` / `FAILED`)
- Stored wallet balances with concurrency-safe debits
- Optional `GET /wallets/{walletId}/balance`

## AI disclosure

See [`docs/ai-usage.md`](docs/ai-usage.md).

1. **Tool:** Cursor (Claude-based agent)
2. **Usage:** Pair-programming for design doc, scaffolding, implementation, tests, and documentation. Human review applied before submission.
3. **Transcript:** Prompt summary included in `docs/ai-usage.md`; export full Cursor chat history from this workspace session for the complete transcript.

## Schema Design

| Table | Purpose |
|-------|---------|
| `wallets` | Stored balance with `CHECK (balance >= 0)` |
| `transfers` | Transfer metadata, status, unique `idempotency_key` |
| `ledger_entries` | Double-entry audit trail; unique `(transfer_id, wallet_id, entry_type)` |
| `idempotency_records` | Durable idempotency cache with request hash and serialized response |

Indexes on wallet/transfer foreign keys and ledger lookup columns.

## Idempotency Strategy

1. Insert idempotency row (`PROCESSING`) or block on existing row via `SELECT … FOR UPDATE`.
2. Hash request payload (`fromWalletId|toWalletId|amount`); reject same key with different payload (409).
3. Execute transfer inside the same transaction.
4. Persist HTTP status + JSON body when complete; replays return cached response with no duplicate side effects.

## Concurrency Strategy

- One PostgreSQL transaction per transfer.
- Wallets locked in ascending ID order (`SELECT … FOR UPDATE`) to reduce deadlocks.
- Balance update guarded by `balance + delta >= 0` at the database level to prevent overdraw.
- Integration test fires 10 concurrent debits against a wallet with balance for only one transfer.

## How to Run

```bash
docker compose up -d
make seed
make run
```

## How to Test

```bash
make test
```

## Tradeoffs / Assumptions

- Wallets must exist before transfer (seeded via SQL/Makefile).
- Stored balance is authoritative; ledger is the audit trail.
- Insufficient funds produce a `FAILED` transfer (422) rather than a hard error, and are idempotency-cached.
- Embedded PostgreSQL is used in tests (no Docker required for `make test`).
- Deadlock under extreme concurrency aborts the losing transaction; clients should retry with the same idempotency key.

## Checklist

- [x] Tests pass
- [ ] Lint passes (`make lint`)
- [ ] Format check passes (`make fmt-check`)
- [x] README or notes updated
- [x] PR description explains schema, idempotency, and concurrency
