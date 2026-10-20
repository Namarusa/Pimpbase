# Bubibase

Bubibase is a self-hosted low-code platform in the spirit of Budibase: one Go
binary hosts many apps, each with its own data, and a browser builder to
design them. This repository holds phase 1, the **data core** (built-in
tables, the formula engine, the REST API and the data studio), phase 2,
the **UI builder**: screens composed from components and published as apps,
phase 3, **flows**: automations started by row changes, schedules,
webhooks or buttons, phase 4, **access**: app roles, row rules, single
sign-on and API keys, and phase 5's **API studio**: REST and SOAP APIs as
data sources whose query results feed screens.

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
`BUBIBASE_SECURE_COOKIES` (set it to `true` behind HTTPS), `BUBIBASE_SMTP`
and `BUBIBASE_MAIL_FROM` for the flows' Send email step:

```sh
./bubibase serve --smtp "smtp://user:pass@mail.example:587" --mail-from "Orders <orders@example.com>"
```

`smtp://` upgrades with STARTTLS when the server offers it, `smtps://` uses
TLS from the start, and `--smtp log` only writes the mails to the log.

## What works

- **Apps**: create, rename, delete; each app keeps its data in its own
  database file or schema.
- **Tables** with text, long text, number, yes/no, date, single select, multi
  select, link to another table (one or many), auto number, formula and
  attachment (files such as PDFs) fields. Renaming a field rewrites the
  formulas that use it; converting a field's type converts its data.
- **Formulas**: one Excel-style language (`IF([Qty] > 2, "bulk", "single")`)
  with 45 functions, type checking with error positions, dynamic (computed
  on read) or static (stored) formula fields. Filters are formulas too, and
  are compiled to SQL when possible, so they run in the database; the rest
  run in Go with the same results.
- **Data studio**: grid with inline editing, sorting, formula filters,
  paging, row forms with link pickers and file uploads, CSV import and
  export.
- **Files**: attachment fields hold up to 20 files of at most 25 MB each.
  Files are stored in the app's own database (SQLite or Postgres), so they
  are backed up and deleted with the app's data. Reading a file needs read
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
  Set `--public-url` (`BUBIBASE_PUBLIC_URL`) when Bubibase runs behind a
  proxy, so the redirect URI is right.
- **API keys** (`Authorization: Bearer bbk_…`) act as their owner, or only
  in one app with one role, and can expire. Keys limited to an app cannot
  reach builder or admin endpoints.

`bubibase dev-idp` runs a test identity provider on `127.0.0.1:9000`
(client `bubibase-dev`, secret `dev-secret`) with a few demo people and
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
`{{ state.status }}`. Buttons, forms and table rows run action lists:
navigate, open a URL, save or delete a row, open or close a dialog, show a
toast, refresh data, set state, ask for confirmation, export CSV, print or
save as PDF, sign out. Printing leaves out the app's navigation and any
component marked "Hide when printing", and prints dark themes light.

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
as `{{ state.category }}` or `{{ url.id }}`; the **Run API query** action
calls a query from a button or form and gives its answer to later actions as
`{{ result }}` and, if named, to `{{ state.<name> }}`.

```
GET    /api/apps/{app}/datasources                 POST, PATCH /{ds}, DELETE /{ds}
GET    /api/apps/{app}/queries                     POST, GET /{query}, PATCH, DELETE
POST   /api/apps/{app}/queries/test                {"query": {…unsaved}, "env"}   builders
POST   /api/apps/{app}/queries/{query}/run         {"params": {…}}                app users
GET    /api/apps/{app}/variables                   PUT /{name} {"secret", "development", "production"}, DELETE /{name}
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
  set variable, send email (with attached files), HTTP request, generate
  PDF and return result. A step can be switched off, or let the flow go on
  when it fails.
- **Bindings**: every input is a `{{ }}` template that reads
  `{{ trigger.row.Amount }}`, `{{ steps.Customer.first.Email }}`,
  `{{ vars.total }}`, `{{ env.API_KEY }}`, `{{ loop.item }}`, `{{ user }}`
  and `{{ app.name }}`, with pipes and `{{ = formula }}`. Templates also
  know `{{#each list}}…{{/each}}` and `{{#if x}}…{{else}}…{{/if}}`.
- **Generate PDF** writes Markdown (headings, bold, lists, tables, page
  breaks) into an A4 or Letter PDF in the app's accent colour, stores it
  with a table's files and can attach it to a row's attachment field.
- **Runs**: each run keeps its trigger data and, per step, the inputs, the
  result, the time taken and the error; the 200 latest runs of a flow are
  kept. Secret variables are hidden in the log. Test runs use the
  development values of variables and the newest row as sample data.

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
that calls the Anthropic API:

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

The API key belongs to the server. Start it with `--anthropic-key` (or
`BUBIBASE_ANTHROPIC_KEY` or `ANTHROPIC_API_KEY`), or let a workspace admin
save one in **Users → AI assistant**, where the model is chosen too
(`claude-opus-5-5` by default). The key is never sent to the browser, only a
hint such as `sk-ant-…a1b2`, and published apps cannot reach the assistant:
every assistant endpoint needs a builder. Without a key, the assistant says
so and points admins to the setting. What is sent to Anthropic is the app's
structure (table and field names, screens, queries, flows, role names,
variable names but never their values) and, for questions, the rows the
tools read.

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
```

An attachment field takes the list of uploaded files as returned by the
upload, e.g. `{"Documents": [{"id": "fil_…", "name": "contract.pdf", …}]}`.

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
internal/rbac/     roles, inheritance and the request actor
internal/sso/      OpenID Connect sign-in and role mappings; ssotest is a fake provider
internal/screens/  app screens and their definitions
internal/files/    files of attachment fields, stored in the app's database
internal/interp/   {{ binding }} templates for API queries, escaped per context
internal/xmltree/  XML to JSON-like data and back
internal/queries/  API data sources, queries, variables and the query runner
internal/flows/    flows, the engine, cron, email, PDF documents
internal/ai/       the assistant: settings, Anthropic API calls, drafts, formulas, questions
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
