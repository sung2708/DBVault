.PHONY: build test test-race integration-test e2e-test vet lint fmt fmt-check coverage bench docker-up docker-down clean test-install

EXE :=
ifeq ($(OS),Windows_NT)
EXE := .exe
endif
GOFLAGS ?=

build:
	go build $(GOFLAGS) -o bin/dbvault$(EXE) ./cmd/dbvault
test:
	go test ./...
test-race:
	go test -race ./...
integration-test:
	go test -v -tags=integration ./test/integration/...
e2e-test: integration-test
vet:
	go vet ./...
lint: fmt-check vet
fmt:
	gofmt -w cmd internal test
fmt-check:
	@test -z "$$(gofmt -l cmd internal test)"
coverage:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out
bench:
	go test -run '^$$' -bench . -benchmem ./internal/compression ./internal/pipeline ./internal/storage/local
docker-up:
	docker compose up -d --wait
docker-down:
	docker compose down
clean:
	go clean ./...
test-install:
	pwsh -NoProfile -ExecutionPolicy Bypass -File scripts/test-install.ps1
