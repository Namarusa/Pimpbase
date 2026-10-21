.PHONY: build ui test test-pg run

# Build the builder UI, then the single binary that embeds it.
build: ui
	go build -o pimpbase ./cmd/pimpbase

ui:
	cd web/builder && npm ci && npm run build

test:
	go test ./...

# Runs the tests against Postgres too, e.g.
#   make test-pg PG="host=/tmp user=postgres dbname=postgres sslmode=disable"
test-pg:
	PIMPBASE_TEST_POSTGRES="$(PG)" go test ./...

run: build
	./pimpbase serve
