// A thin client for the Bubibase HTTP API.
import type { Design, Node, Screen } from './screen/types'

export type Role = 'PUBLIC' | 'BASIC' | 'POWER' | 'ADMIN'
export type FieldType =
  | 'text' | 'longtext' | 'number' | 'boolean' | 'date' | 'single_select'
  | 'multi_select' | 'link' | 'autonumber' | 'formula'

export interface User { id: string; email: string; name: string; role: string; createdAt: string }
export interface App { id: string; slug: string; name: string; design: Design; createdAt: string; updatedAt: string }
export interface Field {
  id: string; name: string; type: FieldType; required?: boolean; options?: string[]
  linkTable?: string; linkMany?: boolean; formula?: string; formulaMode?: 'dynamic' | 'static'
  resultType?: string
}
export interface Table {
  id: string; appId: string; name: string; fields: Field[]; primaryField?: string
  permissions: { read: Role; write: Role }
}
export interface LinkRef { _id: number; label: string }
export type Row = Record<string, any> & { _id: number }
export interface Page { rows: Row[]; total: number; engine: 'sql' | 'go' }
export interface FormulaError { start: number; end: number; message: string }
export interface FunctionInfo { name: string; category: string; signature: string; description: string }

export interface RuntimeInfo {
  app: App; role: Role; builder: boolean; user: { id: string; email: string; name: string } | null
  screens: Screen[]; tables: Table[]; queries: RuntimeQuery[]
}

// API connectors.
export interface Pair { key: string; value: string; off?: boolean }
export interface Auth { type: 'none' | 'basic' | 'bearer' | 'header' | 'query'; username?: string; password?: string; token?: string; name?: string; value?: string }
export interface Datasource {
  id: string; appId: string; name: string; kind: 'rest'
  config: { baseUrl: string; headers: Pair[]; auth: Auth; timeoutSeconds: number }
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
}
export interface RuntimeQuery { id: string; name: string; parameters: { name: string; type: ParamType; default: string }[]; fields: QueryField[] }
export interface Variable { name: string; secret: boolean; development: string; production: string; hasDevelopment: boolean; hasProduction: boolean }
export interface QueryResult {
  ok: boolean; status: number; statusText: string; headers: Record<string, string>
  format: 'json' | 'xml' | 'text'; body: any; rows: Record<string, any>[]; fields: QueryField[]
  parseError?: string; raw?: string; size: number; durationMs: number; cached: boolean
  request?: { method: string; url: string; headers: Pair[]; body: string }
}

export class ApiError extends Error {
  constructor(public status: number, message: string, public body: any) { super(message) }
}

async function call<T>(method: string, path: string, body?: unknown): Promise<T> {
  const init: RequestInit = { method, headers: {} }
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

  users: () => call<User[]>('GET', '/users'),
  createUser: (u: { email: string; name: string; password: string; role: string }) => call<User>('POST', '/users', u),
  updateUser: (id: string, p: Partial<{ name: string; role: string; password: string }>) => call<User>('PATCH', `/users/${id}`, p),
  deleteUser: (id: string) => call('DELETE', `/users/${id}`),

  apps: () => call<App[]>('GET', '/apps'),
  createApp: (name: string) => call<App>('POST', '/apps', { name }),
  app: (app: string) => call<{ app: App; role: Role; builder: boolean }>('GET', `/apps/${enc(app)}`),
  updateApp: (app: string, p: { name?: string; slug?: string; design?: Design }) => call<App>('PATCH', `/apps/${enc(app)}`, p),
  deleteApp: (app: string) => call('DELETE', `/apps/${enc(app)}`),
  members: (app: string) => call<{ userId: string; email: string; name: string; role: Role }[]>('GET', `/apps/${enc(app)}/members`),
  setMember: (app: string, user: string, role: Role | 'NONE') => call('PUT', `/apps/${enc(app)}/members/${user}`, { role }),

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
  row: (app: string, t: string, id: number) => call<Row>('GET', `/apps/${enc(app)}/tables/${t}/rows/${id}`),
  createRow: (app: string, t: string, v: object) => call<Row>('POST', `/apps/${enc(app)}/tables/${t}/rows`, v),
  updateRow: (app: string, t: string, id: number, v: object) => call<Row>('PATCH', `/apps/${enc(app)}/tables/${t}/rows/${id}`, v),
  deleteRows: (app: string, t: string, ids: number[]) => call<{ deleted: number }>('POST', `/apps/${enc(app)}/tables/${t}/rows/delete`, { ids }),
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

  datasources: (app: string) => call<Datasource[]>('GET', `/apps/${enc(app)}/datasources`),
  createDatasource: (app: string, d: Partial<Datasource>) => call<Datasource>('POST', `/apps/${enc(app)}/datasources`, d),
  updateDatasource: (app: string, id: string, d: Partial<Datasource>) => call<Datasource>('PATCH', `/apps/${enc(app)}/datasources/${id}`, d),
  deleteDatasource: (app: string, id: string) => call('DELETE', `/apps/${enc(app)}/datasources/${id}`),
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
  exportURL: (app: string, t: string, filter: string) =>
    `/api/apps/${enc(app)}/tables/${t}/export` + (filter ? `?filter=${enc(filter)}` : ''),
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
]

export const typeInfo = (t: FieldType) => fieldTypes.find((x) => x.type === t)!
export const roles: Role[] = ['PUBLIC', 'BASIC', 'POWER', 'ADMIN']
