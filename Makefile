DB ?= postgres://drill:drill@localhost:5432/drill?sslmode=disable

.PHONY: build test test-all sandbox-image tests run-local db

build:
	go build -o bin/drill ./cmd/drill

test:
	go test ./...

# needs Docker and a throwaway Postgres database
test-all: sandbox-image
	DRILL_TEST_DOCKER=1 DRILL_TEST_DB=$(DB) go test -count=1 ./...

sandbox-image:
	docker build -t drill-go:1 -f sandbox/go.Dockerfile sandbox

# regenerate tasks/*/tests.json from tools/gentests (spoilers inside)
tests:
	go run ./tools/gentests

db:
	docker compose up -d db

run-local: build
	DRILL_DB=$(DB) ./bin/drill -sandbox docker
