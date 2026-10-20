// A thin client for the Bubibase HTTP API.
import type { Design, Node, Screen } from './screen/types'

// Role is an app role id: PUBLIC, BASIC, POWER, ADMIN or a custom id.
export type Role = string
export interface RoleDef { id: Role; name: string; description: string; inherits?: Role; color?: string; builtin: boolean }
export type FieldType =
  | 'text' | 'longtext' | 'number' | 'boolean' | 'date' | 'single_select'
  | 'multi_select' | 'link' | 'autonumber' | 'formula' | 'attachment'

export interface User {
  id: string; email: string; name: string; role: string; createdAt: string
  disabled?: boolean; hasPassword?: boolean; groups?: string[]; lastLoginAt?: string | null
}
export interface Grant { appId: string; role: Role; roleName?: string; source: 'manual' | 'sso' }
// UserRow is a user as the Users page lists them.
export interface UserRow extends User { signIn: string[]; apps: Grant[] }
export interface Member { userId: string; email: string; name: string; role: Role; source: 'manual' | 'sso' }
export interface ApiKey {
  id: string; name: string; appId: string; role: string; createdAt: string
  expiresAt: string | null; lastUsedAt: string | null; key?: string
}
export type ProviderKind = 'google' | 'microsoft' | 'oidc'
export interface Mapping { claim: string; value: string; workspaceRole: string; apps: { appId: string; role: Role }[] }
export interface Provider {
  id: string; kind: ProviderKind; name: string; enabled: boolean; hasSecret: boolean
  issuer: string; tenant: string; clientId: string; scopes: string[]; allowedDomains: string[]
  autoCreate: boolean; defaultRole: string; trustEmail: boolean; groupsClaim: string; mappings: Mapping[]
  createdAt?: string; updatedAt?: string
}
export interface PublicProvider { id: string; kind: ProviderKind; name: string }
export interface App { id: string; slug: string; name: string; design: Design; createdAt: string; updatedAt: string }
export interface Field {
  id: string; name: string; type: FieldType; required?: boolean; options?: string[]
  linkTable?: string; linkMany?: boolean; formula?: string; formulaMode?: 'dynamic' | 'static'
  resultType?: string
  // readOnly fields of external tables are filled by their database.
  readOnly?: boolean
}
export interface Table {
  id: string; appId: string; name: string; fields: Field[]; primaryField?: string
  permissions: Permissions
  // external is set on tables imported from a database data source.
  external?: ExternalInfo
}
export type DbKind = 'postgres' | 'mysql' | 'mssql' | 'mongodb'
export interface DbColumn {
  name: string; type: FieldType; dbType: string; nullable: boolean; options?: string[]
  auto?: boolean; generated?: boolean; binary?: boolean; hidden?: boolean
}
export interface ExternalInfo {
  datasourceId: string; kind: DbKind; schema: string; source: string; sourceKind: 'table' | 'view' | 'collection'
  primaryKey: string[]; readOnly: boolean; columns: DbColumn[]
}
export interface DbTableInfo { schema: string; name: string; kind: 'table' | 'view' | 'collection'; imported?: string; columns?: DbColumn[]; primaryKey?: string[] }
export interface Connector { kind: DbKind; label: string; port: string }
export interface ConnectionTest { ok: boolean; message: string; tables: number; durationMs: number }
export interface RowRule { role: Role; filter: string }
export interface Permissions { read: Role; write: Role; delete: Role; rows: RowRule[] }
export interface LinkRef { _id: RowId; label: string; hidden?: boolean }
// FileRef is one file of an attachment field.
export interface FileRef { id: string; name: string; type: string; size: number; url: string }
// RowId is a built-in row's number, or an external row's key.
export type RowId = number | string
export type Row = Record<string, any> & { _id: RowId }

// rowKey reads a row id from a row, a number or text; null when blank.
export function rowKey(v: any): RowId | null {
  if (v && typeof v === 'object' && '_id' in v) v = v._id
  if (v === null || v === undefined || v === '') return null
  if (typeof v === 'number') return v
  const s = String(v).trim()
  return /^-?\d+$/.test(s) && s.length < 16 ? Number(s) : s
}
export interface Page { rows: Row[]; total: number; engine: 'sql' | 'go' }
export interface FormulaError { start: number; end: number; message: string }
export interface FunctionInfo { name: string; category: string; signature: string; description: string }

export interface RuntimeInfo {
  app: App; role: Role; builder: boolean
  user: { id: string; email: string; name: string; role: Role; roles: Role[]; groups: string[] } | null
  screens: Screen[]; tables: Table[]; queries: RuntimeQuery[]; flows: RuntimeFlow[]
  // roles and preview are sent to builders, who may preview the app as a role.
  roles?: RoleDef[]; preview?: boolean
  // providers are sent to visitors who are not signed in.
  providers?: PublicProvider[]
}

// API connectors.
export interface Pair { key: string; value: string; off?: boolean }
export interface Auth { type: 'none' | 'basic' | 'bearer' | 'header' | 'query'; username?: string; password?: string; token?: string; name?: string; value?: string }
export interface DbSettings {
  host: string; port: string; database: string; user: string; password: string; schema?: string
  ssl?: '' | 'disable' | 'require' | 'verify'; uri?: string; options: { key: string; value: string }[]
  maxConnections?: number; readOnly?: boolean
}
export interface Datasource {
  id: string; appId: string; name: string; kind: 'rest' | DbKind
  config: { baseUrl: string; headers: Pair[]; auth: Auth; timeoutSeconds: number; db?: DbSettings }
}
export type ParamType = 'text' | 'number' | 'boolean' | 'json'
export interface QueryParam { name: string; type: ParamType; default: string; test: string }
export interface QueryField { name: string; type: 'text' | 'number' | 'boolean' | 'date' | 'object' }
export type BodyType = 'none' | 'json' | 'form' | 'xml' | 'text'
export interface Query {
  id: string; appId: string; datasourceId: string; name: string
  method: string; path: string; query: Pair[]; headers: Pair[]
  bodyType: BodyType; body: string; form: Pair[]; parameters: QueryParam[]
  responseType: 'auto' | 'json' | 'xml' | 'text'; resultPath: string; role: Role; cacheSeconds: number
  fields: QueryField[]; createdAt?: string; updatedAt?: string
  // Database queries: SQL, or a MongoDB operation on a collection.
  sql: string; collection: string; operation: string; filter: string; document: string; options: string
}
export interface RuntimeQuery { id: string; name: string; parameters: { name: string; type: ParamType; default: string }[]; fields: QueryField[] }
export interface Variable { name: string; secret: boolean; development: string; production: string; hasDevelopment: boolean; hasProduction: boolean }
export interface QueryResult {
  ok: boolean; status: number; statusText: string; headers: Record<string, string>
  format: 'json' | 'xml' | 'text'; body: any; rows: Record<string, any>[]; fields: QueryField[]
  parseError?: string; raw?: string; size: number; durationMs: number; cached: boolean
  request?: { method: string; url: string; headers: Pair[]; body: string; params?: any[] }
  rowsAffected?: number; truncated?: boolean; columns?: string[]
}

// Flows.
export type TriggerType = 'row_created' | 'row_updated' | 'row_deleted' | 'schedule' | 'webhook' | 'manual'
export interface FlowParam { name: string; type: ParamType; default: string }
export interface Trigger {
  type: TriggerType; table?: string; fields?: string[]; cron?: string; timezone?: string
  parameters?: FlowParam[]; role: Role; respond?: boolean
}
export interface FlowStep {
  id: string; name: string; type: string; inputs: Record<string, any>
  then?: FlowStep[]; else?: FlowStep[]; do?: FlowStep[]; disabled?: boolean; continueOnError?: boolean
}
export interface Flow {
  id: string; appId: string; name: string; enabled: boolean; trigger: Trigger; steps: FlowStep[]; runAs?: 'flow' | 'user'; runRole?: string
  hookToken: string; createdAt?: string; updatedAt?: string
}
export interface StepLog {
  stepId: string; name: string; type: string; depth: number; item?: number
  status: 'ok' | 'failed' | 'skipped'; inputs?: any; output?: any; error?: string; startedAt: string; durationMs: number
}
export interface FlowRun {
  id: string; appId: string; flowId: string; status: 'running' | 'succeeded' | 'failed'; test: boolean
  triggerKind: string; trigger: any; steps: StepLog[]; output: any; error: string
  startedAt: string; finishedAt?: string; durationMs: number
}
export interface RuntimeFlow { id: string; name: string; enabled: boolean; parameters: FlowParam[] }

export class ApiError extends Error {
  constructor(public status: number, message: string, public body: any) { super(message) }
}

// preview, when set, makes a builder's requests act as that app role.
export const preview = { role: '' }

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, headers: {} }
  if (preview.role) (init.headers as Record<string, string>)['X-Bubibase-Preview-Role'] = preview.role
  // The builder reads external tables with the development connection.
  if (typeof location !== 'undefined' && !location.pathname.startsWith('/app/')) (init.headers as Record<string, string>)['X-Bubibase-Env'] = 'development'
  if (body instanceof FormData) {
    init.body = body
  } else if (body !== undefined) {
    init.body = JSON.stringify(body)
    ;(init.headers as Record<string, string>)['Content-Type'] = 'application/json'
  }
  const res = await fetch('/api' + path, init)
  const text = await res.text()
  const data = text ? JSON.parse(text) : null
  if (!res.ok) throw new ApiError(res.status, data?.error ?? res.statusText, data)
  return data as T
}

const enc = encodeURIComponent

export const api = {
  setup: () => call<{ needsSetup: boolean }>('GET', '/setup'),
  createOwner: (email: string, name: string, password: string) => call<User>('POST', '/setup', { email, name, password }),
  login: (email: string, password: string) => call<User>('POST', '/auth/login', { email, password }),
  logout: () => call('POST', '/auth/logout'),
  me: () => call<User>('GET', '/auth/me'),
  providers: () => call<PublicProvider[]>('GET', '/auth/providers'),
  ssoStartURL: (id: string, returnTo: string) => `/api/sso/${enc(id)}/start?return=${enc(returnTo)}`,
  keys: () => call<ApiKey[]>('GET', '/auth/keys'),
  createKey: (k: { name: string; appId?: string; role?: string; expiresInDays?: number }) => call<ApiKey>('POST', '/auth/keys', k),
  deleteKey: (id: string) => call('DELETE', `/auth/keys/${id}`),

  users: () => call<UserRow[]>('GET', '/users'),
  createUser: (u: { email: string; name: string; password: string; role: string; ssoOnly?: boolean }) => call<User>('POST', '/users', u),
  updateUser: (id: string, p: Partial<{ name: string; role: string; password: string; disabled: boolean }>) => call<User>('PATCH', `/users/${id}`, p),
  ssoProviders: () => call<{ providers: Provider[]; redirectUrl: string }>('GET', '/sso/providers'),
  createProvider: (p: Partial<Provider> & { clientSecret?: string }) => call<Provider>('POST', '/sso/providers', p),
  updateProvider: (id: string, p: Partial<Provider> & { clientSecret?: string }) => call<Provider>('PATCH', `/sso/providers/${id}`, p),
  deleteProvider: (id: string) => call('DELETE', `/sso/providers/${id}`),
  testMapping: (id: string, t: { email: string; groups: string[] }) =>
    call<{ workspace: string; apps: Record<string, Role>; matched: number[] }>('POST', `/sso/providers/${id}/test`, t),
  deleteUser: (id: string) => call('DELETE', `/users/${id}`),

  apps: () => call<App[]>('GET', '/apps'),
  createApp: (name: string) => call<App>('POST', '/apps', { name }),
  app: (app: string) => call<{ app: App; role: Role; builder: boolean }>('GET', `/apps/${enc(app)}`),
  updateApp: (app: string, p: { name?: string; slug?: string; design?: Design }) => call<App>('PATCH', `/apps/${enc(app)}`, p),
  deleteApp: (app: string) => call('DELETE', `/apps/${enc(app)}`),
  members: (app: string) => call<Member[]>('GET', `/apps/${enc(app)}/members`),
  setMember: (app: string, user: string, role: Role | 'NONE') => call('PUT', `/apps/${enc(app)}/members/${user}`, { role }),
  roles: (app: string) => call<RoleDef[]>('GET', `/apps/${enc(app)}/roles`),
  createRole: (app: string, r: Partial<RoleDef>) => call<RoleDef>('POST', `/apps/${enc(app)}/roles`, r),
  updateRole: (app: string, id: Role, r: Partial<RoleDef>) => call<RoleDef>('PATCH', `/apps/${enc(app)}/roles/${enc(id)}`, r),
  deleteRole: (app: string, id: Role) => call('DELETE', `/apps/${enc(app)}/roles/${enc(id)}`),

  tables: (app: string) => call<Table[]>('GET', `/apps/${enc(app)}/tables`),
  createTable: (app: string, name: string, fields: Partial<Field>[]) => call<Table>('POST', `/apps/${enc(app)}/tables`, { name, fields }),
  updateTable: (app: string, t: string, p: object) => call<Table>('PATCH', `/apps/${enc(app)}/tables/${t}`, p),
  deleteTable: (app: string, t: string) => call('DELETE', `/apps/${enc(app)}/tables/${t}`),
  addField: (app: string, t: string, f: Partial<Field>) => call<Table>('POST', `/apps/${enc(app)}/tables/${t}/fields`, f),
  updateField: (app: string, t: string, f: string, p: object) => call<Table>('PATCH', `/apps/${enc(app)}/tables/${t}/fields/${f}`, p),
  deleteField: (app: string, t: string, f: string) => call<Table>('DELETE', `/apps/${enc(app)}/tables/${t}/fields/${f}`),
  checkFormula: (app: string, t: string, formula: string, isStatic = false) =>
    call<{ type: string; errors: FormulaError[] }>('POST', `/apps/${enc(app)}/tables/${t}/formula/check`, { formula, static: isStatic }),
  functions: () => call<FunctionInfo[]>('GET', '/formula/functions'),

  rows: (app: string, t: string, q: { filter?: string; sort?: string; desc?: boolean; limit?: number; offset?: number }) => {
    const p = new URLSearchParams()
    if (q.filter) p.set('filter', q.filter)
    if (q.sort) p.set('sort', q.sort)
    if (q.desc) p.set('desc', 'true')
    p.set('limit', String(q.limit ?? 100))
    p.set('offset', String(q.offset ?? 0))
    return call<Page>('GET', `/apps/${enc(app)}/tables/${t}/rows?${p}`)
  },
  row: (app: string, t: string, id: RowId) => call<Row>('GET', `/apps/${enc(app)}/tables/${t}/rows/${enc(String(id))}`),
  createRow: (app: string, t: string, v: object) => call<Row>('POST', `/apps/${enc(app)}/tables/${t}/rows`, v),
  updateRow: (app: string, t: string, id: RowId, v: object) => call<Row>('PATCH', `/apps/${enc(app)}/tables/${t}/rows/${enc(String(id))}`, v),
  deleteRows: (app: string, t: string, ids: RowId[]) => call<{ deleted: number }>('POST', `/apps/${enc(app)}/tables/${t}/rows/delete`, { ids }),
  refreshTable: (app: string, t: string) => call<Table>('POST', `/apps/${enc(app)}/tables/${t}/refresh`),
  uploadFile: (app: string, t: string, file: File) => {
    const fd = new FormData()
    fd.append('file', file)
    return call<FileRef>('POST', `/apps/${enc(app)}/tables/${t}/files`, fd)
  },
  importCSV: (app: string, t: string, file: File, addFields: boolean) => {
    const fd = new FormData()
    fd.append('file', file)
    return call<{ imported: number; skipped: number; errors: string[]; newFields: string[]; ignoredFields: string[] }>(
      'POST', `/apps/${enc(app)}/tables/${t}/import?addFields=${addFields}`, fd)
  },
  screens: (app: string) => call<Screen[]>('GET', `/apps/${enc(app)}/screens`),
  createScreen: (app: string, s: { name: string; route: string; role?: Role; definition?: Node }) => call<Screen>('POST', `/apps/${enc(app)}/screens`, s),
  updateScreen: (app: string, id: string, s: Partial<{ name: string; route: string; role: Role; position: number; definition: Node }>) =>
    call<Screen>('PATCH', `/apps/${enc(app)}/screens/${id}`, s),
  deleteScreen: (app: string, id: string) => call('DELETE', `/apps/${enc(app)}/screens/${id}`),
  runtime: (app: string) => call<RuntimeInfo>('GET', `/apps/${enc(app)}/runtime`),
  brand: (app: string) => call<{ name: string; slug: string; design: Design; providers: PublicProvider[] }>('GET', `/apps/${enc(app)}/brand`),

  datasources: (app: string) => call<Datasource[]>('GET', `/apps/${enc(app)}/datasources`),
  createDatasource: (app: string, d: Partial<Datasource>) => call<Datasource>('POST', `/apps/${enc(app)}/datasources`, d),
  updateDatasource: (app: string, id: string, d: Partial<Datasource>) => call<Datasource>('PATCH', `/apps/${enc(app)}/datasources/${id}`, d),
  deleteDatasource: (app: string, id: string) => call('DELETE', `/apps/${enc(app)}/datasources/${id}`),
  connectors: () => call<Connector[]>('GET', '/connectors'),
  testConnection: (app: string, kind: DbKind, config: Datasource['config'], env: 'development' | 'production') =>
    call<ConnectionTest>('POST', `/apps/${enc(app)}/datasources/test`, { kind, config, env }),
  dbTables: (app: string, ds: string) => call<DbTableInfo[]>('GET', `/apps/${enc(app)}/datasources/${ds}/schema`),
  dbTable: (app: string, ds: string, schema: string, table: string) =>
    call<DbTableInfo>('GET', `/apps/${enc(app)}/datasources/${ds}/schema/table?schema=${enc(schema)}&table=${enc(table)}`),
  importTable: (app: string, ds: string, t: { schema: string; source: string; name: string }) => call<Table>('POST', `/apps/${enc(app)}/datasources/${ds}/tables`, t),
  queries: (app: string) => call<Query[]>('GET', `/apps/${enc(app)}/queries`),
  createQuery: (app: string, q: Partial<Query>) => call<Query>('POST', `/apps/${enc(app)}/queries`, q),
  updateQuery: (app: string, id: string, q: Partial<Query>) => call<Query>('PATCH', `/apps/${enc(app)}/queries/${id}`, q),
  deleteQuery: (app: string, id: string) => call('DELETE', `/apps/${enc(app)}/queries/${id}`),
  testQuery: (app: string, q: Partial<Query>, env: 'development' | 'production') => call<QueryResult>('POST', `/apps/${enc(app)}/queries/test`, { query: q, env }),
  runQuery: (app: string, id: string, params: Record<string, any>, env?: 'development' | 'production') =>
    call<QueryResult>('POST', `/apps/${enc(app)}/queries/${enc(id)}/run`, { params, env }),
  variables: (app: string) => call<Variable[]>('GET', `/apps/${enc(app)}/variables`),
  setVariable: (app: string, name: string, v: Partial<{ secret: boolean; development: string; production: string }>) =>
    call<Variable>('PUT', `/apps/${enc(app)}/variables/${enc(name)}`, v),
  deleteVariable: (app: string, name: string) => call('DELETE', `/apps/${enc(app)}/variables/${enc(name)}`),
  flows: (app: string) => call<Flow[]>('GET', `/apps/${enc(app)}/flows`),
  createFlow: (app: string, f: Partial<Flow>) => call<Flow>('POST', `/apps/${enc(app)}/flows`, f),
  updateFlow: (app: string, id: string, f: Partial<Flow>) => call<Flow>('PATCH', `/apps/${enc(app)}/flows/${id}`, f),
  deleteFlow: (app: string, id: string) => call('DELETE', `/apps/${enc(app)}/flows/${id}`),
  newHook: (app: string, id: string) => call<Flow>('POST', `/apps/${enc(app)}/flows/${id}/hook`),
  testFlow: (app: string, id: string, trigger?: Record<string, any>) => call<FlowRun>('POST', `/apps/${enc(app)}/flows/${id}/test`, trigger ? { trigger } : {}),
  runFlow: (app: string, id: string, params: Record<string, any>, test = false) =>
    call<{ id: string; status: FlowRun['status']; output: any; error: string }>('POST', `/apps/${enc(app)}/flows/${enc(id)}/run`, { params, test }),
  flowRuns: (app: string, id: string) => call<FlowRun[]>('GET', `/apps/${enc(app)}/flows/${id}/runs`),
  flowRun: (app: string, run: string) => call<FlowRun>('GET', `/apps/${enc(app)}/runs/${run}`),
  nextRuns: (cron: string, timezone: string) => call<{ next: string[] }>('GET', `/flows/schedule?cron=${enc(cron)}&timezone=${enc(timezone)}`),
  exportURL: (app: string, t: string, filter: string) =>
    `/api/apps/${enc(app)}/tables/${t}/export` + (filter ? `?filter=${enc(filter)}` : ''),
}

// The AI assistant.
export interface AIStatus { configured: boolean; source: '' | 'server' | 'workspace'; keyHint: string; model: string; models: string[] }
export type DraftKind = 'table' | 'screen' | 'query' | 'flow'
export interface AIDraft { kind: DraftKind; summary: string; changes: string[]; notes: string; draft: any; target: string; current?: any }
export interface AIFormula { formula: string; explanation: string; type: string }
export interface AIAnswer { answer: string; calls: { tool: string; input: any; error?: string }[] }
export const ai = {
  status: () => call<AIStatus>('GET', '/ai'),
  update: (p: { apiKey?: string; model?: string }) => call<AIStatus>('PATCH', '/ai', p),
  draft: (app: string, kind: DraftKind, prompt: string, catalog: any, target?: string) =>
    call<AIDraft>('POST', `/apps/${enc(app)}/ai/draft`, { kind, prompt, catalog, target: target ?? '' }),
  formula: (app: string, table: string, prompt: string, purpose: 'field' | 'filter' | 'rule', current = '') =>
    call<AIFormula>('POST', `/apps/${enc(app)}/ai/formula`, { table, prompt, purpose, current }),
  ask: (app: string, question: string, as = '') => call<AIAnswer>('POST', `/apps/${enc(app)}/ai/ask`, { question, as }),
}

export const fieldTypes: { type: FieldType; label: string; icon: string }[] = [
  { type: 'text', label: 'Text', icon: 'Aa' },
  { type: 'longtext', label: 'Long text', icon: '¶' },
  { type: 'number', label: 'Number', icon: '#' },
  { type: 'boolean', label: 'Yes / no', icon: '✓' },
  { type: 'date', label: 'Date', icon: '◷' },
  { type: 'single_select', label: 'Single select', icon: '◉' },
  { type: 'multi_select', label: 'Multi select', icon: '☰' },
  { type: 'link', label: 'Link to table', icon: '↗' },
  { type: 'autonumber', label: 'Auto number', icon: '№' },
  { type: 'formula', label: 'Formula', icon: 'ƒ' },
  { type: 'attachment', label: 'Attachment', icon: '⎘' },
]

export const typeInfo = (t: FieldType) => fieldTypes.find((x) => x.type === t)!
export const roles: Role[] = ['PUBLIC', 'BASIC', 'POWER', 'ADMIN']

// The built-in roles, for pages that have not loaded an app's own.
export const builtinRoles: RoleDef[] = [
  { id: 'ADMIN', name: 'Admin', description: 'Everything in the app, including every custom role.', inherits: 'POWER', builtin: true, color: '#dc2626' },
  { id: 'POWER', name: 'Power', description: 'Basic access plus what tables and screens open to power users.', inherits: 'BASIC', builtin: true, color: '#7c3aed' },
  { id: 'BASIC', name: 'Basic', description: 'Signed-in users who were given access to the app.', inherits: 'PUBLIC', builtin: true, color: '#2563eb' },
  { id: 'PUBLIC', name: 'Public', description: 'Anyone, without signing in.', builtin: true, color: '#64748b' },
]
