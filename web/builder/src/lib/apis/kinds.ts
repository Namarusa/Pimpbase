// The kinds of data sources the API studio offers.
import type { DbKind } from '../api'

export const dbLabels: Record<DbKind, string> = {
  postgres: 'PostgreSQL',
  mysql: 'MySQL / MariaDB',
  mssql: 'SQL Server',
  mongodb: 'MongoDB',
}

export const sourceKinds: { kind: 'rest' | DbKind; label: string; hint: string }[] = [
  { kind: 'rest', label: 'REST API', hint: 'JSON or XML over HTTP' },
  { kind: 'postgres', label: 'PostgreSQL', hint: 'and compatible: Supabase, Neon, CockroachDB' },
  { kind: 'mysql', label: 'MySQL / MariaDB', hint: 'and compatible: PlanetScale, TiDB' },
  { kind: 'mssql', label: 'SQL Server', hint: 'and Azure SQL' },
  { kind: 'mongodb', label: 'MongoDB', hint: 'and Atlas' },
]
