// The runtime every rendered screen shares, and the scope each component
// sees. Both travel through Svelte context.
import { getContext, setContext } from 'svelte'
import type { RuntimeFlow, RuntimeQuery, Table } from '../api'
import { queryTable } from '../apis/bindings'
import type { Node, Scope } from './types'

export class Runtime {
  appId: string
  appSlug: string
  tables: Table[]
  // queries are the API queries the visitor may run.
  queries: RuntimeQuery[]
  // flows are the button-started flows the visitor may run.
  flows: RuntimeFlow[]
  // env picks the variables queries use: development in the builder.
  env: 'development' | 'production'
  // editing is true inside the builder's canvas.
  editing: boolean
  state = $state<Record<string, any>>({})
  // dialogs maps a dialog component id to whether it is open.
  dialogs = $state<Record<string, boolean>>({})
  // dialogScopes keeps the scope a dialog was opened from, so a dialog
  // opened from a table row can read {{ row }}.
  dialogScopes = $state<Record<string, Partial<Scope>>>({})
  // refresh counters: data components reload when theirs or "all" change.
  refresh = $state<Record<string, number>>({ all: 0 })
  // applyConditions makes the builder's canvas follow show-when rules and
  // conditional settings, as the app does.
  applyConditions = $state(false)
  // device is the kind of screen the app is shown on.
  device = $state<'desktop' | 'tablet' | 'phone'>('desktop')
  // selected and hovered component ids, in the builder.
  selected = $state<string | null>(null)
  hovered = $state<string | null>(null)
  navigate: (path: string) => void
  toast: (kind: 'success' | 'error' | 'info', message: string) => void
  confirm: (title: string, message: string) => Promise<boolean>
  select: (id: string) => void

  constructor(o: {
    appId: string; appSlug: string; tables: Table[]; editing: boolean
    queries?: RuntimeQuery[]; flows?: RuntimeFlow[]; env?: 'development' | 'production'
    navigate: (path: string) => void
    toast: (kind: 'success' | 'error' | 'info', message: string) => void
    confirm: (title: string, message: string) => Promise<boolean>
    select?: (id: string) => void
  }) {
    this.appId = o.appId
    this.appSlug = o.appSlug
    this.tables = o.tables
    this.queries = o.queries ?? []
    this.flows = o.flows ?? []
    this.env = o.env ?? 'production'
    this.editing = o.editing
    this.navigate = o.navigate
    this.toast = o.toast
    this.confirm = o.confirm
    this.select = o.select ?? (() => {})
  }

  // table finds a table, or an API query's rows described as one
  // ("query:<id>").
  table(id?: string): Table | undefined {
    if (!id) return undefined
    if (id.startsWith('query:')) {
      const q = this.query(id.slice(6))
      return q ? queryTable(q, this.appId) : undefined
    }
    return this.tables.find((t) => t.id === id || t.name === id)
  }

  flow(id?: string): RuntimeFlow | undefined {
    return id ? this.flows.find((f) => f.id === id || f.name === id) : undefined
  }

  query(id?: string): RuntimeQuery | undefined {
    return id ? this.queries.find((q) => q.id === id || q.name === id) : undefined
  }

  reload(target?: string) {
    if (target) this.refresh[target] = (this.refresh[target] ?? 0) + 1
    else this.refresh.all++
  }
}

const runtimeKey = Symbol('runtime')
const scopeKey = Symbol('scope')
const formKey = Symbol('form')

export const setRuntime = (r: Runtime) => setContext(runtimeKey, r)
export const getRuntime = () => getContext<Runtime>(runtimeKey)

type ScopeFn = () => Scope

export function setScope(fn: ScopeFn) {
  setContext(scopeKey, fn)
}

export function getScope(): ScopeFn {
  return getContext<ScopeFn>(scopeKey)
}

// extendScope gives the children of a component more to bind to.
export function extendScope(extra: () => Partial<Scope>) {
  const parent = getScope()
  setScope(() => ({ ...parent(), ...extra() }))
}

// FieldRule is what a form field asks the form to check.
export interface FieldRule {
  label: string
  required?: boolean
  minLength?: number
  maxLength?: number
  min?: number
  max?: number
  pattern?: string
  patternMessage?: string
  type?: string
}

export class FormState {
  values = $state<Record<string, any>>({})
  errors = $state<Record<string, string>>({})
  submitting = $state(false)
  // uploading counts files still on their way to the server.
  uploading = $state(0)
  rules = new Map<string, FieldRule>()
  table: Table | undefined
  submitted = $state(false)

  constructor(table?: Table) {
    this.table = table
  }

  register(name: string, rule: FieldRule) {
    this.rules.set(name, rule)
    return () => this.rules.delete(name)
  }

  // check validates one field and records its error.
  check(name: string): boolean {
    const rule = this.rules.get(name)
    const err = rule ? validate(this.values[name], rule) : ''
    if (err) this.errors[name] = err
    else delete this.errors[name]
    return !err
  }

  validate(): boolean {
    let ok = true
    for (const name of this.rules.keys()) ok = this.check(name) && ok
    return ok
  }
}

export function validate(v: any, r: FieldRule): string {
  const empty = v === null || v === undefined || v === '' || (Array.isArray(v) && v.length === 0)
  if (empty) return r.required ? `${r.label} is required` : ''
  if (typeof v === 'string') {
    if (r.minLength && v.length < r.minLength) return `${r.label} needs at least ${r.minLength} characters`
    if (r.maxLength && v.length > r.maxLength) return `${r.label} has at most ${r.maxLength} characters`
    if (r.type === 'email' && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(v)) return `${r.label} must be an email address`
    if (r.type === 'url' && !/^https?:\/\/\S+$/.test(v)) return `${r.label} must be a web address starting with http`
    if (r.pattern) {
      try {
        if (!new RegExp(`^(?:${r.pattern})$`).test(v)) return r.patternMessage || `${r.label} is not in the expected format`
      } catch { /* a broken pattern checks nothing */ }
    }
  }
  if (typeof v === 'number' || (r.type === 'number' && v !== '')) {
    const n = Number(v)
    if (isNaN(n)) return `${r.label} must be a number`
    if (r.min !== undefined && r.min !== null && n < r.min) return `${r.label} must be at least ${r.min}`
    if (r.max !== undefined && r.max !== null && n > r.max) return `${r.label} must be at most ${r.max}`
  }
  return ''
}

export const setForm = (f: FormState) => setContext(formKey, f)
export const getForm = () => getContext<FormState | undefined>(formKey)

// findNode searches a tree.
export function findNode(root: Node, id: string): Node | undefined {
  if (root.id === id) return root
  for (const c of root.children) {
    const f = findNode(c, id)
    if (f) return f
  }
  return undefined
}

// pathTo returns the nodes from the root to id.
export function pathTo(root: Node, id: string): Node[] | null {
  if (root.id === id) return [root]
  for (const c of root.children) {
    const p = pathTo(c, id)
    if (p) return [root, ...p]
  }
  return null
}
