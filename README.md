# Bubibase

Bubibase is a self-hosted low-code platform in the spirit of Budibase: one Go
binary hosts many apps, each with its own data, and a browser builder to
design them. This repository holds phase 1, the **data core**: built-in
tables, the formula engine, the REST API and the data studio.

## Run it

```sh
make build          # builds the UI (needs Node 22+) and the binary (needs Go 1.26+)
./bubibase serve    # http://localhost:8080
```

The first visit asks for the owner account. Data lives in `./data`
(`bubibase.db` for users and app definitions, `apps/<id>.db` per app).
To use Postgres instead, pass a connection string:

```sh
./bubibase serve --postgres "postgres://user:pass@host/bubibase"
```

Each app then gets its own Postgres schema. Every flag also reads an
environment variable: `BUBIBASE_ADDR`, `BUBIBASE_DATA`, `BUBIBASE_POSTGRES`,
`BUBIBASE_SECURE_COOKIES` (set it to `true` behind HTTPS).

## What works

- **Apps**: create, rename, delete; each app keeps its data in its own
  database file or schema.
- **Tables** with text, long text, number, yes/no, date, single select, multi
  select, link to another table (one or many), auto number and formula
  fields. Renaming a field rewrites the formulas that use it; converting a
  field's type converts its data.
- **Formulas**: one Excel-style language (`IF([Qty] > 2, "bulk", "single")`)
  with 45 functions, type checking with error positions, dynamic (computed
  on read) or static (stored) formula fields. Filters are formulas too, and
  are compiled to SQL when possible, so they run in the database; the rest
  run in Go with the same results.
- **Data studio**: grid with inline editing, sorting, formula filters,
  paging, row forms with link pickers, CSV import and export.
- **Users and roles**: workspace roles (owner, admin, builder, member) and
  app roles (Public, Basic, Power, Admin) with read and write permissions per
  table, checked on every request. Sessions use HTTP-only cookies; machines
  use API keys (`Authorization: Bearer bbk_…`).

## REST API

All endpoints are under `/api`. Rows of a table:

```
GET    /api/apps/{app}/tables/{table}/rows?filter=&sort=&desc=&limit=&offset=
POST   /api/apps/{app}/tables/{table}/rows            {"Field name": value}
GET    /api/apps/{app}/tables/{table}/rows/{id}
PATCH  /api/apps/{app}/tables/{table}/rows/{id}
DELETE /api/apps/{app}/tables/{table}/rows/{id}
POST   /api/apps/{app}/tables/{table}/import          CSV body or multipart "file"
GET    /api/apps/{app}/tables/{table}/export?filter=
```

`{app}` is the app's id or URL name. See `internal/api/server.go` for the
full list.

## Layout

```
cmd/bubibase/      the binary
internal/store/    SQLite and Postgres behind one interface, migrations
internal/formula/  lexer, parser, type checker, evaluator, SQL compiler
internal/tables/   table schemas, DDL, rows, links, CSV
internal/auth/     users, Argon2id passwords, sessions, API keys
internal/apps/     apps and app members
internal/rbac/     roles and the request actor
internal/api/      HTTP API
web/builder/       Svelte builder, embedded from web/builder/dist
```

## Tests

```sh
go test ./...                                           # SQLite
BUBIBASE_TEST_POSTGRES="postgres://…" go test ./...     # SQLite and Postgres
```

The formula tests include a differential test that runs every filter both
in Go and as SQL on each database and checks they select the same rows.

Bubibase reimplements Budibase's ideas; it contains no Budibase code.
