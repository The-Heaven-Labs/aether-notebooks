.PHONY: dev build test migrate up down

up:
	docker compose up -d

down:
	docker compose down

build:
	go build -o bin/hnb-server ./cmd/hnb-server
	go build -o bin/hnb ./cmd/hnb

dev:
	go run ./cmd/hnb-server

test:
	go test ./... -v

migrate:
	go run ./cmd/hnb-server migrate
