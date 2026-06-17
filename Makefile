.PHONY: run test lint fmt migrate seed

DATABASE_URL ?= postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable

run:
	DATABASE_URL=$(DATABASE_URL) go run ./cmd/server

test:
	CGO_ENABLED=1 go test ./... -race -count=1

lint:
	golangci-lint run ./...

fmt:
	gofmt -w .

fmt-check:
	@test -z "$$(gofmt -l $$(go list -f '{{.Dir}}' ./...))"

migrate:
	@echo "Migrations run automatically on server startup"

seed:
	psql "$(DATABASE_URL)" -c "INSERT INTO wallets (id, balance) VALUES ('wallet_1', 1000), ('wallet_2', 500) ON CONFLICT (id) DO UPDATE SET balance = EXCLUDED.balance;"
