.PHONY: build ui test test-pg run

# Build the builder UI, then the single binary that embeds it.
build: ui
	go build -o bubibase ./cmd/bubibase

ui:
	cd web/builder && npm ci && npm run build

test:
	go test ./...

# Runs the tests against Postgres too, e.g.
#   make test-pg PG="host=/tmp user=postgres dbname=postgres sslmode=disable"
test-pg:
	BUBIBASE_TEST_POSTGRES="$(PG)" go test ./...

run: build
	./bubibase serve
