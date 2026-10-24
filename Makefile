.PHONY: build ui test test-pg run release

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

# Release archives for Linux, macOS and Windows (amd64 and arm64) in dist/,
# e.g. make release VERSION=1.0.0, or scripts/release.sh windows/amd64.
release:
	VERSION=$(VERSION) scripts/release.sh
