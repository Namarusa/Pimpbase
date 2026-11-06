# Pimpbase

Pimpbase is a self-hosted low-code platform in the spirit of Budibase: one Go
binary hosts many apps, each with its own data, and a browser builder to
design them. This repository holds phase 1, the **data core** (built-in
tables, the formula engine, the REST API and the data studio), phase 2,
the **UI builder**: screens composed from components and published as apps,
phase 3, **flows**: automations started by row changes, schedules,
webhooks or buttons, phase 4, **access**: app roles, row rules, single
sign-on and API keys, phase 5's **API studio**: REST and SOAP APIs as
data sources whose query results feed screens, **external databases**
(PostgreSQL, MySQL and MariaDB, SQL Server, MongoDB) as data sources and
tables, phase 6's **AI assistant**, and phase 7, **scale**: big tables,
several servers, deployment, quotas and moving apps between servers
([docs/scale.md](docs/scale.md)).

It stays one binary: `pimpbase serve` needs no database server, no Node, no
Docker and no outside service. Postgres, several servers, shared file
storage and containers are options for when you need them.

## Run it

```sh
go run ./cmd/pimpbase serve    # http://localhost:8080, needs Go 1.26+ only
```

The built UI in `web/builder/dist` is committed, so Go alone is enough. After
changing the UI, `make build` rebuilds it (needs Node 22+) and the binary.

The first visit asks for the owner account. Data lives in `./data`
(`pimpbase.db` for users and app definitions, `apps/<id>.db` per app).
To use Postgres instead, pass a connection string:

```sh
./pimpbase serve --postgres "postgres://user:pass@host/pimpbase"
```

Each app then gets its own Postgres schema. Every flag also reads an
environment variable: `PIMPBASE_ADDR`, `PIMPBASE_DATA`, `PIMPBASE_POSTGRES`,
`PIMPBASE_SECURE_COOKIES` (set it to `true` behind HTTPS), `PIMPBASE_SMTP`
and `PIMPBASE_MAIL_FROM`. Mail is usually set up by an admin in
**Users → Email** (SMTP, SendGrid, Mailgun, Postmark, Resend, or log only);
`--smtp` sets it on the server instead, and wins:

```sh
./pimpbase serve --smtp "smtp://user:pass@mail.example:587" --mail-from "Orders <orders@example.com>"
```

`smtp://` upgrades with STARTTLS when the server offers it, `smtps://` uses
TLS from the start, and `--smtp log` only writes the mails to the log.

### Release builds and Windows

```sh
make release VERSION=1.0.0           # dist/: Linux, macOS, Windows, amd64 and arm64
scripts/release.sh windows/amd64     # only the platforms named
```

No cgo is needed, so one machine builds every platform. Each archive holds
the binary and a short README; the Windows zip adds `start-pimpbase.bat`,
which keeps the data in a `data` folder next to it and opens the browser.
File paths, the SQLite file names and folder file storage (`--files
dir:C:\Pimpbase\files`) work with Windows paths, including spaces, and
Ctrl+C stops the server cleanly. Keep SQLite data on a local disk, not a
network share or a synced folder. Pimpbase does not install itself as a
Windows service; the Windows README explains Task Scheduler instead.

## Email templates

Each app has an **Emails** tab: templates with a subject, a preview line,
blocks (heading, text with bold, links and lists, button, image, a table
with a row per list item, divider, space, your own HTML) or a whole HTML
body, and a look (brand colour, font, logo, footer). A template declares
the fields it reads, such as `customer`, `order` and `lines`, with sample
values that fill the live preview, at desktop or phone width or as the
plain text sent alongside. Bound values are HTML-escaped, links only take
http, https, mailto and tel, and a block can have a Show when. Each save
keeps a version (the last 50); flows send the newest, and any version can
be restored. Templates travel with app export and import.

The flows' **Send email** step sends a template, filling each field from
the flow (`{{ trigger.row }}`, `{{ steps.Lines.rows }}`), or a message
written in the step. **Preview** shows the email with the flow's sample
data and **Send me a test** mails it to you only; a test run can only
preview its emails, and each run's log keeps the emails it sent.

The workspace's own emails (invitation, password reset, sign-in link) are
templates too, edited in **Users → Email**. With mail set up, admins
invite people by email, people reset a forgotten password, and, when the
admin turns it on, sign in with a link. Links work once and expire (7
days, 1 hour, 15 minutes).

## Integrations

Each app has an **Integrations** tab with a catalog of services: Stripe,
Slack, GitHub, Twilio SMS, Airtable, Google Sheets and Amazon S3 (or R2,
MinIO). A connection holds the service's keys, live ones and optional test
ones; secrets never go back to the browser and are hidden from run logs
(like secret app variables, they are stored in the database as they are).
**Test** checks the keys, and **Actions** tries any action by hand with
the test keys by default.

- **Flow steps**: every action of a connection is a step (Stripe checkout
  session, payment link, customer, invoice, refund, price, cancel a
  subscription, get or list objects; Slack messages; GitHub issues and
  workflow runs; Twilio SMS; Airtable records; Sheets ranges; S3 objects and
  expiring links). Test runs use the test keys, switched-on flows the live
  ones. Writes to Stripe carry an idempotency key, so a retried step never
  charges twice; rate limits (429) and, for safe requests, server errors are
  retried three times.
- **Service events**: a connection that receives webhooks (Stripe, Slack,
  GitHub, Twilio) has its own address. Events are checked with the
  service's signature scheme, recorded, handled once each even when sent
  again, and start the flows whose **Service event** trigger lists their
  type (`invoice.*` matches a family). A failed start answers 503 so the
  service retries. The **Events** tab lists the last 7 days, refused ones
  included.
- **Sync to tables**: Stripe customers, payments, subscriptions and
  invoices, GitHub issues and Airtable tables are copied into app tables,
  by hand or every 15 minutes to every day, and Stripe events update the
  rows at once. Rows the service no longer has are removed.
- **Payments**: **Make a payment flow** creates a flow that opens a Stripe
  Checkout page for a price fixed in the flow; a button with the **Take a
  payment** action runs it and sends the buyer there.
- **Your own**: `--integrations-dir` (`PIMPBASE_INTEGRATIONS_DIR`) loads more
  services from JSON manifests: credentials, auth (bearer, basic, header,
  query), actions, webhook verification and syncs.

The plain **Webhook** trigger can check signatures too: an HMAC of the body
in a header you name (sha256, sha1 or sha512, hex or base64, optional
prefix), or the GitHub, Stripe, Slack, Shopify and Standard Webhooks
(Svix, Resend) schemes, with the secret kept in a secret app variable.

Connections are not part of app export and import yet: after importing an
app, connect its services again.

## Deploy and scale

```sh
docker compose up -d                                        # one server, SQLite, daily backups
docker compose -f deploy/compose.cluster.yml up -d --build  # Postgres, 2 servers, Caddy
pimpbase backup --out backup.zip                            # SQLite backup while serving
```

### Docker

The image (`docker build -t pimpbase .`, about 75 MB) is the same static
binary on a distroless base. It runs as the non-root user 65532, listens on
8080, and checks itself with `pimpbase healthcheck` (`/readyz`).

- **Keep `/data` on a volume.** It holds the SQLite databases, and uploaded
  files too unless `PIMPBASE_FILES` sends them elsewhere. `/backups` is
  where `PIMPBASE_BACKUP_DIR` writes in `docker-compose.yml`.
- Named volumes need nothing more. A host folder must be writable by
  user 65532: `sudo chown -R 65532:65532 ./pimpbase-data`.
- Behind HTTPS set `PIMPBASE_PUBLIC_URL` (links in emails, OAuth and
  webhooks), `PIMPBASE_SECURE_COOKIES=true` and, when a proxy sets
  `X-Forwarded-For`, `PIMPBASE_BEHIND_PROXY=true`.
- Every `serve` flag has a `PIMPBASE_*` variable (`docker run pimpbase serve -h`
  lists them): `PIMPBASE_POSTGRES`, `PIMPBASE_FILES`, `PIMPBASE_SMTP`,
  `PIMPBASE_MAIL_FROM`, `PIMPBASE_METRICS_TOKEN`, `PIMPBASE_RATE_LIMIT`, AI
  keys such as `PIMPBASE_ANTHROPIC_KEY`, and so on.
- The first person to open the app creates the owner account, so finish
  setup before exposing a new server to the internet.
- Back up while running: `docker compose exec pimpbase pimpbase backup --out /backups/now.zip`.

- `/healthz`, `/readyz` (fails while draining) and Prometheus `/metrics`.
- Per-client rate limit (`--rate-limit`, 100 requests a second by default)
  and per-app quotas on rows, file megabytes, requests a minute and flow runs
  an hour (**Settings → Usage and limits**).
- Several servers on one Postgres run each scheduled flow once; uploaded
  files go to a shared folder or an S3-compatible bucket (`--files`).
- Apps move between servers as one `.bubiapp` file, as a new app or as an
  update of an existing one with a plan shown first.
- On one million rows with indexes, SQLite serves a first page in 5 ms and an
  indexed filter in under 1 ms; a whole-table chart takes 0.7 s on SQLite and
  under 0.1 s on Postgres. A mixed load of 32 clients reached about 2,500
  requests a second on SQLite on a 4 vCPU machine.

All of it, with the numbers, how they were measured and the limits, is in
[docs/scale.md](docs/scale.md).

## What works

- **Apps**: create, rename, delete; each app keeps its data in its own
  database file or schema.
- **Tables** with text, long text, number, yes/no, date, single select, multi
  select, link to another table (one or many), auto number, formula and
  attachment (files such as PDFs) fields. Renaming a field rewrites the
  formulas that use it; converting a field's type converts its data.
- **Formulas**: one Excel-style language (`IF([Qty] > 2, "bulk", "single")`)
  with 87 functions (logic, text, regular expressions, number and date
  formatting, working days, lists), type checking with error positions,
  dynamic (computed on read) or static (stored) formula fields. Filters are
  formulas too, and are compiled to SQL when possible, so they run in the
  database; the rest run in Go with the same results.
- **Linked rows in formulas**: `[Customer].[City]` reads a field of the
  linked row; over a many link it gives a list, so `SUM([Lines].[Amount])`
  or `JOIN([Lines].[Item], ", ")` work. Rollups add a condition:
  `SUMOF([Lines], [Qty] * [Price])`, `COUNTOF([Lines], NOT [Shipped])`,
  `AVERAGEOF`, `MINOF`, `MAXOF`. They compile to SQL subqueries, so
  filtering and sorting by a rollup run in the database. Formulas read one
  link deep and must be dynamic (a stored formula would go stale when the
  linked rows change). Renaming a field rewrites the formulas of other
  tables that read it, and changes that would break them are refused. A
  filter through a link needs the person to see every linked table without
  row rules; otherwise a builder adds a formula field instead.
- **Data studio**: grid with inline editing, sorting, formula filters,
  paging, row forms with link pickers and file uploads, CSV import and
  export.
- **Files**: attachment fields hold up to 20 files of at most 25 MB each.
  By default files are stored in the app's own database (SQLite or
  Postgres), so they are backed up and deleted with the app's data;
  `--files dir:/path` or `--files s3://bucket` keeps them outside. Reading a file needs read
  access to its table; PDFs and pictures open in the browser, other types
  are only downloaded.
- **Users and roles**: workspace roles (owner, admin, builder, member) and
  app roles with read, write and delete permissions per table plus row
  rules, checked on every request. See [Access](#access).
- **AI assistant**: drafts tables, screens, queries and flows for review,
  writes formulas, and answers questions about data within a person's
  access. See [AI assistant](#ai-assistant).

## Access

- **Workspace roles**: owners and admins manage people and sign-in;
  builders design every app; members only use the apps they hold a role
  in. Accounts can be switched off, which ends their sessions and keys.
- **App roles**: Public (anyone, no sign-in), Basic, Power and Admin, each
  holding the one below it, plus custom roles such as `SALES` made in an
  app's **Access** tab. A custom role inherits one other role; Admin holds
  every role. Deleting a role moves its members to the role it inherited.
- **Tables**: a role each for reading, writing and deleting rows, and row
  rules: a formula per role, such as `[Owner] = Current.User.Email` or
  `CONTAINS(Current.User.Groups, LOWER([Region]))`. A role uses the nearest
  rule in its chain; "every row" stops the inheritance. Rules run in the
  database when they compile to SQL and also guard creating, changing,
  deleting and importing rows. They also cover a table's files (a file
  downloads only for people who may see a row holding it; a file not yet
  saved to a row only for its uploader) and the labels other tables show
  of its rows (links to rows you may not see read "Hidden row").
- **Screens and components**: each screen needs a role to open, and any
  component can be limited to some roles (**Visible to**). The server
  removes hidden components before sending the screen, so they never reach
  the browser. Builders can preview an app as any role.
- **Flows and queries**: button flows and API queries need a role; a flow
  runs its steps either with full access or as the person who started it.
  Schedules and webhooks have nobody to act as, so such flows name the role
  their runs act as; without one they refuse to run rather than guess.
- **Published apps** show a sign-in page in the app's own colours, with
  password and single sign-on, and a "no access yet" page for people
  without a role.
- **Single sign-on** with OpenID Connect: Google, Microsoft Entra ID (one
  tenant or any) and any other provider, with PKCE, state and nonce
  checks. Role mappings turn a group, email domain, email or any ID token
  claim into a workspace role and app roles at every sign-in; roles given
  by hand always win. Accounts can be made on first sign-in, limited to
  some email domains, and linked to existing accounts by verified email.
  Set `--public-url` (`PIMPBASE_PUBLIC_URL`) when Pimpbase runs behind a
  proxy, so the redirect URI is right.
- **API keys** (`Authorization: Bearer bbk_…`) act as their owner, or only
  in one app with one role, and can expire. Keys limited to an app cannot
  reach builder or admin endpoints.

`pimpbase dev-idp` runs a test identity provider on `127.0.0.1:9000`
(client `pimpbase-dev`, secret `dev-secret`) with a few demo people and
groups, for trying single sign-on without a real provider.

Known limits: filters and sorting on a link field still compare the
linked rows' real labels, so a reader could probe a hidden label by
filtering on it; stored (static) formulas computed from link labels are
shown as stored.

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
none) are set in the designer's theme menu, as are the chart colours: a
preset palette (each with light and dark variants), shades of the accent, or
five custom colours. A chart can use another palette than the app's and set
the colour of single slices or series.

Components (42):

- **Layout**: container, grid, card, tabs, accordion, divider, spacer,
  dialog, side panel.
- **Content**: heading, text, badge, image, PDF viewer, link, button, stat,
  avatar, alert, progress, empty state. The PDF viewer draws pages with
  pdf.js (loaded only when a screen shows one), so it works the same on
  phones; it shows the PDFs of an attachment field or a web address, with
  paging, zoom, fit to width, download and a file switcher.
- **Data**: data provider, row provider, table (server paging, sorting and
  search, row selection, bulk delete, CSV export), list, cards, repeater,
  details, chart (bar, horizontal bar, line, area, pie, donut, grouped by a
  field or by day, week, month or year).
- **Forms**: form (create or update a row, column layout, server error
  mapping), text, long text, number, select, radio, checkbox group, date
  (with optional time), checkbox, switch, slider, link (searchable row
  picker) and file upload (drag and drop, PDF / pictures / any file). Fields validate required, min and max, length, pattern and email.

Any text setting accepts bindings such as `{{ row.Title }}`,
`{{ url.id }}`, `{{ form.Hours | number:1 }}`, `{{ user.name }}` or
`{{ state.status }}`, and formulas: `{{ = ROUND(row.Price * 1.2, 2) }}`.
Screen formulas use the language of table formulas and run in the browser
with the same results (both engines are tested against one set of
cases). They read `row.X` (or `[X]` for the current row), `form.X`,
`user.x`, `state.x`, `url.x`, `data.x`, `components.[Name].value`, `device` (`desktop`, `tablet` or
`phone`), and `row.[Unit price]` for names with spaces.

Every component has **Show when**, a formula such as
`form.Kind = "Business"`, and **Change settings when**: conditions that
set other settings while their formula is true, such as a red "Late"
badge or a disabled button (when several are true, the lower one wins).
The designer shows every component (conditional ones outlined) until you turn on
**Conditions** in its toolbar. Hidden components are not mounted, so they
load no data. These rules only change what the page shows; they cannot
change **Visible to**. Use Visible to, table permissions and row rules to
keep data from people, since those are enforced by the server.

**Components talk to each other.** Each field, form, table, list, card
grid, repeater, data or row provider and dialog has a name in the tree
("Text field 1" until you rename it), and every binding picker lists them
under **Components**: `{{ components["Search box"].value }}` is what is
typed or chosen in a field (in a form or not), `components.Orders.rows`,
`.total` and `.loading` are a table's or provider's rows, a table also
gives `.search`, `.selectedRows` and `.selectedIds`, a form `.values` and
`.valid`, a dialog `.open`. Renaming a component rewrites the bindings that
use it. A table's filter or an API query's parameters that read a
component or variable reload when it changes, so a search box in front of a
query needs no button. Formulas read them as `components.[Search box].value`.

**Screen variables** are declared in the screen settings with a first value
(bindings allowed, such as `{{ query.status }}`), read as `{{ state.name }}`
and changed with the **Update variable** action (set, reset or clear). The
screen settings also hold actions to run **when the screen opens**.

Buttons, forms, table and list rows, fields (**When the value changes**,
with the new value as `{{ value }}`; text fields also **When Enter is
pressed**) and screens run action lists. Data: save, duplicate, delete or
fetch a row, run an API query or a flow, refresh data, update a variable,
export CSV, clear a table's selection, take a payment. Screen: go to a
screen, open a web address, open or close a dialog or side panel, show a
notification, scroll to a component, copy to the clipboard, download a
file, print or save as PDF. Form: update or reset a field's value, focus a
field, clear a form, check a form. Logic: continue only if a test passes,
ask to confirm. App: sign out. Printing leaves out the app's navigation and any
component marked "Hide when printing", and prints dark themes light.

Every component also has a **Style** tab with the CSS a builder needs:
layout (flex or grid, direction, wrap, justify, align, gaps), grid
(columns, rows, areas, auto flow; `3` means three equal columns),
placement in its parent (align self, grow, order, grid column such as
`span 2`), size (width, height, min and max, aspect ratio), margin and
padding by side, position and layer, typography, background (colour,
gradient or picture address), border and radius, shadow (`sm` to `xl` or
CSS), opacity, transform, filter, transition, pointer, hide, and custom
CSS. Values are set for all devices, then overridden for tablets and
phones (phones also take the tablet values), and for hover, focus and
pressed states. Every value can hold a binding or a formula, such as
`{{ row.Colour }}`, `{{ = IF(device = "phone", 1, 3) }}` or
`{{ theme.brand }}`; a value that comes out empty is left unset, and
**Change settings when** can change style values too. Choices, numbers,
colours and switches in the Settings tab take bindings as well (the
braces button next to their label). Styles are stored in `props.style`
as `{ base, tablet, phone, hover, focus, active, css }`.

The **Theme** menu sets the corner roundness, the font and named
**tokens** (`brand = #0f766e`, `gutter = 20px`) that styles read as
`{{ theme.brand }}`; the theme's own colours are there too
(`{{ theme.primary }}`, `{{ theme.muted }}`, …) and follow light and dark.

To pass values from one screen to another, use the **Go to screen** action:
pick the target screen, fill a value for each `:part` of its route (`:id`
gets `{{ row._id }}`), and add extra values that go in the query string.
The target reads them as `{{ url.id }}` and `{{ query.customer }}`, for
example in a row provider's Row id or a table filter
(`[Customer] = "{{ query.customer }}"`; a filter line whose value is empty
is skipped, so the same screen opened from the menu shows every row). The
target's settings list the values it receives and which screens send them,
the binding picker offers them, and the designer's toolbar has a box for
each to preview the screen with a value.

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

## API studio

Open an app and choose **APIs**. A **data source** is a REST API: its
address, headers sent with every query, and sign-in (bearer token, user name
and password, or an API key in a header or the query string). A **query**
adds a method, a path, query string entries, headers and a body (JSON, form,
XML or text). **Send** runs it and shows the rows, the body as a tree (click
a list to use it as the rows, a name to copy its binding), the raw answer,
the headers and the request as it was sent.

Every text in a query can hold bindings, resolved on the server:

| Binding | Value |
| --- | --- |
| `{{ params.id }}` | a parameter the screen passes (text, number, boolean or JSON, with a default and a test value) |
| `{{ env.API_KEY }}` | an app variable; development values in the builder, production values in the published app |
| `{{ user.email }}` | the signed-in user: `id`, `email`, `name`, `role` |
| `{{ app.slug }}` | the app: `id`, `name`, `slug` |
| `{{ queries.Login.body.token }}` | another query's answer (`body`, `rows`, `headers`, `status`); that query runs first, once per run, and circles are refused |
| `{{ = CONCAT("Bearer ", env.TOKEN) }}` | a formula, with the functions of table formulas; `[id]` is `params.id` |

Pipes format values: `upper`, `lower`, `trim`, `default:"x"`, `number:2`,
`count`, `first`, `last`, `join:","`, `urlencode`, `base64`, `json`,
`xml:"Item"` and `raw`. Each value is escaped for where it lands: path parts
and query values are URL-encoded, headers lose line breaks, a JSON body gets
an escaped string inside quotes and a typed JSON value outside them
(`{"limit": {{ params.limit }}}` sends a number), an XML body gets escaped
text. `{{ params.ids | xml:"Id" }}` writes a list or object as XML elements;
`raw` skips the escaping. JSON and XML bodies are checked once filled, so a
broken body fails with a clear message instead of reaching the API.

Answers are read by their Content-Type (or as chosen). XML becomes the same
data JSON gives: an element is a key named after its local name, attributes
are `@name`, text next to children is `#text`, repeated elements are a list.
The **rows** are found at the result path (`data.items`,
`Envelope.Body.SearchResponse.Item`), or else at the first list; one XML
element at that path is still one row.

Variables marked secret are never sent to the browser and are hidden in the
echoed request. Each query has the role needed to run it from a published
app and an optional cache time (handy for sign-in queries other queries
read).

On screens, data providers, tables, lists, cards, charts and stats can use
an API query instead of a table, passing its parameters from bindings such
as `{{ components["Search box"].value }}`, `{{ state.category }}` or
`{{ url.id }}` (the query runs again when they change); the **Run API query** action
calls a query from a button or form and gives its answer to later actions as
`{{ result }}` and, if named, to `{{ state.<name> }}`.

```
GET    /api/apps/{app}/datasources                 POST, PATCH /{ds}, DELETE /{ds}
GET    /api/apps/{app}/queries                     POST, GET /{query}, PATCH, DELETE
POST   /api/apps/{app}/queries/test                {"query": {…unsaved}, "env"}   builders
POST   /api/apps/{app}/queries/{query}/run         {"params": {…}}                app users
GET    /api/apps/{app}/variables                   PUT /{name} {"secret", "development", "production"}, DELETE /{name}
```

## External databases

Pimpbase keeps its own data in SQLite (or Postgres with `--postgres`); that
does not change. Separately, an app can use databases you already run. In
**APIs**, a new data source can be PostgreSQL (and Supabase, Neon,
CockroachDB), MySQL or MariaDB, SQL Server (and Azure SQL) or MongoDB. Its
settings are a host, port, database, user and password, or one connection
string, plus TLS, a pool size, a timeout, driver options and a **read only**
switch. Every setting can hold bindings, so the password stays in a secret
variable (`{{ env.DB_PASSWORD }}`) and development and production can point
at different servers. **Test connection** tries either one.

**Queries** on a database are SQL, or for MongoDB an operation (`find`,
`findOne`, `aggregate`, `count`, `insertOne`, `insertMany`, `updateOne`,
`updateMany`, `deleteOne`, `deleteMany`) on a collection with a filter,
document and options in extended JSON. Bindings in SQL are always sent as
parameters, never written into the statement: write `WHERE id =
{{ params.id }}` without quotes, and a list fills `IN ({{ params.ids }})`
with one parameter per item. A binding inside quotes or a comment is
refused, and `| raw` is not available. The request tab shows the statement
and each parameter. A query reads at most 5,000 rows; statements that do
not read rows report how many they changed. Read-only data sources run
queries in a read-only transaction.

**Tables**: the data source lists its tables, views and collections, and
**Import** adds one to the app. It then appears in the data studio and on
screens like a built-in table, and its rows stay in your database. Column
types are mapped to field types (enums become single selects, binary
columns are hidden); MongoDB columns are read from a sample of 200
documents. Table permissions and row rules apply as for built-in tables,
and filters, search and sorting run in the database when the formula has a
SQL (or MongoDB) form, else in Pimpbase over at most 50,000 rows.
**Refresh columns** reads the schema again after it changes; the table's
settings hide columns or remove the table from the app (the database table
is never dropped).

**Keys**: a row's id is its primary-key value. Tables with a composite key
can be edited too; their row id is the key values as a JSON list, such as
`[12,2]`. Tables without a primary key are read only. For MongoDB the key is
always `_id`.

**Links**: single-column foreign keys are found on import and on refresh and
become link fields that show the linked row's label. The table settings can
turn any column into a link to another external table with a single-column
key, or to a built-in table (the column then holds the built-in row's
number). An optional field name adds a read-only list of the linking rows to
the other table, when it is external. Writes take the key, the label, or
`{"_id": …}`. Rows hidden from someone by permissions or row rules show as
hidden links.

**CSV**: **Import** adds rows one at a time with the same checks as the
form, and skips and lists those the database or a row rule refuses (it is
not all or nothing). **Export** writes the rows matching the filter, links
as labels.

**Flows** can trigger on rows created, changed or deleted through Pimpbase
and create, update, delete and find rows in external tables, with the same
permissions and row rules. **The assistant** knows external tables' columns
and links, can draft screens and flows on them, and answers questions over
them under the access of the person or role chosen.

Limits: changes made directly in the database don't trigger flows. Built-in
tables cannot link to external tables, `[Link].[Field]` formulas don't
reach into external tables, PDFs from flows are stored with built-in tables,
and the assistant does not draft new external tables. Text comparisons
follow the database's collation, so they ignore case on most MySQL and SQL
Server setups.

The drivers are pure Go and compiled in, so there is still one binary and
no database servers ship with it. They add about 8 MB (MongoDB about 5 MB,
SQL Server about 2 MB, MySQL under 1 MB); build tags leave them out:

```sh
go build -tags "nomongo nomssql nomysql" ./cmd/pimpbase   # PostgreSQL only, +0.4 MB
```

```
GET    /api/connectors                                  compiled-in kinds
POST   /api/apps/{app}/datasources/test                 {"kind", "config": {"db": {…}}, "env"}
GET    /api/apps/{app}/datasources/{ds}/schema          tables, views and collections
GET    /api/apps/{app}/datasources/{ds}/schema/table?schema=&name=
POST   /api/apps/{app}/datasources/{ds}/tables          {"schema", "source", "name"}   import
POST   /api/apps/{app}/tables/{table}/refresh           read the columns again
PATCH  /api/apps/{app}/tables/{table}                   {"links": [{"column", "table", "reverse"}], "hidden": […]}
```

## Flows

A flow is a trigger and a list of steps, edited on a canvas in the
**Flows** tab of an app.

- **Triggers**: row created, updated (optionally only when given fields
  change) or deleted in a table; a schedule (cron with five fields, or
  every few minutes, daily, weekly or monthly, in a chosen time zone); a
  webhook, a secret address other services POST JSON or forms to,
  optionally answered with the flow's result; and a button, run from
  screens with the **Run flow** action, with typed parameters and the
  lowest app role allowed to run it.
- **Steps**: create, update, delete and find rows, run an API studio query,
  condition (Yes and No branches), loop over a list, delay (up to an hour),
  set variable, send email (a template or a written message, with attached files), HTTP request, generate
  PDF and return result. A step can be switched off, or let the flow go on
  when it fails.
- **Bindings**: every input is a `{{ }}` template that reads
  `{{ trigger.row.Amount }}`, `{{ steps.Customer.first.Email }}`,
  `{{ vars.total }}`, `{{ env.API_KEY }}`, `{{ loop.item }}`, `{{ user }}`
  and `{{ app.name }}`, with pipes and `{{ = formula }}`. Templates also
  know `{{#each list}}…{{/each}}` and `{{#if x}}…{{else}}…{{/if}}`.
  Paths join keys with dots; list positions count from 0 and can be
  written `.0` or `[0]`, and keys with spaces or dots go in quoted
  brackets: `{{ steps.Api.rows[0]["first name"] }}`. Inside a Loop step
  or an `{{#each}}` block the current item is `{{ loop.item }}`, with
  `{{ loop.number }}` (from 1), `{{ loop.index }}` (from 0) and
  `{{ loop.count }}`; `{{#each}}` still accepts the short `{{ item }}`.
- **Sample data**: the binding picker is a tree of what each step gives,
  with the values from the flow's latest run, so an API step's answer and
  a loop's current item can be unfolded key by key, nested keys included.
  **Get sample data** on a query or find-rows step fills it before any
  run (the query is called with development values). Each binding shows a
  preview under its box, and paths the sample lacks are flagged with the
  keys that are there.
- **Generate PDF** writes Markdown (headings, bold, lists, tables, page
  breaks) into an A4 or Letter PDF in the app's accent colour, stores it
  with a table's files and can attach it to a row's attachment field.
- **AI prompt** asks a model from any provider set up in **Users → AI
  assistant** (or the workspace's default) and gives back text, any JSON,
  or named fields (`{{ steps.Ai.data.category }}`). The settings follow
  the provider: Anthropic takes an effort (low to max) and no temperature;
  OpenAI and Azure a reasoning effort, temperature, top P, seed and stop
  sequences; Gemini a thinking setting; xAI, Groq, Mistral and
  OpenAI-compatible servers temperature, top P and seed; DeepSeek has no
  JSON schemas, so fields are asked for in the instructions and checked.
  Fields are enforced with a JSON schema where the provider has one. The
  model is picked from the provider's live list (the deployment for Azure,
  with its API version). The result also has `text`, `model`, `provider`,
  `usage.input`, `usage.output` and `truncated`; one call may take up to
  3 minutes.
- **System One models** (TypeSafe's Jev, with the TypeSafe provider or
  through OpenRouter as `typesafe/jev-1.14` or `~typesafe/jev-latest`) do
  not take a prompt. The step then asks for a **state** (text, or a whole
  binding such as `{{ trigger.row }}`, sent as JSON) and **questions**:
  a *choice* among options, a *score* on 2 to 10 levels, or a *yes/no*
  (noul). Each is answered in parallel with probabilities:
  `{{ steps.Jev.data.team.choice }}`, `.confidence`, `.probabilities.billing`,
  `{{ steps.Jev.data.urgency.score }}` (the expected level from 0) and
  `{{ steps.Jev.data.angry.noul }}` (0 to 1). A condition on `.confidence`
  can hand unsure cases to a person or a larger model. TypeSafe has no chat
  models, so it serves flows only, not the assistant.
- **Runs**: each run keeps its trigger data and, per step, the inputs, the
  result, the time taken and the error; the 200 latest runs of a flow are
  kept. Secret variables are hidden in the log. Test runs use the
  development values of variables and the newest row as sample data.
  Large results are logged with long lists and texts cut short, keeping
  their shape for the sample data.

Row changes made by flows start other flows up to three levels deep, so
flows that update each other stop.

```
GET    /api/apps/{app}/flows                       POST, GET /{flow}, PATCH, DELETE
POST   /api/apps/{app}/flows/{flow}/test           {"trigger": {…sample}}          builders
POST   /api/apps/{app}/flows/{flow}/run            {"params": {…}}                 button flows, app users
POST   /api/apps/{app}/flows/{flow}/hook           a new webhook address
GET    /api/apps/{app}/flows/{flow}/runs           GET /api/apps/{app}/runs/{run}
POST   /api/hooks/{token}                          webhook, no login
GET    /api/flows/schedule?cron=&timezone=         the next times a schedule fires
```

## AI assistant

The builder has an assistant (the **Assistant** button next to an app's tabs)
that calls a model from the AI provider a workspace admin picks:

- **Drafts**: describe a table, screen, API query or flow and get a draft.
  The server checks it the same way it checks the builder's own saves and
  sends any problem back to the model, which corrects it. You then see a
  preview and a line diff against what exists, and nothing changes until
  you press **Apply**. On a screen, query or flow page the assistant can
  also change the open one.
- **Formula help**: the **AI** button next to formula fields, filters and
  row rules writes a formula from a description. It is compiled and type
  checked before you see it, and you choose whether to use it.
- **Ask about data**: questions about an app's rows, answered by reading
  them through two tools (list rows, and count/sum/average per group). The
  reads use the access of whoever you pick: you, a member, or a role, so
  table permissions, row rules and hidden links apply.

### Providers

| Provider | Key | Address | Model |
|---|---|---|---|
| Anthropic | `--anthropic-key`, `ANTHROPIC_API_KEY` | optional | `claude-opus-5-5` unless another is picked |
| OpenAI | `--openai-key`, `OPENAI_API_KEY` | optional | picked from the list, or `--ai-model` |
| Google Gemini | `--gemini-key`, `GEMINI_API_KEY`, `GOOGLE_API_KEY` | optional | picked from the list, or `--ai-model` |
| Azure OpenAI | `--azure-openai-key`, `AZURE_OPENAI_API_KEY` | `--azure-openai-endpoint`, `AZURE_OPENAI_ENDPOINT` | the deployment: `--azure-openai-deployment` |
| Mistral | `--mistral-key`, `MISTRAL_API_KEY` | optional | picked from the list, or `--ai-model` |
| xAI Grok | `--xai-key`, `XAI_API_KEY` | optional | picked from the list, or `--ai-model` |
| DeepSeek | `--deepseek-key`, `DEEPSEEK_API_KEY` | optional | picked from the list, or `--ai-model` |
| Groq | `--groq-key`, `GROQ_API_KEY` | optional | picked from the list, or `--ai-model` |
| OpenRouter | `--openrouter-key`, `OPENROUTER_API_KEY` | optional | picked from the list, or `--ai-model` |
| TypeSafe Jev (flows only) | `--typesafe-key`, `TYPESAFE_API_KEY` | optional | `jev-latest` unless another is picked |
| OpenAI-compatible | `--openai-compatible-key` (optional) | `--openai-compatible-url` | `--openai-compatible-model` |

Every flag also reads `PIMPBASE_<FLAG>` (e.g. `PIMPBASE_OPENAI_KEY`).
**OpenAI-compatible** is any server speaking the chat completions API with
tools: Ollama (`http://localhost:11434/v1`), LM Studio
(`http://localhost:1234/v1`), Jan (`http://localhost:1337/v1`), OpenRouter,
Together, vLLM and the like.
With a local model nothing leaves your machines. Azure uses API version
`2024-10-21` unless `--azure-openai-api-version` (or `OPENAI_API_VERSION`)
says otherwise; `v1` selects Azure's v1 API, where the deployment is sent as
the model.

```sh
./pimpbase serve --ai-provider openai_compatible \
  --openai-compatible-url http://localhost:11434/v1 --openai-compatible-model qwen3:8b
```

Admins set the same things in **Users → AI assistant**: pick a provider,
paste its key, change its address, pick a model, and **Test connection**,
which sends one small request that must call a tool. The model list comes
from the provider itself, read with the key and address on the page even
before they are saved: Anthropic's and Gemini's model lists, the `/models`
list of OpenAI, Mistral, xAI, DeepSeek, Groq and OpenAI-compatible servers (for Ollama, the
models you have pulled), and an Azure resource's deployments. Models that
cannot use tools are left out where the provider says so (Mistral,
OpenRouter, Gemini) or the name shows it (OpenAI embeddings, audio, images).
When the list cannot be read, the page says why and the name can be typed
instead; **Type another name…** does the same for a model not listed. Azure
lists deployments only on resources that still offer that call; elsewhere
type the deployment name from Azure AI Foundry. Only Anthropic has a model
used by default; the others are not set up until a model is picked. Settings given to the server
win and are shown as such; `--ai-provider` (`PIMPBASE_AI_PROVIDER`) fixes the
provider and `--ai-model` (`PIMPBASE_AI_MODEL`) its model. Without either, the
provider an admin picked is used, else the first one that is set up.

The provider applies to drafts, formula help and questions alike. All of
them work by tool calls (a draft or formula is proposed through a tool and
checked by the server; questions read rows through tools), so the model must
support tool or function calling. A model that answers in words where a
tool call was expected is reminded once. Rate limits and overloads are
retried twice.

Keys belong to the server. They are never sent to the browser, only a hint
such as `sk-ant-…a1b2`, and published apps cannot reach the assistant: every
assistant endpoint needs a builder, and its settings an admin. Without a
provider set up, the assistant says so and points admins to the setting.
What is sent to the provider is the app's structure (table and field names,
screens, queries, flows, role names, variable names but never their values)
and, for questions, the rows the tools read, with the access of whoever the
question is asked as.

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
POST   /api/apps/{app}/tables/{table}/files           multipart "file"; returns {"id", "name", "type", "size", "url"}
GET    /api/apps/{app}/files/{file}?download=1
GET    /api/apps/{app}/tables/{table}/aggregate?groupBy=Stage&values=sum:Amount&values=count&dateGroup=&sortBy=&filter=
```

Apps and the server:

```
GET    /api/apps/{app}/export?data=1&secrets=1        a .bubiapp file (secrets: admins only)
POST   /api/apps/import?name=&dryRun=1&enableFlows=1  .bubiapp body or multipart "file"; a new app
POST   /api/apps/{app}/import?dryRun=1&prune=1        update an app in place; answers the plan
GET    /api/apps/{app}/usage                          usage against the app's quotas
PUT    /api/apps/{app}/quotas                         {"maxRows", "maxFileMB", "requestsPerMinute", "flowRunsPerHour"}
GET    /api/admin/status                              version, storage, servers sharing the database
GET    /api/admin/backup                              a SQLite backup zip
GET    /healthz  /readyz  /metrics
```

An attachment field takes the list of uploaded files as returned by the
upload, e.g. `{"Documents": [{"id": "fil_…", "name": "contract.pdf", …}]}`.

`{app}` is the app's id or URL name. See `internal/api/server.go` for the
full list.

## Layout

```
cmd/pimpbase/      the binary
internal/store/    SQLite and Postgres behind one interface, migrations
internal/formula/  lexer, parser, type checker, evaluator, SQL compiler
internal/tables/   table schemas, DDL, rows, links, CSV
internal/auth/     users, Argon2id passwords, sessions, API keys
internal/apps/     apps and app members
internal/rbac/     roles, inheritance and the request actor
internal/sso/      OpenID Connect sign-in and role mappings; ssotest is a fake provider
internal/screens/  app screens and their definitions
internal/files/    files of attachment fields: in the app's database, a folder or S3
internal/interp/   {{ binding }} templates for API queries, escaped per context
internal/xmltree/  XML to JSON-like data and back
internal/queries/  API and database data sources, queries, variables and the query runner
internal/extdb/    PostgreSQL, MySQL, SQL Server and MongoDB drivers, schema, rows, SQL parameters
internal/exttables/ tables imported from external databases
internal/flows/    flows, the engine, cron, Send email, PDF documents, AI steps
internal/integrations/ service manifests (Stripe, Slack, GitHub…), connections, calls, webhook checks, syncs
internal/email/    mail delivery (SMTP, provider APIs, log), email templates, versions, system emails
internal/ai/       the assistant: providers (Anthropic, OpenAI, Gemini, Azure, Mistral, xAI, DeepSeek, Groq, OpenRouter, TypeSafe, OpenAI-compatible), drafts, formulas, questions, flow AI steps
internal/cluster/  servers sharing a database: heartbeats and job claims
internal/bundle/   .bubiapp export and import
internal/backup/   SQLite backups while serving
internal/metrics/  Prometheus metrics
internal/api/      HTTP API, rate limits, quotas, health
cmd/pimpbase-load/ load tests
deploy/            cluster compose file and Caddy config
web/builder/       Svelte builder, embedded from web/builder/dist
```

## Tests

```sh
go test ./...                                           # SQLite
PIMPBASE_TEST_POSTGRES="postgres://…" go test ./...     # SQLite and Postgres
```

External database tests run when their connection strings are set:
`PIMPBASE_TEST_EXT_POSTGRES`, `PIMPBASE_TEST_EXT_MYSQL`,
`PIMPBASE_TEST_EXT_MSSQL` and `PIMPBASE_TEST_EXT_MONGO`.

The formula tests include a differential test that runs every filter both
in Go and as SQL on each database and checks they select the same rows;
aggregates are checked the same way. `PIMPBASE_SCALE_ROWS=1000000 go test
./internal/tables -run TestScale -v` times reads on a million rows.

Pimpbase reimplements Budibase's ideas; it contains no Budibase code.
