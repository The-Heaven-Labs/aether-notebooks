.PHONY: dev build test migrate up down

up:
	docker compose up -d

down:
	docker compose down

build:
	go build -o bin/aether-server ./cmd/aether-server
	go build -o bin/aether ./cmd/aether

dev:
	go run ./cmd/aether-server

test:
	go test ./... -v

migrate:
	go run ./cmd/aether-server migrate
