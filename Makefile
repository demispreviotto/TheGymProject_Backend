.PHONY: run test lint tidy up down
run:   ; go run ./cmd/api
test:  ; go test ./... -race -cover
lint:  ; go vet ./...
tidy:  ; go mod tidy
up:    ; docker compose up --build
down:  ; docker compose down
