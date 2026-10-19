# Bubibase

Bubibase is a self-hosted low-code platform in the spirit of Budibase: one Go
binary hosts many apps, each with its own data, and a browser builder to
design them. This repository holds phase 1, the **data core** (built-in
tables, the formula engine, the REST API and the data studio), and phase 2,
the **UI builder**: screens composed from components and published as apps.

## Run it

```sh
go run ./cmd/bubibase serve    # http://localhost:8080, needs Go 1.26+ only
```

The built UI in `web/builder/dist` is committed, so Go alone is enough. After
changing the UI, `make build` rebuilds it (needs Node 22+) and the binary.

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

## UI builder

Open an app and choose **Design**. Screens are trees of components you add
from the palette, arrange by drag and drop in the tree, and configure in the
settings panel; the canvas previews them live against your data, at desktop,
tablet or phone width. **New screen** can generate a list screen (table plus
an "add" dialog), a detail screen (tabs with details and an edit form) or a
dashboard from any table. Undo, redo and autosave are built in.

Published apps live at `/app/<app-url-name>/<screen route>`. Each screen has
a route (`/jobs/:id` passes `url.id`) and a minimum role; anonymous visitors
see Public screens only and are asked to sign in otherwise. The app's theme
(light, dark or system), accent colour and navigation (sidebar, top bar or
none) are set in the designer's theme menu.

Components (40):

- **Layout**: container, grid, card, tabs, accordion, divider, spacer,
  dialog, side panel.
- **Content**: heading, text, badge, image, link, button, stat, avatar,
  alert, progress, empty state.
- **Data**: data provider, row provider, table (server paging, sorting and
  search, row selection, bulk delete, CSV export), list, cards, repeater,
  details, chart (bar, horizontal bar, line, area, pie, donut, grouped by a
  field or by day, week, month or year).
- **Forms**: form (create or update a row, column layout, server error
  mapping), text, long text, number, select, radio, checkbox group, date
  (with optional time), checkbox, switch, slider and link (searchable row
  picker). Fields validate required, min and max, length, pattern and email.

Any text setting accepts bindings such as `{{ row.Title }}`,
`{{ url.id }}`, `{{ form.Hours | number:1 }}`, `{{ user.name }}` or
`{{ state.status }}`. Buttons, forms and table rows run action lists:
navigate, open a URL, save or delete a row, open or close a dialog, show a
toast, refresh data, set state, ask for confirmation, export CSV, sign out.

The UI uses [shadcn-svelte](https://shadcn-svelte.com) components (bits-ui,
Tailwind CSS 4) and LayerChart for charts. Known limits: charts and stats
group rows in the browser over at most 1,000 rows; there is no custom code
component yet.

Screens are stored as JSON in `app_screens`:

```
GET    /api/apps/{app}/screens
POST   /api/apps/{app}/screens                     {"name", "route", "role", "definition"}
PATCH  /api/apps/{app}/screens/{screen}
DELETE /api/apps/{app}/screens/{screen}
GET    /api/apps/{app}/runtime                     screens and tables the caller may use
```

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
internal/screens/  app screens and their definitions
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
