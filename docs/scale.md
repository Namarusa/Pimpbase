# Running Bubibase at scale

Bubibase stays one binary. `bubibase serve` with no flags runs everything in
one process: embedded SQLite, the embedded builder, in-process flow
schedules and files kept inside each app's database. Nothing else needs
installing. Everything on this page past "Big tables" is opt-in.

| You want | Run |
|---|---|
| One server, the simplest setup | `bubibase serve` (SQLite in `./data`) |
| Nightly backups on that server | add `--backup-dir /backups` |
| A container | `docker compose up -d` (the same binary, on SQLite) |
| Several servers behind a load balancer | Postgres + shared file storage: `deploy/compose.cluster.yml` |

The release binary is 34 MB stripped (48 MB with debug symbols), statically
linked, with no cgo. It starts with an empty environment.

## Big tables

Charts, stats, table pages, filters and row rules all run in the database.

- **Aggregates on the server.** `GET /api/apps/{app}/tables/{table}/aggregate`
  groups and reduces rows in SQL: `count`, `sum`, `avg`, `min`, `max`, grouped
  by a field, by day, month or year for dates, ordered by key or by value. The
  Chart and Stat components use it whenever they read their own table, so a
  chart covers every row instead of the first 1,000 the browser loaded. Charts
  over an API query, an external database table or a surrounding data
  provider still group in the browser (over the rows they loaded).
- **Roles and row rules apply to aggregates** exactly as to row lists: the
  person's row rule is added to the SQL `WHERE`, and links to rows they cannot
  read are shown as "Hidden row". Tests compare every SQL aggregate with the
  same computation in Go, under row rules, on SQLite and Postgres.
- **Indexes.** Any stored field (text, number, yes/no, date, single select,
  auto number, static formula) can be marked *Indexed* in the field editor or
  with `"indexed": true`. Single links are indexed automatically. Indexed text
  is capped at 2,000 characters per value.
- **Paging.** Sorting by an indexed field reads pages from the index; sorting
  by row id or the timestamps never sorts the whole table.
- **Counts and charts are cached** when they took 40 ms or more, for up to
  10 seconds, and forgotten as soon as a row of the table changes on this
  server. While one request is working out a slow answer, others asking the
  same get the previous answer instead of starting the same work again.
- **Filters that cannot become SQL** (some text functions on Postgres, for
  instance) are worked out in Go, on tables of up to 100,000 matching rows;
  beyond that the request is refused with a message, rather than loading a
  million rows into memory. The builder's search box only builds filters that
  run in SQL on big tables.
- **SQLite readers do not wait for writers.** Each SQLite database has one
  write connection and a pool of read-only connections (WAL mode).

### Measured on one million rows

4 vCPU, 15 GB RAM, one request at a time, cold cache, best of three. The table
has a text owner, a single select stage, a number amount and a date.

| Request | SQLite | SQLite, indexed | Postgres | Postgres, indexed |
|---|---:|---:|---:|---:|
| First page of 50 | 35 ms | 5 ms | 29 ms | 28 ms |
| Page 10,000 (offset 500,000) | 62 ms | 30 ms | 74 ms | 64 ms |
| Sort by amount | 299 ms | 21 ms | 148 ms | 67 ms |
| Filter owner = … (1,005 rows) | 243 ms | 0.4 ms | 50 ms | 12 ms |
| Row rule, owner's first page | 134 ms | 0.5 ms | 50 ms | 7 ms |
| Search two text fields | 1.2 s | 1.1 s | 344 ms | 326 ms |
| Stat: sum of amount | 158 ms | 98 ms | 44 ms | 43 ms |
| Chart: sum by stage | 736 ms | 663 ms | 86 ms | 70 ms |
| Chart: count by month | 725 ms | 763 ms | 91 ms | 82 ms |
| Same chart again within 10 s | 0 ms | 0 ms | 0.1 ms | 0.1 ms |

Reproduce with `BUBIBASE_SCALE_ROWS=1000000 go test ./internal/tables -run TestScale -v`.

Read it this way: on SQLite, index the fields people filter, sort and write
row rules on, and a million rows page and filter in milliseconds. Charts over
a whole million-row table take most of a second on SQLite, and under 100 ms
on Postgres; repeated views are served from the cache.

## Load tests

`cmd/bubibase-load` creates an app with a seeded table and sends a mix of
requests from many clients at once: 35% row pages, 20% filtered pages, 15%
single rows, 10% charts, 12% inserts, 8% updates. Run the servers with
`--rate-limit 0`, or the limit is what gets measured.

```sh
go run ./cmd/bubibase-load -url http://localhost:8080 -email you@example.com -password '…' -rows 100000 -c 32 -d 30s
```

Results on one 4 vCPU machine, 100,000 rows, 32 clients for 30 seconds. The
load generator, the servers, Postgres and the load balancer all shared the
same four cores, so these are floors, not ceilings.

| Setup | Requests/s | p50 | p95 | p99 | Errors |
|---|---:|---:|---:|---:|---:|
| One server, SQLite | 2,557 | 6 ms | 49 ms | 108 ms | 0 |
| One server, Postgres (native) | 788 | 29 ms | 112 ms | 177 ms | 0 |
| Docker: Caddy, 1 server, Postgres | 454 | 35 ms | 245 ms | 331 ms | 0 |
| Docker: Caddy, 2 servers, Postgres | 443 | 32 ms | 274 ms | 381 ms | 0 |

Per request type on SQLite: row pages p50 7 ms, filtered pages 5 ms, single
rows 3 ms, charts 2 ms, inserts and updates 25 ms (writes go one at a time
through SQLite's single writer, and each waits for the disk).

What this says, honestly:

- **SQLite is the fastest setup on one machine**, about three times Postgres
  here. Keep it unless you need more than one server.
- **A second server did not add throughput on this machine**, because
  Postgres was the bottleneck and every container shared the same four cores.
  The two-server run shows the cluster works under load (no errors, and the
  traffic split about evenly between the two servers), not that it is faster. Adding servers helps when the Bubibase processes, not the
  database, are what is busy: many people, heavy flows, many API queries.
- The first SQLite run, before the read pool, reached only 133 requests/s:
  every read waited behind writes on SQLite's single connection.

## Several servers

Point every server at the same Postgres database and the same file storage:

```sh
bubibase serve --postgres "postgres://…" --files dir:/shared/files --behind-proxy --drain 10s
```

- **Scheduled flows run once.** Each server claims a minute of a schedule in
  the database (`job_claims`) before running it, so with ten servers a flow
  scheduled for 9:00 runs once. Servers register in `instances` with a
  heartbeat every 10 seconds; flow runs left running by a server that stopped
  answering for 45 seconds are marked failed.
- **Migrations and schema changes take Postgres advisory locks**, so servers
  starting together, or two builders editing one table on two servers, do not
  collide.
- **File storage** is one of `db` (the default: inside each app's database),
  `dir:/path` (a folder, such as an NFS or EFS mount all servers share), or
  `s3://bucket/prefix?region=…&endpoint=…&path_style=true` for AWS S3, MinIO,
  Cloudflare R2, Backblaze B2 and other S3-compatible stores, with keys in
  `AWS_ACCESS_KEY_ID` and `AWS_SECRET_ACCESS_KEY`. Each file remembers where
  it was stored, so changing the setting later keeps older files readable.
  Files in the database already work across servers on Postgres.
- **Health.** `/healthz` answers while the process runs; `/readyz` also checks
  the database, and fails during the `--drain` time at shutdown so the load
  balancer stops sending requests before the server closes.
- SQLite is for one server. A second server started on the same data folder
  logs a warning.

What is per server, not shared: the rate limit, app request quotas (each
server counts its own requests), caches of counts (other servers may show a
count up to 10 seconds old) and of row-trigger flows (edits reach other
servers within 5 seconds). A Delay step waits in the memory of the server
running it; if that server stops, the run is marked failed.

## Deployment

- `Dockerfile` builds the same static binary on a distroless base (52 MB
  image, runs as non-root, healthcheck with `bubibase healthcheck`).
- `docker-compose.yml`: one server on SQLite with daily backups in a volume.
- `deploy/compose.cluster.yml`: Postgres, two Bubibase servers sharing a files
  volume, and Caddy balancing between them on `/readyz`. Scale with
  `--scale bubibase=4`. Both files were built and run for this phase; the
  cluster numbers above come from it.

Every flag also reads an environment variable (`BUBIBASE_POSTGRES`,
`BUBIBASE_FILES`, `BUBIBASE_RATE_LIMIT`, …); `bubibase serve -h` lists them.

## Backups

On SQLite:

```sh
bubibase serve --backup-dir /backups --backup-every 24h --backup-keep 7
bubibase backup --data ./data --out backup.zip     # any time, even while serving
bubibase restore --data ./new-data backup.zip      # into an empty folder, server stopped
```

Backups copy every database with `VACUUM INTO`, a consistent snapshot taken
without stopping writers. Admins can also download one from **Server**. Files
kept in a folder or S3 are not in the zip; back those up with the folder or
bucket. On Postgres, use `pg_dump` or your provider's snapshots.

## Metrics

`/metrics` serves Prometheus text: requests by route and status class,
request durations, rate-limited requests, quota refusals, flow runs by result
and their durations, database pool connections, server count and build
information. Protect it with `--metrics-token`, which then has to come as a
Bearer token.

## Rate limits and quotas

- **Rate limit**: each client (API key, signed-in user, or address) may make
  `--rate-limit` requests a second (100 by default) with bursts of
  `--rate-burst` (200) to each server; then `429` with `Retry-After`. Behind
  a load balancer, pass `--behind-proxy` so the client's address is read from
  `X-Forwarded-For`.
- **App quotas**, set by admins in an app's **Settings → Usage and limits**
  or `PUT /api/apps/{app}/quotas`: total rows, total file megabytes, requests
  a minute from people using the app (builders are not counted), and flow
  runs an hour. Going over a row or file quota answers `403`; going over a
  request or flow-run quota answers `429`. The row count behind the quota is
  refreshed every 30 seconds, so a burst of inserts can pass the limit by
  what it adds in that time.

## Moving an app: export and import

An app travels as one `.bubiapp` file (a zip): its tables, screens, roles,
API and database data sources and queries, external tables, variables,
flows and design, and optionally every row and file of its built-in tables.
Rows of external tables stay in their database.

- **Export** from **Settings → Move this app**, or
  `GET /api/apps/{app}/export?data=1`. Secret variable values, and passwords,
  tokens and connection strings written into data sources, are left blank unless an admin asks
  for them with `secrets=1`; `{{ env.X }}` bindings are kept, as they hold
  no secret.
- **Import as a new app** from **Apps → Import app…**, or
  `POST /api/apps/import` with the file as the body or a multipart `file`.
  Ids are kept, so a production server built from a development export can
  later be updated from newer exports. Importing next to the original, where
  the ids are taken, gives every table, field, screen and flow new ids. Flows
  arrive switched off unless asked otherwise, so a copy never sends a
  schedule's emails twice.
- **Update an app in place** from **Settings → Update from a file…**, or
  `POST /api/apps/{app}/import`. Things are matched by id, then by name
  (screens by route). The builder shows the plan first (`dryRun=1`): fields
  and tables to add, change or keep, screens, queries, flows and variables.
  The app keeps its rows, members, variable values, secrets, data source
  passwords, and flows' on/off state and webhook addresses. Tables, fields,
  queries, data sources and flows missing from the file are kept unless
  `prune=1`. A field whose type cannot be converted safely is left as it is
  and listed in the plan.

## Limits worth knowing

- Deep pages (`offset` in the hundreds of thousands) cost more than early
  ones; the data studio and table components page from the start.
- Search on SQLite scans text: about 1.1 s per million rows.
- Postgres needs a UTF-8 database ctype for `LOWER` and `UPPER` to match the
  Go behaviour on non-ASCII text (the cluster compose file sets it).
- A long text value produced by a static formula can still exceed the
  indexed-text cap on Postgres and fail that save.
- Importing rows does not count against row quotas.
