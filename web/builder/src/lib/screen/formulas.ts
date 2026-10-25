// Formulas on screens: {{ = formula }} in any setting, "Show when" and the
// conditions that change a component's settings. They run in the browser
// with the same language as table formulas.
//
// Names read the screen's scope: row.Name, form.Email, user.email,
// state.search, url.id, data.total, device. [Field] is a field of the
// current row, and a.[name with spaces] reaches names with spaces.
import { compile, evaluate, formatLayout, FormulaError, type Checked, type Type, type Value } from '../formula/formula.ts'
import type { Scope } from './types'

export const scopeNames = ['user', 'app', 'url', 'query', 'state', 'data', 'row', 'index', 'form', 'result', 'device', 'theme']

const known = new Set(scopeNames)

const screenScope = {
  fieldType: (): Type => 'any',
  nameType: (parts: string[]): Type | undefined => (known.has(parts[0].toLowerCase()) ? 'any' : undefined),
}

const cache = new Map<string, Checked | FormulaError>()

function compiled(src: string): Checked {
  let c = cache.get(src)
  if (!c) {
    try {
      c = compile(src, screenScope)
    } catch (e) {
      c = e instanceof FormulaError ? e : new FormulaError(String(e))
    }
    if (cache.size > 500) cache.clear()
    cache.set(src, c)
  }
  if (c instanceof FormulaError) throw c
  return c
}

// formulaProblem checks a formula as typed in the designer: "" when it is
// fine, else what is wrong.
export function formulaProblem(src: string): FormulaError | null {
  if (!src.trim()) return null
  try {
    compiled(src)
    return null
  } catch (e) {
    return e instanceof FormulaError ? e : new FormulaError(String(e))
  }
}

// get reads a key, ignoring case when there is no exact match.
function get(obj: any, key: string): any {
  if (obj === null || obj === undefined || typeof obj !== 'object') return undefined
  if (Array.isArray(obj) && /^\d+$/.test(key)) return obj[Number(key)]
  if (key in obj) return obj[key]
  const lower = key.toLowerCase()
  for (const k of Object.keys(obj)) if (k.toLowerCase() === lower) return obj[k]
  return undefined
}

// toFormula turns screen data into formula values: linked rows become
// their labels and files their names, as in table formulas.
function toFormula(v: any): Value {
  if (v === null || v === undefined) return null
  if (typeof v === 'number' || typeof v === 'string' || typeof v === 'boolean' || v instanceof Date) return v
  if (Array.isArray(v)) return v.map(toFormula)
  if (typeof v === 'object') {
    if ('_id' in v && 'label' in v) return v.hidden ? '' : String(v.label ?? '')
    if ('name' in v && 'url' in v) return String(v.name)
  }
  return v
}

// fromFormula turns a result back into screen data: dates become ISO
// text, which components show as dates.
function fromFormula(v: Value): any {
  if (v instanceof Date) return formatLayout(v)
  if (Array.isArray(v)) return v.map(fromFormula)
  return v
}

// runFormula evaluates a formula in a scope. It throws when the formula is
// wrong or fails.
export function runFormula(src: string, scope: Scope): any {
  const c = compiled(src)
  const env = {
    field: (name: string) => toFormula(get(scope.row, name)),
    name: (parts: string[]) => {
      let v: any = scope
      for (const p of parts) v = get(v, p)
      return toFormula(v)
    },
    now: () => new Date(),
  }
  return fromFormula(evaluate(c, env))
}

// isTrue runs a condition; a condition that fails counts as false.
export function isTrue(src: string | undefined, scope: Scope): boolean {
  if (!src || !src.trim()) return true
  try {
    const v = runFormula(src, scope)
    return v === true || (typeof v === 'number' && v !== 0) || (typeof v === 'string' && ['true', 'yes', '1'].includes(v.trim().toLowerCase())) || (Array.isArray(v) && v.length > 0)
  } catch {
    return false
  }
}

// Condition changes some settings while its formula is true.
export interface SettingCondition {
  when: string
  set: Record<string, any>
}

// Settings a condition may not change: who receives the component is the
// server's business, and conditions do not change conditions.
export const fixedSettings = new Set(['roles', 'showWhen', 'visibleIf', 'conditions', 'actions'])

// applyConditions returns props with the settings of every true condition
// applied in order, so later conditions win.
export function applyConditions(props: Record<string, any>, conditions: SettingCondition[] | undefined, scope: Scope): Record<string, any> {
  if (!conditions?.length) return props
  let out = props
  for (const c of conditions) {
    if (!c?.when || !c.set || !isTrue(c.when, scope)) continue
    out = { ...out }
    for (const [k, v] of Object.entries(c.set)) {
      if (fixedSettings.has(k)) continue
      // "style:color" changes one style value of the Normal layer.
      if (k.startsWith('style:')) out.style = { ...out.style, base: { ...out.style?.base, [k.slice(6)]: v } }
      else out[k] = v
    }
  }
  return out
}

// formulaName writes a binding path as a formula name: row.[First name].
export const formulaName = (path: string) =>
  path.split('|')[0].trim().split('.').map((p, i) => (i > 0 && !/^[A-Za-z_][A-Za-z0-9_]*$/.test(p) ? `[${p}]` : p)).join('.')

// conditionFormula rewrites an older "Show only when" rule as a formula,
// or gives "" when its text mixes words and bindings.
export function conditionFormula(c: { left: string; op: string; right?: string }): string {
  const operand = (s: string, numeric: boolean): string | null => {
    const t = (s ?? '').trim()
    const one = t.match(/^\{\{\s*([^}|]*?)\s*\}\}$/)
    if (one) return one[1].startsWith('=') ? `(${one[1].slice(1).trim()})` : formulaName(one[1])
    if (t.includes('{{')) return null
    if (numeric && /^-?\d+(\.\d+)?$/.test(t)) return t
    return `"${t.replaceAll('"', '""')}"`
  }
  const l = operand(c.left, false)
  const r = operand(c.right ?? '', c.op === 'gt' || c.op === 'lt')
  if (l === null || r === null) return ''
  switch (c.op) {
    case 'equals': return `${l} = ${r}`
    case 'not_equals': return `${l} <> ${r}`
    case 'contains': return `CONTAINS(LOWER(${l}), LOWER(${r}))`
    case 'empty': return `ISBLANK(${l})`
    case 'not_empty': return `NOT ISBLANK(${l})`
    case 'true': return `IFERROR(IF(${l}, TRUE, FALSE), FALSE)`
    case 'false': return `NOT IFERROR(IF(${l}, TRUE, FALSE), FALSE)`
    case 'gt': return `${l} > ${r}`
    case 'lt': return `${l} < ${r}`
  }
  return ''
}
