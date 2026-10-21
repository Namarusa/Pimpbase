package store

import (
	"context"
	"fmt"
)

// metaMigrations are applied in order; never edit one that has shipped.
var metaMigrations = []string{
	`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL,
		created_at TEXT NOT NULL
	);
	CREATE TABLE sessions (
		token_hash TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at TEXT NOT NULL
	);
	CREATE TABLE api_keys (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		key_hash TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL
	);
	CREATE TABLE apps (
		id TEXT PRIMARY KEY,
		slug TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE app_users (
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		role TEXT NOT NULL,
		PRIMARY KEY (app_id, user_id)
	);
	CREATE TABLE app_tables (
		id TEXT PRIMARY KEY,
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		definition TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (app_id, name)
	);`,
	// Phase 2: app screens and the app's design settings.
	`CREATE TABLE app_screens (
		id TEXT PRIMARY KEY,
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		route TEXT NOT NULL,
		role TEXT NOT NULL,
		position INTEGER NOT NULL,
		definition TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (app_id, route)
	);
	ALTER TABLE apps ADD COLUMN design TEXT NOT NULL DEFAULT '{}';`,
	// Phase 5: API data sources, queries and app variables.
	`CREATE TABLE app_datasources (
		id TEXT PRIMARY KEY,
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		kind TEXT NOT NULL,
		config TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (app_id, name)
	);
	CREATE TABLE app_queries (
		id TEXT PRIMARY KEY,
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		datasource_id TEXT NOT NULL REFERENCES app_datasources(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		definition TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (app_id, name)
	);
	CREATE TABLE app_variables (
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		secret INTEGER NOT NULL DEFAULT 0,
		dev_value TEXT NOT NULL,
		prod_value TEXT NOT NULL,
		PRIMARY KEY (app_id, name)
	);`,
	// Phase 3: flows (automations) and their runs.
	`CREATE TABLE app_flows (
		id TEXT PRIMARY KEY,
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 0,
		definition TEXT NOT NULL,
		hook_token TEXT NOT NULL UNIQUE,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (app_id, name)
	);
	CREATE TABLE app_flow_runs (
		id TEXT PRIMARY KEY,
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		flow_id TEXT NOT NULL REFERENCES app_flows(id) ON DELETE CASCADE,
		status TEXT NOT NULL,
		test INTEGER NOT NULL DEFAULT 0,
		trigger_kind TEXT NOT NULL,
		trigger_data TEXT NOT NULL,
		steps TEXT NOT NULL,
		output TEXT NOT NULL,
		error TEXT NOT NULL,
		started_at TEXT NOT NULL,
		finished_at TEXT NOT NULL
	);
	CREATE INDEX app_flow_runs_flow ON app_flow_runs (flow_id, started_at);`,
	// Phase 4: custom app roles, single sign-on, scoped API tokens.
	`CREATE TABLE app_roles (
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		id TEXT NOT NULL,
		name TEXT NOT NULL,
		description TEXT NOT NULL,
		inherits TEXT NOT NULL,
		color TEXT NOT NULL,
		position INTEGER NOT NULL,
		PRIMARY KEY (app_id, id)
	);
	ALTER TABLE app_users ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
	ALTER TABLE users ADD COLUMN disabled INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE users ADD COLUMN sso_groups TEXT NOT NULL DEFAULT '[]';
	ALTER TABLE users ADD COLUMN last_login_at TEXT NOT NULL DEFAULT '';
	ALTER TABLE api_keys ADD COLUMN app_id TEXT NOT NULL DEFAULT '';
	ALTER TABLE api_keys ADD COLUMN role TEXT NOT NULL DEFAULT '';
	ALTER TABLE api_keys ADD COLUMN expires_at TEXT NOT NULL DEFAULT '';
	ALTER TABLE api_keys ADD COLUMN last_used_at TEXT NOT NULL DEFAULT '';
	CREATE TABLE sso_providers (
		id TEXT PRIMARY KEY,
		kind TEXT NOT NULL,
		name TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		config TEXT NOT NULL,
		client_secret TEXT NOT NULL,
		position INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);
	CREATE TABLE user_identities (
		provider_id TEXT NOT NULL REFERENCES sso_providers(id) ON DELETE CASCADE,
		subject TEXT NOT NULL,
		user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		email TEXT NOT NULL,
		created_at TEXT NOT NULL,
		last_login_at TEXT NOT NULL,
		PRIMARY KEY (provider_id, subject)
	);
	CREATE TABLE sso_states (
		state_hash TEXT PRIMARY KEY,
		provider_id TEXT NOT NULL,
		nonce TEXT NOT NULL,
		verifier TEXT NOT NULL,
		return_to TEXT NOT NULL,
		expires_at TEXT NOT NULL
	);`,
	// Phase 6: workspace settings, such as the AI assistant's API key.
	`CREATE TABLE settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);`,
	// External database connectors: tables imported from a database
	// data source.
	`CREATE TABLE app_ext_tables (
		id TEXT PRIMARY KEY,
		app_id TEXT NOT NULL REFERENCES apps(id) ON DELETE CASCADE,
		datasource_id TEXT NOT NULL,
		name TEXT NOT NULL,
		definition TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		UNIQUE (app_id, name)
	);`,
	// Phase 7: servers sharing one database, jobs only one of them may
	// run, the server doing each flow run, and per-app quotas.
	`CREATE TABLE instances (
		id TEXT PRIMARY KEY,
		host TEXT NOT NULL,
		version TEXT NOT NULL,
		started_at TEXT NOT NULL,
		seen_at TEXT NOT NULL
	);
	CREATE TABLE job_claims (
		key TEXT PRIMARY KEY,
		instance_id TEXT NOT NULL,
		claimed_at TEXT NOT NULL
	);
	ALTER TABLE app_flow_runs ADD COLUMN instance_id TEXT NOT NULL DEFAULT '';
	CREATE INDEX app_flow_runs_status ON app_flow_runs (status);
	CREATE INDEX app_flow_runs_app ON app_flow_runs (app_id, started_at);
	ALTER TABLE apps ADD COLUMN quotas TEXT NOT NULL DEFAULT '{}';`,
}

func migrateMeta(ctx context.Context, db *DB) error {
	// Servers starting together against one Postgres database take turns.
	unlock, err := db.Lock(ctx, "pimpbase:migrate")
	if err != nil {
		return err
	}
	defer unlock()
	if _, err := db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version INTEGER PRIMARY KEY)`); err != nil {
		return err
	}
	var current int
	if err := db.QueryRow(ctx, `SELECT COALESCE(MAX(version), 0) FROM schema_migrations`).Scan(&current); err != nil {
		return err
	}
	for i := current; i < len(metaMigrations); i++ {
		tx, err := db.Begin(ctx)
		if err != nil {
			return err
		}
		for _, stmt := range splitStatements(metaMigrations[i]) {
			if _, err := tx.Exec(ctx, stmt); err != nil {
				tx.Rollback()
				return fmt.Errorf("migration %d: %w", i+1, err)
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES (?)`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func splitStatements(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ';' {
			if stmt := trimSpace(s[start:i]); stmt != "" {
				out = append(out, stmt)
			}
			start = i + 1
		}
	}
	if stmt := trimSpace(s[start:]); stmt != "" {
		out = append(out, stmt)
	}
	return out
}

func trimSpace(s string) string {
	i, j := 0, len(s)
	for i < j && (s[i] == ' ' || s[i] == '\n' || s[i] == '\t' || s[i] == '\r') {
		i++
	}
	for j > i && (s[j-1] == ' ' || s[j-1] == '\n' || s[j-1] == '\t' || s[j-1] == '\r') {
		j--
	}
	return s[i:j]
}
