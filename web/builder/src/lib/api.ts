// A thin client for the Bubibase HTTP API.

export type Role = 'PUBLIC' | 'BASIC' | 'POWER' | 'ADMIN'
export type FieldType =
  | 'text' | 'longtext' | 'number' | 'boolean' | 'date' | 'single_select'
  | 'multi_select' | 'link' | 'autonumber' | 'formula'

export interface User { id: string; email: string; name: string; role: string; createdAt: string }
export interface App { id: string; slug: string; name: string; createdAt: string; updatedAt: string }
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
  updateApp: (app: string, p: { name?: string; slug?: string }) => call<App>('PATCH', `/apps/${enc(app)}`, p),
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
  createRow: (app: string, t: string, v: object) => call<Row>('POST', `/apps/${enc(app)}/tables/${t}/rows`, v),
  updateRow: (app: string, t: string, id: number, v: object) => call<Row>('PATCH', `/apps/${enc(app)}/tables/${t}/rows/${id}`, v),
  deleteRows: (app: string, t: string, ids: number[]) => call<{ deleted: number }>('POST', `/apps/${enc(app)}/tables/${t}/rows/delete`, { ids }),
  importCSV: (app: string, t: string, file: File, addFields: boolean) => {
    const fd = new FormData()
    fd.append('file', file)
    return call<{ imported: number; skipped: number; errors: string[]; newFields: string[]; ignoredFields: string[] }>(
      'POST', `/apps/${enc(app)}/tables/${t}/import?addFields=${addFields}`, fd)
  },
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
