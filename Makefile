.PHONY: fmt build test test-race cover lint vet bench up down demo integration-test

fmt:
	gofmt -l .
	@test -z "$$(gofmt -l .)" || (echo "gofmt found issues, run: gofmt -w ." && exit 1)

build:
	go build ./...

test:
	go test ./...

test-race:
	go test ./... -race

cover:
	go test ./pkg/... -coverprofile=coverage.out
	go tool cover -func=coverage.out

vet:
	go vet ./...

lint:
	@which golangci-lint > /dev/null || (echo "install golangci-lint: https://golangci-lint.run/welcome/install/" && exit 1)
	golangci-lint run ./...

bench:
	go test ./pkg/riskguard/... -run=^$$ -bench=. -benchmem

# Local stack: Postgres + Kafka + the server itself.
up:
	docker compose up -d --build

down:
	docker compose down -v

# Fire a sample transaction at the running demo server.
demo:
	curl -s -X POST http://localhost:8080/v1/transactions/evaluate \
		-H "Content-Type: application/json" \
		-d '{"entity_id":"alice","amount_minor":75000,"currency":"USD","ip":"1.2.3.4","device_id":"phone-1","country":"US","payment_method":"card"}' \
		| tee /dev/stderr | jq .

# Requires `make up` first: runs tests tagged "integration" against the
# Postgres/Kafka containers instead of in-memory fakes.
integration-test:
	DATABASE_URL="postgres://riskguard:riskguard@localhost:5432/riskguard?sslmode=disable" \
	KAFKA_BROKERS="localhost:29092" \
	go test ./test/integration/... -tags=integration -v
