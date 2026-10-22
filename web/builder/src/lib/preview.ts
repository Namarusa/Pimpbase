// Previews of {{ bindings }} against sample data: the value a binding
// gives, the text a template becomes, and the paths the data lacks.
import { parsePath, walk } from './paths.ts'

export interface Preview {
  // kind: none (no bindings), value (one binding alone, keeps its type),
  // text (bindings within text), blocks ({{#each}} or {{#if}}), formula.
  kind: 'none' | 'value' | 'text' | 'blocks' | 'formula'
  value?: any
  text?: string
  found?: boolean
  // missing lists the paths the sample does not have.
  missing: string[]
  error?: string
}

const bindingRe = /\{\{([\s\S]*?)\}\}/g

// preview fills a step text with the sample.
export function preview(src: string, scope: Record<string, any> | null): Preview {
  if (!src || !src.includes('{{')) return { kind: 'none', missing: [] }
  if (!scope) return { kind: 'none', missing: [] }
  if (/\{\{\s*[#/]/.test(src)) return { kind: 'blocks', missing: [] }
  const missing: string[] = []
  let formula = false
  let error = ''
  const one = (expr: string): any => {
    const e = expr.trim()
    if (e.startsWith('=')) { formula = true; return undefined }
    const [path, ...pipes] = splitPipes(e)
    const segs = parsePath(path.trim())
    if (!segs || !segs.length) { error = `{{${expr}}} is not a binding path`; return undefined }
    const r = walk(scope, segs)
    if (!r.found && !known(segs, scope)) missing.push(path.trim())
    let v = r.value
    for (const p of pipes) v = pipe(v, p)
    return v
  }
  const whole = src.match(/^\s*\{\{((?:(?!\}\})[\s\S])*)\}\}\s*$/)
  if (whole) {
    const v = one(whole[1])
    if (formula) return { kind: 'formula', missing }
    return { kind: 'value', value: v, found: !missing.length, missing, error: error || undefined }
  }
  const text = src.replace(bindingRe, (_, e) => toText(one(e)))
  if (formula) return { kind: 'formula', missing }
  return { kind: 'text', text, missing, error: error || undefined }
}

// known is true for paths the sample cannot hold yet but are still right,
// such as a step that has not run in the sample run.
function known(segs: string[], scope: Record<string, any>): boolean {
  if (segs[0] === 'steps' && segs.length > 1) return !(segs[1] in (scope.steps ?? {}))
  if (segs[0] === 'user' || segs[0] === 'vars') return true
  if (segs[0] === 'trigger') return !scope.trigger || !Object.keys(scope.trigger).length
  return false
}

function splitPipes(s: string): string[] {
  const out: string[] = []
  let start = 0, quoted = false
  for (let i = 0; i < s.length; i++) {
    if (s[i] === '"') quoted = !quoted
    else if (s[i] === '|' && !quoted) { out.push(s.slice(start, i)); start = i + 1 }
  }
  out.push(s.slice(start))
  return out
}

function pipe(v: any, raw: string): any {
  const m = raw.trim().match(/^(\w+)(?::\s*"?([\s\S]*?)"?)?$/)
  if (!m) return v
  const [, name, arg] = m
  switch (name) {
    case 'upper': return toText(v).toUpperCase()
    case 'lower': return toText(v).toLowerCase()
    case 'trim': return toText(v).trim()
    case 'default': return v === null || v === undefined || v === '' ? arg ?? '' : v
    case 'json': return JSON.stringify(v ?? null)
    case 'join': return Array.isArray(v) ? v.map(toText).join(arg ?? ',') : v
    case 'count': return Array.isArray(v) ? v.length : v ? 1 : 0
    case 'first': return Array.isArray(v) ? v[0] : v
    case 'number': return v === null || v === undefined || v === '' || isNaN(Number(v)) ? v : Number(v).toFixed(Number(arg ?? 0))
  }
  return v
}

// toText is how a value lands inside text.
export function toText(v: any): string {
  if (v === null || v === undefined) return ''
  if (typeof v === 'string') return v
  if (typeof v === 'number' || typeof v === 'boolean') return String(v)
  return JSON.stringify(v)
}

// describe is a value in a few words, for previews.
export function describe(v: any, max = 60): string {
  if (v === undefined) return '—'
  if (v === null) return 'empty'
  if (Array.isArray(v)) return `list · ${v.length} item${v.length === 1 ? '' : 's'}`
  if (typeof v === 'object') {
    const keys = Object.keys(v)
    const shown = keys.slice(0, 4).join(', ')
    return `{ ${shown}${keys.length > 4 ? ', …' : ''} }`
  }
  if (typeof v === 'string') return JSON.stringify(v.length > max ? v.slice(0, max) + '…' : v)
  return String(v)
}

export const kindOf = (v: any): string =>
  v === null ? 'empty' : Array.isArray(v) ? 'list' : typeof v === 'object' ? 'object' : typeof v === 'string' ? 'text' : typeof v === 'number' ? 'number' : typeof v === 'boolean' ? 'yes/no' : ''
