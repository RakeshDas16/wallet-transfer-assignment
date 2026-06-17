# Wallet Transfer Service — Design

## Problem Statement

Implement a wallet-to-wallet transfer API with exactly-once semantics (when `idempotencyKey` is provided), double-entry ledger recording, stored balances, and safe concurrent execution.

## API Contract

### POST /transfers

**Request**

```json
{
  "idempotencyKey": "abc123",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

**Success (201 Created)**

```json
{
  "transferId": "uuid",
  "status": "PROCESSED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100
}
```

**Failure (422 Unprocessable Entity — insufficient funds)**

```json
{
  "transferId": "uuid",
  "status": "FAILED",
  "fromWalletId": "wallet_1",
  "toWalletId": "wallet_2",
  "amount": 100,
  "failureReason": "insufficient funds"
}
```

Duplicate requests with the same `idempotencyKey` and identical payload return the original HTTP status and body.

If the same `idempotencyKey` is reused with a different payload, the API returns **409 Conflict**.

### GET /wallets/{walletId}/balance (optional)

Returns `{ "walletId": "...", "balance": 1000 }`.

## Side Effects

Within a single database transaction:

1. Claim idempotency record (or return cached response).
2. Insert transfer row in `PENDING`.
3. Lock source and destination wallets (`SELECT … FOR UPDATE`, ordered by wallet ID to avoid deadlocks).
4. Validate sufficient balance.
5. Update stored balances (debit source, credit destination).
6. Insert two ledger entries (DEBIT + CREDIT).
7. Transition transfer to `PROCESSED` or `FAILED`.
8. Persist idempotency response for replay.

## Failure Modes

| Scenario | Behavior |
|----------|----------|
| Insufficient funds | Transfer → `FAILED`, idempotency cached, 422 returned |
| Unknown wallet | 404, no transfer persisted |
| Same wallet transfer | 400 validation error |
| Duplicate idempotency key (same payload) | Original response replayed |
| Duplicate idempotency key (different payload) | 409 Conflict |
| Concurrent debits on same wallet | Serialized via row locks; no double spend |

## Idempotency Behavior

- `idempotency_records.idempotency_key` is the primary key.
- First request inserts a row with status `PROCESSING` inside a transaction and holds `FOR UPDATE` lock.
- Concurrent duplicate requests block on the same row until the first transaction commits.
- On completion, status becomes `COMPLETED` with serialized response body and HTTP status code.
- Retries after commit always replay the stored response — no duplicate ledger entries.

## Retry Behavior

Clients may safely retry on network failure. The service is retry-safe because:

- Idempotency is durable in PostgreSQL.
- Transfer + ledger + balance updates share one transaction boundary.
- State transitions are guarded (`PENDING` → `PROCESSED` / `FAILED` only).

## Consistency Expectations

- **Strong consistency** within a transfer: balances, ledger, and transfer state commit atomically.
- Stored balance is the source of truth for authorization; ledger provides an audit trail.
- `ledger_entries` has a uniqueness constraint ensuring exactly one DEBIT and one CREDIT per transfer.

## Concurrency Strategy

- PostgreSQL transaction with `READ COMMITTED` (default).
- `SELECT … FOR UPDATE` on both wallets involved, always locked in ascending ID order to prevent deadlocks.
- Idempotency row locked first to serialize duplicate API calls.

## Observability

- Structured JSON logging via `slog` (request ID, transfer ID, idempotency key).
- HTTP middleware logs method, path, status, duration.

## Testing Strategy

- Integration tests against embedded PostgreSQL:
  - Successful transfer updates balances and creates balanced ledger entries.
  - Idempotent replay returns same result without double debit.
  - Insufficient funds marks transfer `FAILED` without changing balances.
  - Concurrent transfers from the same wallet never overdraw.
