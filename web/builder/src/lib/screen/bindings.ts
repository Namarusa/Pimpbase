// {{ bindings }} read values from the scope: {{ row.Name }}, {{ url.id }},
// {{ user.name }}, {{ data.total }}, {{ state.search }}, {{ form.Email }}.
// A pipe formats the value: {{ row.Price | currency }}.
import { formatDate } from '../format'
import type { Scope } from './types'

const re = /\{\{\s*([^}]*?)\s*\}\}/g

export const hasBinding = (v: unknown): v is string => typeof v === 'string' && v.includes('{{')

// lookup follows a dotted path; segments may hold spaces ("row.First name").
export function lookup(scope: Record<string, any>, path: string): any {
  let cur: any = scope
  for (const raw of path.split('.')) {
    const key = raw.trim()
    if (key === '') continue
    if (cur === null || cur === undefined) return undefined
    cur = Array.isArray(cur) && /^\d+$/.test(key) ? cur[Number(key)] : cur[key]
  }
  return cur
}

const isoDate = /^\d{4}-\d\d-\d\dT\d\d:\d\d/

// toText shows a value the way people read it.
export function toText(v: any): string {
  if (v === null || v === undefined) return ''
  if (typeof v === 'boolean') return v ? 'Yes' : 'No'
  if (typeof v === 'number') return v.toLocaleString(undefined, { maximumFractionDigits: 10 })
  if (typeof v === 'string') return isoDate.test(v) ? formatDate(v) : v
  if (Array.isArray(v)) return v.map(toText).join(', ')
  if (typeof v === 'object' && 'label' in v) return String(v.label)
  return JSON.stringify(v)
}

const pipes: Record<string, (v: any, arg?: string) => any> = {
  upper: (v) => toText(v).toUpperCase(),
  lower: (v) => toText(v).toLowerCase(),
  number: (v, arg) => (v === null || v === undefined || v === '' ? '' : Number(v).toLocaleString(undefined, { minimumFractionDigits: Number(arg ?? 0), maximumFractionDigits: Number(arg ?? 2) })),
  currency: (v, arg) => (v === null || v === undefined || v === '' ? '' : Number(v).toLocaleString(undefined, { style: 'currency', currency: arg || 'USD' })),
  percent: (v) => (v === null || v === undefined || v === '' ? '' : Number(v).toLocaleString(undefined, { style: 'percent', maximumFractionDigits: 1 })),
  date: (v) => (v ? new Date(v).toLocaleDateString(undefined, { timeZone: String(v).endsWith('T00:00:00Z') ? 'UTC' : undefined, year: 'numeric', month: 'short', day: 'numeric' }) : ''),
  datetime: (v) => (v ? new Date(v).toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }) : ''),
  count: (v) => (Array.isArray(v) ? v.length : v ? 1 : 0),
  json: (v) => JSON.stringify(v),
  default: (v, arg) => (v === null || v === undefined || v === '' || (Array.isArray(v) && v.length === 0) ? arg ?? '' : v),
  first: (v) => (Array.isArray(v) ? v[0] : v),
  ids: (v) => (Array.isArray(v) ? v.map((x) => x?._id ?? x) : v?._id ?? v),
}
export const pipeNames = Object.keys(pipes)

// evaluate reads one binding expression: a path and optional pipes.
export function evaluate(expr: string, scope: Record<string, any>): any {
  const [path, ...rest] = expr.split('|')
  let v = lookup(scope, path.trim())
  for (const p of rest) {
    const m = p.trim().match(/^(\w+)(?::\s*"?(.*?)"?)?$/)
    if (m && pipes[m[1]]) v = pipes[m[1]](v, m[2])
  }
  return v
}

// resolve fills the bindings in a value. A value that is exactly one
// binding keeps the bound value's type (a number, a row, a list).
export function resolve(value: any, scope: Record<string, any>, escape?: (s: string) => string): any {
  if (!hasBinding(value)) return value
  const whole = value.match(/^\{\{\s*([^}]*?)\s*\}\}$/)
  if (whole && !escape) return evaluate(whole[1], scope)
  return value.replace(re, (_, expr) => {
    const text = toText(evaluate(expr, scope))
    return escape ? escape(text) : text
  })
}

// resolveText always returns text.
export const resolveText = (value: any, scope: Record<string, any>): string => {
  const v = resolve(value, scope)
  return typeof v === 'string' ? v : toText(v)
}

// formulaText escapes a value placed inside a "quoted" formula string.
export const formulaText = (s: string) => s.replaceAll('"', '""')

// resolveFilter fills the bindings in a filter formula. Lines whose
// bindings are all empty are dropped, so "search" boxes left blank do not
// filter anything out.
export function resolveFilter(filter: string | undefined, scope: Scope): string {
  if (!filter) return ''
  const parts: string[] = []
  for (const line of filter.split(/\n+/)) {
    const t = line.trim()
    if (!t) continue
    const exprs = [...t.matchAll(re)].map((m) => m[1])
    if (exprs.length && exprs.every((e) => {
      const v = evaluate(e, scope)
      return v === null || v === undefined || v === '' || (Array.isArray(v) && v.length === 0)
    })) continue
    parts.push(resolve(t, scope, formulaText))
  }
  if (parts.length <= 1) return parts[0] ?? ''
  return parts.map((p) => `(${p})`).join(' AND ')
}

// matchRoute matches a path against a route such as /orders/:id.
export function matchRoute(route: string, path: string): Record<string, string> | null {
  const r = route.split('/').filter(Boolean)
  const p = path.split('/').filter(Boolean)
  if (r.length !== p.length) return null
  const out: Record<string, string> = {}
  for (let i = 0; i < r.length; i++) {
    if (r[i].startsWith(':')) out[r[i].slice(1)] = decodeURIComponent(p[i])
    else if (r[i] !== p[i]) return null
  }
  return out
}

// condition checks a component's "visible when" rule.
export interface Condition { left: string; op: string; right?: string }
export const conditionOps = [
  { value: 'equals', label: 'equals' },
  { value: 'not_equals', label: 'does not equal' },
  { value: 'contains', label: 'contains' },
  { value: 'empty', label: 'is empty' },
  { value: 'not_empty', label: 'is not empty' },
  { value: 'true', label: 'is true' },
  { value: 'false', label: 'is false' },
  { value: 'gt', label: 'is greater than' },
  { value: 'lt', label: 'is less than' },
]

export function checkCondition(c: Condition | undefined, scope: Scope): boolean {
  if (!c || !c.left) return true
  const l = resolve(c.left, scope)
  const r = resolve(c.right ?? '', scope)
  const empty = l === null || l === undefined || l === '' || (Array.isArray(l) && l.length === 0)
  switch (c.op) {
    case 'equals': return toText(l) === toText(r)
    case 'not_equals': return toText(l) !== toText(r)
    case 'contains': return toText(l).toLowerCase().includes(toText(r).toLowerCase())
    case 'empty': return empty
    case 'not_empty': return !empty
    case 'true': return l === true || l === 'true' || l === 'Yes'
    case 'false': return !(l === true || l === 'true' || l === 'Yes')
    case 'gt': return Number(l) > Number(r)
    case 'lt': return Number(l) < Number(r)
  }
  return true
}
