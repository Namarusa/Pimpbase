// The formula language in the browser: the same syntax, types and results
// as internal/formula in Go, so a formula in a screen means what it means
// in a table. Both are tested against internal/formula/testdata/vectors.json.
//
// Values are null (blank), number, string, boolean, Date or arrays of them.

export type Type = 'any' | 'blank' | 'number' | 'text' | 'boolean' | 'date' | 'list'
export type Value = null | number | string | boolean | Date | Value[] | object

export class FormulaError extends Error {
  start: number
  end: number
  constructor(message: string, start = 0, end = 0) {
    super(message)
    this.start = start
    this.end = end
  }
}

// Scope tells the checker what names mean.
export interface Scope {
  fieldType(name: string): Type | undefined
  nameType(parts: string[]): Type | undefined
}

// Env supplies values while a formula runs.
export interface Env {
  field(name: string): Value
  name(parts: string[]): Value
  now(): Date
}

// ---------------------------------------------------------------- lexer

type TokKind = 'eof' | 'number' | 'string' | 'field' | 'ident' | '(' | ')' | ',' | '.' | 'op'
interface Token { kind: TokKind; text: string; start: number; end: number }

const isDigit = (c: string) => c >= '0' && c <= '9'
const isIdentStart = (c: string) => c === '_' || /[A-Za-z]/.test(c) || c.charCodeAt(0) >= 0x80
const isIdentPart = (c: string) => isIdentStart(c) || isDigit(c)

function lex(src: string): Token[] {
  const toks: Token[] = []
  let i = 0
  while (i < src.length) {
    const c = src[i]
    if (c === ' ' || c === '\t' || c === '\n' || c === '\r') {
      i++
    } else if (isDigit(c) || (c === '.' && i + 1 < src.length && isDigit(src[i + 1]))) {
      const start = i
      while (i < src.length && (isDigit(src[i]) || src[i] === '.')) i++
      if (i < src.length && (src[i] === 'e' || src[i] === 'E')) {
        let j = i + 1
        if (j < src.length && (src[j] === '+' || src[j] === '-')) j++
        if (j < src.length && isDigit(src[j])) {
          i = j
          while (i < src.length && isDigit(src[i])) i++
        }
      }
      toks.push({ kind: 'number', text: src.slice(start, i), start, end: i })
    } else if (c === '"') {
      const start = i
      i++
      let s = ''
      let closed = false
      while (i < src.length) {
        if (src[i] === '"') {
          if (src[i + 1] === '"') {
            s += '"'
            i += 2
            continue
          }
          i++
          closed = true
          break
        }
        s += src[i++]
      }
      if (!closed) throw new FormulaError('text is missing its closing quote', start, src.length)
      toks.push({ kind: 'string', text: s, start, end: i })
    } else if (c === '[') {
      const start = i
      const end = src.indexOf(']', i)
      if (end < 0) throw new FormulaError('field reference is missing its closing ]', start, src.length)
      const name = src.slice(i + 1, end).trim()
      if (name === '') throw new FormulaError('empty field reference', start, end + 1)
      i = end + 1
      toks.push({ kind: 'field', text: name, start, end: i })
    } else if (c === '(' || c === ')' || c === ',' || c === '.') {
      toks.push({ kind: c, text: c, start: i, end: i + 1 })
      i++
    } else if ('+-*/^&'.includes(c)) {
      toks.push({ kind: 'op', text: c, start: i, end: i + 1 })
      i++
    } else if (c === '=') {
      const n = src[i + 1] === '=' ? 2 : 1
      toks.push({ kind: 'op', text: '=', start: i, end: i + n })
      i += n
    } else if (c === '!' && src[i + 1] === '=') {
      toks.push({ kind: 'op', text: '<>', start: i, end: i + 2 })
      i += 2
    } else if (c === '<' || c === '>') {
      let op = c
      if (src[i + 1] === '=' || (c === '<' && src[i + 1] === '>')) op += src[i + 1]
      toks.push({ kind: 'op', text: op, start: i, end: i + op.length })
      i += op.length
    } else if (isIdentStart(c)) {
      const start = i
      while (i < src.length && isIdentPart(src[i])) i++
      toks.push({ kind: 'ident', text: src.slice(start, i), start, end: i })
    } else {
      throw new FormulaError(`unexpected character "${c}"`, i, i + 1)
    }
  }
  toks.push({ kind: 'eof', text: '', start: src.length, end: src.length })
  return toks
}

// ---------------------------------------------------------------- parser

export type Node =
  | { k: 'num'; v: number; s: number; e: number }
  | { k: 'str'; v: string; s: number; e: number }
  | { k: 'bool'; v: boolean; s: number; e: number }
  | { k: 'null'; s: number; e: number }
  | { k: 'field'; name: string; s: number; e: number }
  | { k: 'link'; link: string; field: string; s: number; e: number }
  | { k: 'name'; parts: string[]; s: number; e: number }
  | { k: 'unary'; op: '-' | 'NOT'; x: Node; s: number; e: number }
  | { k: 'binary'; op: string; l: Node; r: Node; s: number; e: number }
  | { k: 'call'; name: string; args: Node[]; s: number; e: number }
  | { k: 'in'; x: Node; list: Node[]; not: boolean; s: number; e: number }

const BP = { or: 1, and: 2, not: 3, compare: 4, concat: 5, add: 6, mul: 7, pow: 8, unary: 9 }

function keyword(t: Token): string {
  if (t.kind !== 'ident') return ''
  const k = t.text.toUpperCase()
  return ['AND', 'OR', 'NOT', 'IN', 'TRUE', 'FALSE', 'NULL'].includes(k) ? k : ''
}

function infix(t: Token): [string, number] {
  if (t.kind === 'op') {
    switch (t.text) {
      case '=': case '<>': case '<': case '<=': case '>': case '>=': return [t.text, BP.compare]
      case '&': return ['&', BP.concat]
      case '+': case '-': return [t.text, BP.add]
      case '*': case '/': return [t.text, BP.mul]
      case '^': return ['^', BP.pow]
    }
  }
  switch (keyword(t)) {
    case 'OR': return ['OR', BP.or]
    case 'AND': return ['AND', BP.and]
    case 'IN': return ['IN', BP.compare]
    case 'NOT': return ['NOT IN', BP.compare]
  }
  return ['', 0]
}

export function parse(src: string): Node {
  const toks = lex(src)
  let pos = 0
  const peek = () => toks[pos]
  const next = () => {
    const t = toks[pos]
    if (t.kind !== 'eof') pos++
    return t
  }
  const text = (t: Token) => src.slice(t.start, t.end)

  function parenList(): [Node[], number] {
    const t = next()
    if (t.kind !== '(') throw new FormulaError('expected ( to start a list', t.start, t.end)
    const items: Node[] = []
    if (peek().kind === ')') return [items, next().end]
    for (;;) {
      items.push(expr(0))
      const t = next()
      if (t.kind === ')') return [items, t.end]
      if (t.kind !== ',') throw new FormulaError('expected , or )', t.start, t.end)
    }
  }

  function expr(minBP: number): Node {
    let left = prefix()
    for (;;) {
      const t = peek()
      const [op, bp] = infix(t)
      if (bp === 0 || bp <= minBP) return left
      next()
      if (op === 'IN' || op === 'NOT IN') {
        if (op === 'NOT IN') {
          if (keyword(peek()) !== 'IN') throw new FormulaError('expected IN after NOT', peek().start, peek().end)
          next()
        }
        const [list, end] = parenList()
        left = { k: 'in', x: left, list, not: op === 'NOT IN', s: left.s, e: end }
        continue
      }
      const right = expr(op === '^' ? bp - 1 : bp)
      left = { k: 'binary', op, l: left, r: right, s: left.s, e: right.e }
    }
  }

  function prefix(): Node {
    const t = next()
    switch (t.kind) {
      case 'number': {
        const v = Number(t.text)
        if (!/^(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?$/.test(t.text) || Number.isNaN(v)) throw new FormulaError(`invalid number "${t.text}"`, t.start, t.end)
        return { k: 'num', v, s: t.start, e: t.end }
      }
      case 'string':
        return { k: 'str', v: t.text, s: t.start, e: t.end }
      case 'field':
        if (peek().kind === '.' && toks[pos + 1].kind === 'field') {
          next()
          const f = next()
          return { k: 'link', link: t.text, field: f.text, s: t.start, e: f.end }
        }
        return { k: 'field', name: t.text, s: t.start, e: t.end }
      case '(': {
        const n = expr(0)
        const c = next()
        if (c.kind !== ')') throw new FormulaError('expected )', c.start, c.end)
        return n
      }
      case 'op':
        if (t.text === '-' || t.text === '+') {
          const x = expr(BP.unary - 1)
          return t.text === '+' ? x : { k: 'unary', op: '-', x, s: t.start, e: x.e }
        }
        break
      case 'ident': {
        const kw = keyword(t)
        if (kw === 'TRUE' || kw === 'FALSE') return { k: 'bool', v: kw === 'TRUE', s: t.start, e: t.end }
        if (kw === 'NULL') return { k: 'null', s: t.start, e: t.end }
        if (kw === 'NOT' && peek().kind !== '(') {
          const x = expr(BP.not)
          return { k: 'unary', op: 'NOT', x, s: t.start, e: x.e }
        }
        if (peek().kind === '(') {
          const [args, end] = parenList()
          return { k: 'call', name: t.text.toUpperCase(), args, s: t.start, e: end }
        }
        const parts = [t.text]
        let end = t.end
        while (peek().kind === '.') {
          next()
          const id = next()
          if (id.kind !== 'ident' && id.kind !== 'field') throw new FormulaError('expected a name after .', id.start, id.end)
          parts.push(id.text)
          end = id.end
        }
        return { k: 'name', parts, s: t.start, e: end }
      }
      case 'eof':
        throw new FormulaError('the formula ends too early', t.start, t.end)
    }
    throw new FormulaError(`unexpected "${t.text}"`, t.start, t.end)
  }

  if (peek().kind === 'op' && peek().text === '=') next()
  if (peek().kind === 'eof') throw new FormulaError('the formula is empty', 0, src.length)
  const n = expr(0)
  if (peek().kind !== 'eof') throw new FormulaError(`unexpected "${text(peek())}"`, peek().start, peek().end)
  return n
}

// ---------------------------------------------------------------- values

export const DATE_LAYOUT_RE = /^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$/

const pad2 = (n: number) => String(n).padStart(2, '0')
const pad4 = (n: number) => (n < 0 ? '-' + String(-n).padStart(4, '0') : String(n).padStart(4, '0'))

// formatLayout writes a date as 2006-01-02T15:04:05Z.
export function formatLayout(d: Date): string {
  return `${pad4(d.getUTCFullYear())}-${pad2(d.getUTCMonth() + 1)}-${pad2(d.getUTCDate())}T${pad2(d.getUTCHours())}:${pad2(d.getUTCMinutes())}:${pad2(d.getUTCSeconds())}Z`
}

// formatDate shows just the day when there is no time of day.
export function formatDate(d: Date): string {
  const s = formatLayout(d)
  return s.endsWith('T00:00:00Z') ? s.slice(0, 10) : s
}

// formatNumber prints a number with no exponent and no trailing zeros.
export function formatNumber(f: number): string {
  if (!Number.isFinite(f)) return ''
  if (Object.is(f, -0)) return '-0' // as Go prints it
  const s = String(f)
  const m = s.match(/^(-?)(\d)(?:\.(\d+))?e([+-]\d+)$/)
  if (!m) return s
  const [, sign, d0, rest = '', expS] = m
  const exp = Number(expS)
  const digits = d0 + rest
  if (exp >= 0) {
    const whole = digits.padEnd(exp + 1, '0')
    const intPart = whole.slice(0, exp + 1)
    const frac = whole.slice(exp + 1)
    return sign + intPart + (frac ? '.' + frac : '')
  }
  return sign + '0.' + '0'.repeat(-exp - 1) + digits
}

const dateLayouts: RegExp[] = [
  // RFC3339 with optional fraction and a zone.
  /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(\.\d+)?(Z|[+-]\d\d:\d\d)$/,
  /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)()()$/,
  /^(\d{4})-(\d\d)-(\d\d) (\d\d):(\d\d):(\d\d)()()$/,
  /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d)()()()$/,
  /^(\d{4})-(\d\d)-(\d\d)()()()()()$/,
]

// parseDate accepts a full timestamp or a plain day, as Go's ParseDate.
export function parseDate(s: string): Date {
  s = s.trim()
  for (const re of dateLayouts) {
    const m = s.match(re)
    if (!m) continue
    const [y, mo, d, h = 0, mi = 0, se = 0] = m.slice(1, 7).map((x) => (x === '' || x === undefined ? 0 : Number(x)))
    if (mo < 1 || mo > 12 || d < 1 || d > daysIn(y, mo) || h > 23 || mi > 59 || se > 59) continue
    let ms = utc(y, mo - 1, d, h, mi, se)
    const zone = m[8]
    if (zone && zone !== 'Z') {
      const sign = zone[0] === '-' ? -1 : 1
      const zh = Number(zone.slice(1, 3))
      const zm = Number(zone.slice(4, 6))
      if (zh > 23 || zm > 59) continue
      ms -= sign * (zh * 60 + zm) * 60000
    }
    return new Date(ms)
  }
  throw new Error(`"${s}" is not a date (use YYYY-MM-DD)`)
}

function utc(y: number, mo: number, d: number, h = 0, mi = 0, s = 0): number {
  const dt = new Date(0)
  dt.setUTCFullYear(y, mo, d)
  dt.setUTCHours(h, mi, s, 0)
  return dt.getTime()
}

const daysIn = (y: number, m: number) => new Date(utc(y, m, 0)).getUTCDate()

export function typeOf(v: Value): Type {
  if (v === null || v === undefined) return 'blank'
  if (typeof v === 'number') return 'number'
  if (typeof v === 'string') return 'text'
  if (typeof v === 'boolean') return 'boolean'
  if (v instanceof Date) return 'date'
  if (Array.isArray(v)) return 'list'
  return 'any'
}

const typeName = (t: Type) => ({ any: 'Any', blank: 'Blank', number: 'Number', text: 'Text', boolean: 'Boolean', date: 'Date', list: 'List' })[t]

const numberText = /^[+-]?([0-9]+\.?[0-9]*|\.[0-9]+)([eE][+-]?[0-9]+)?$/

export function parseNumber(s: string): number | undefined {
  s = s.trim()
  if (!numberText.test(s)) return undefined
  return Number(s)
}

function toNumber(v: Value): number {
  if (v === null || v === undefined) return 0
  if (typeof v === 'number') return v
  if (typeof v === 'boolean') return v ? 1 : 0
  if (typeof v === 'string') {
    if (v.trim() === '') return 0
    const f = parseNumber(v)
    if (f === undefined) throw new Error(`expected a number, got "${v}"`)
    return f
  }
  throw new Error(`expected a number, got a ${typeName(typeOf(v))}`)
}

export function toText(v: Value): string {
  if (v === null || v === undefined) return ''
  if (typeof v === 'number') return formatNumber(v)
  if (typeof v === 'string') return v
  if (typeof v === 'boolean') return v ? 'true' : 'false'
  if (v instanceof Date) return formatDate(v)
  if (Array.isArray(v)) return v.map(toText).join(', ')
  return JSON.stringify(v)
}

export function toBool(v: Value): boolean {
  if (v === null || v === undefined) return false
  if (typeof v === 'boolean') return v
  if (typeof v === 'number') return v !== 0
  if (typeof v === 'string') {
    switch (v.trim().toLowerCase()) {
      case '': case 'false': case '0': case 'no': return false
      case 'true': case '1': case 'yes': return true
    }
    throw new Error(`expected true or false, got "${v}"`)
  }
  if (Array.isArray(v)) return v.length > 0
  throw new Error(`expected true or false, got a ${typeName(typeOf(v))}`)
}

function toDate(v: Value): Date | null {
  if (v === null || v === undefined) return null
  if (v instanceof Date) return v
  if (typeof v === 'string') {
    if (v.trim() === '') return null
    return parseDate(v)
  }
  throw new Error(`expected a date, got a ${typeName(typeOf(v))}`)
}

const toList = (v: Value): Value[] => (v === null || v === undefined ? [] : Array.isArray(v) ? v : [v])

function zeroOf(t: Type): Value | undefined {
  switch (t) {
    case 'number': return 0
    case 'text': return ''
    case 'boolean': return false
  }
  return undefined
}

const unixSeconds = (d: Date) => Math.floor(d.getTime() / 1000)
const cmp = (a: number, b: number) => (a < b ? -1 : a > b ? 1 : 0)

// compareText orders by code points, as Go compares UTF-8 bytes.
function compareText(a: string, b: string): number {
  if (a === b) return 0
  const x = Array.from(a)
  const y = Array.from(b)
  for (let i = 0; i < Math.min(x.length, y.length); i++) {
    const c = cmp(x[i].codePointAt(0)!, y[i].codePointAt(0)!)
    if (c !== 0) return c
  }
  return cmp(x.length, y.length)
}

// compare orders a and b; known is false when a blank without a zero value
// is involved, and every comparison is then false.
function compare(a: Value, at: Type, b: Value, bt: Type): [number, boolean] {
  if (a === null || a === undefined) {
    const z = zeroOf(at)
    if (z === undefined) return [0, false]
    a = z
  }
  if (b === null || b === undefined) {
    const z = zeroOf(bt)
    if (z === undefined) return [0, false]
    b = z
  }
  if (typeof a === 'number') return [cmp(a, toNumber(b)), true]
  if (typeof a === 'string') {
    if (typeof b === 'string') return [compareText(a, b), true]
    if (typeof b === 'number') {
      let x: number
      try {
        x = toNumber(a)
      } catch {
        throw new Error('cannot compare text with a number')
      }
      return [cmp(x, b), true]
    }
    if (b instanceof Date) {
      const x = toDate(a)
      if (!x) return [0, false]
      return [cmp(unixSeconds(x), unixSeconds(b)), true]
    }
    throw new Error(`cannot compare text with a ${typeName(typeOf(b))}`)
  }
  if (typeof a === 'boolean') return [cmp(a ? 1 : 0, toBool(b) ? 1 : 0), true]
  if (a instanceof Date) {
    const y = toDate(b)
    if (!y) return [0, false]
    return [cmp(unixSeconds(a), unixSeconds(y)), true]
  }
  throw new Error(`cannot compare a ${typeName(typeOf(a))}`)
}

function unify(a: Type, b: Type): Type {
  if (a === b) return a
  if (a === 'blank') return b
  if (b === 'blank') return a
  return 'any'
}

// ---------------------------------------------------------------- functions

interface Fn {
  params: Type[]
  min: number
  variadic?: boolean
  ret?: Type
  retFn?: (a: Type[]) => Type
  argType?: (i: number) => Type
  check?: (a: Type[]) => string
  rollup?: boolean
  eval?: (env: Env, a: Value[]) => Value
  lazy?: (ev: Evaluator, args: Node[]) => Value
}

const param = (f: Fn, i: number) => (f.argType ? f.argType(i) : i >= f.params.length ? f.params[f.params.length - 1] : f.params[i])
const result = (f: Fn, a: Type[]) => (f.retFn ? f.retFn(a) : f.ret ?? 'any')
function arity(f: Fn): string {
  if (f.variadic) return `at least ${f.min} argument(s)`
  if (f.min === f.params.length) return `${f.min} argument(s)`
  return `${f.min} to ${f.params.length} arguments`
}

const firstType = (a: Type[]) => a.reduce<Type>((t, x) => unify(t, x), 'blank')
const num1 = (fn: (x: number) => number) => (_: Env, a: Value[]) => fn(toNumber(a[0]))
const text1 = (fn: (s: string) => Value) => (_: Env, a: Value[]) => fn(toText(a[0]))
const runes = (s: string) => Array.from(s)

function numbers(args: Value[]): number[] {
  const out: number[] = []
  for (const a of args) for (const v of toList(a)) if (v !== null && v !== undefined) out.push(toNumber(v))
  return out
}

// roundHalfAway rounds half away from zero, as Go's math.Round.
function roundHalfAway(x: number): number {
  if (!Number.isFinite(x)) return x
  const r = Math.round(Math.abs(x))
  return x < 0 ? -r : r
}

const roundTo = (x: number, digits: number) => {
  const p = Math.pow(10, Math.trunc(digits))
  return roundHalfAway(x * p) / p
}

function runeSlice(s: string, start: number, n: number): string {
  const r = runes(s)
  if (start < 0) start = 0
  if (start > r.length) return ''
  let end = start + n
  if (n < 0 || end > r.length) end = r.length
  return r.slice(start, end).join('')
}

function unitOf(unit: string): string {
  const u = unit.toLowerCase()
  return u.endsWith('s') ? u.slice(0, -1) : u
}

const unitError = (unit: string) => new Error(`unknown unit "${unit}" (use days, weeks, months, years, hours, minutes or seconds)`)

function addMonths(t: Date, years: number, months: number, days: number): Date {
  const d = new Date(t.getTime())
  d.setUTCFullYear(t.getUTCFullYear() + years, t.getUTCMonth() + months, t.getUTCDate() + days)
  return d
}

function addDate(t: Date, n: number, unit: string): Date {
  const i = Math.trunc(n)
  switch (unitOf(unit)) {
    case 'second': return new Date(t.getTime() + n * 1000)
    case 'minute': return new Date(t.getTime() + n * 60000)
    case 'hour': return new Date(t.getTime() + n * 3600000)
    case 'day': return addMonths(t, 0, 0, i)
    case 'week': return addMonths(t, 0, 0, 7 * i)
    case 'month': return addMonths(t, 0, i, 0)
    case 'year': return addMonths(t, i, 0, 0)
  }
  throw unitError(unit)
}

function diffDates(a: Date, b: Date, unit: string): number {
  const ms = b.getTime() - a.getTime()
  switch (unitOf(unit)) {
    case 'second': return Math.trunc(ms / 1000)
    case 'minute': return Math.trunc(ms / 60000)
    case 'hour': return Math.trunc(ms / 3600000)
    case 'day': return Math.trunc(ms / 86400000)
    case 'week': return Math.trunc(ms / 86400000 / 7)
    case 'month':
    case 'year': {
      let months = (b.getUTCFullYear() - a.getUTCFullYear()) * 12 + (b.getUTCMonth() - a.getUTCMonth())
      if (months > 0 && addMonths(b, 0, -months, 0).getTime() < a.getTime()) months--
      else if (months < 0 && addMonths(b, 0, -months, 0).getTime() > a.getTime()) months++
      if (unit.toLowerCase().startsWith('year')) return Math.trunc(months / 12)
      return months
    }
  }
  throw unitError(unit)
}

const datePart = (fn: (d: Date) => number) => (_: Env, a: Value[]) => {
  const t = toDate(a[0])
  return t ? fn(t) : null
}

function isoWeek(d: Date): number {
  const day = (d.getUTCDay() + 6) % 7 // Monday 0
  const thursday = new Date(utc(d.getUTCFullYear(), d.getUTCMonth(), d.getUTCDate() - day + 3))
  const jan1 = utc(thursday.getUTCFullYear(), 0, 1)
  return Math.floor((thursday.getTime() - jan1) / 86400000 / 7) + 1
}

const dayNumber = (d: Date) => Math.floor(Math.floor(d.getTime() / 1000) / 86400)
const weekday = (d: number) => (((d + 3) % 7) + 7) % 7

function workdays(a: number, b: number): number {
  let sign = 1
  if (b < a) [a, b, sign] = [b, a, -1]
  const n = b - a + 1
  const weeks = Math.floor(n / 7)
  let count = weeks * 5
  for (let d = a + weeks * 7; d <= b; d++) if (weekday(d) < 5) count++
  return sign * count
}

function isBlank(v: Value): boolean {
  return v === null || v === undefined || v === '' || (Array.isArray(v) && v.length === 0)
}

const MAX_TEXT = 100_000

function padText(a: Value[], left: boolean): string {
  const s = toText(a[0])
  const want = Math.trunc(toNumber(a[1]))
  const p = a.length > 2 ? toText(a[2]) : ' '
  if (want > MAX_TEXT) throw new Error(`cannot pad to more than ${MAX_TEXT} characters`)
  const have = runes(s).length
  if (p === '' || have >= want) return s
  const pr = runes(p)
  let fill = ''
  for (let i = 0; i < want - have; i++) fill += pr[i % pr.length]
  return left ? fill + s : s + fill
}

function roundDir(a: Value[], fn: (x: number) => number): number {
  const x = toNumber(a[0])
  const d = a.length > 1 ? toNumber(a[1]) : 0
  const p = Math.pow(10, Math.trunc(d))
  let y = Math.abs(x) * p
  const r = roundHalfAway(y)
  if (Math.abs(y - r) <= 1e-9 * Math.max(1, y)) y = r
  y = fn(y) / p
  return x < 0 ? -y : y
}

const regexps = new Map<string, RegExp>()

// compileRegexp reads a pattern as Go's regexp does for the parts both
// share; leading flags such as (?i) become JavaScript flags.
function compileRegexp(p: string): RegExp {
  let re = regexps.get(p)
  if (re) return re
  let flags = 'u'
  let body = p
  const m = body.match(/^\(\?([ims]+)\)/)
  if (m) {
    for (const f of m[1]) if (!flags.includes(f)) flags += f
    body = body.slice(m[0].length)
  }
  try {
    re = new RegExp(body, flags)
  } catch (e) {
    throw new Error(`the pattern is not a valid regular expression (${(e as Error).message})`)
  }
  regexps.set(p, re)
  return re
}

function regexExtract(re: RegExp, s: string): Value {
  const m = s.match(new RegExp(re.source, re.flags.replace('g', '')))
  if (!m) return null
  if (m.length > 1) return m[1] === undefined ? null : m[1]
  return m[0]
}

function regexReplace(re: RegExp, s: string, repl: string): string {
  const g = new RegExp(re.source, re.flags.includes('g') ? re.flags : re.flags + 'g')
  return s.replace(g, (...m: any[]) => {
    const groups: string[] = []
    for (const x of m) {
      if (typeof x !== 'string' && x !== undefined) break
      groups.push(x ?? '')
    }
    let out = ''
    for (let i = 0; i < repl.length; i++) {
      const c = repl[i]
      if (c === '$' && i + 1 < repl.length) {
        const n = repl[i + 1]
        if (n === '$') {
          out += '$'
          i++
          continue
        }
        if (n >= '0' && n <= '9') {
          const k = Number(n)
          if (k < groups.length) out += groups[k]
          i++
          continue
        }
      }
      out += c
    }
    return out
  })
}

const monthNames = ['January', 'February', 'March', 'April', 'May', 'June', 'July', 'August', 'September', 'October', 'November', 'December']
const dayNames = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
const dateTokens = ['YYYY', 'MMMM', 'dddd', 'MMM', 'ddd', 'YY', 'MM', 'DD', 'HH', 'hh', 'mm', 'ss', 'M', 'D', 'H', 'h', 'm', 's', 'A', 'a']

interface Parts { y: number; mo: number; d: number; wd: number; h: number; mi: number; s: number }

function partsIn(t: Date, zone: string): Parts {
  if (!zone) return { y: t.getUTCFullYear(), mo: t.getUTCMonth() + 1, d: t.getUTCDate(), wd: t.getUTCDay(), h: t.getUTCHours(), mi: t.getUTCMinutes(), s: t.getUTCSeconds() }
  let f: Intl.DateTimeFormat
  try {
    f = new Intl.DateTimeFormat('en-US', { timeZone: zone, hourCycle: 'h23', year: 'numeric', month: 'numeric', day: 'numeric', hour: 'numeric', minute: 'numeric', second: 'numeric' })
  } catch {
    throw new Error(`unknown time zone "${zone}"`)
  }
  const p: Record<string, number> = {}
  for (const x of f.formatToParts(t)) if (x.type !== 'literal') p[x.type] = Number(x.value)
  const wd = new Date(utc(p.year, p.month - 1, p.day)).getUTCDay()
  return { y: p.year, mo: p.month, d: p.day, wd, h: p.hour % 24, mi: p.minute, s: p.second }
}

function formatDatePattern(t: Parts, pattern: string): string {
  let out = ''
  for (let i = 0; i < pattern.length; ) {
    if (pattern[i] === '[') {
      const j = pattern.indexOf(']', i)
      if (j > i) {
        out += pattern.slice(i + 1, j)
        i = j + 1
        continue
      }
    }
    const tok = dateTokens.find((k) => pattern.startsWith(k, i))
    if (!tok) {
      out += pattern[i++]
      continue
    }
    const h12 = t.h % 12 === 0 ? 12 : t.h % 12
    switch (tok) {
      case 'YYYY': out += pad4(t.y); break
      case 'YY': out += pad2(t.y % 100); break
      case 'MMMM': out += monthNames[t.mo - 1]; break
      case 'MMM': out += monthNames[t.mo - 1].slice(0, 3); break
      case 'MM': out += pad2(t.mo); break
      case 'M': out += t.mo; break
      case 'DD': out += pad2(t.d); break
      case 'D': out += t.d; break
      case 'dddd': out += dayNames[t.wd]; break
      case 'ddd': out += dayNames[t.wd].slice(0, 3); break
      case 'HH': out += pad2(t.h); break
      case 'H': out += t.h; break
      case 'hh': out += pad2(h12); break
      case 'h': out += h12; break
      case 'mm': out += pad2(t.mi); break
      case 'm': out += t.mi; break
      case 'ss': out += pad2(t.s); break
      case 's': out += t.s; break
      case 'A': out += t.h >= 12 ? 'PM' : 'AM'; break
      case 'a': out += t.h >= 12 ? 'pm' : 'am'; break
    }
    i += tok.length
  }
  return out
}

function formatNumberWith(x: number, decimals: number, sep: string): string {
  if (!Number.isFinite(x)) return ''
  x = roundTo(x, decimals)
  const s = Math.abs(x).toFixed(decimals)
  const dot = s.indexOf('.')
  const whole = dot >= 0 ? s.slice(0, dot) : s
  const frac = dot >= 0 ? s.slice(dot) : ''
  let out = x < 0 && s.replace(/[0.]/g, '') !== '' ? '-' : ''
  for (let i = 0; i < whole.length; i++) {
    if (i > 0 && (whole.length - i) % 3 === 0) out += sep
    out += whole[i]
  }
  return out + frac
}

function listItem(v: Value, i: number): Value {
  const l = toList(v)
  if (i < 0) i = l.length + i + 1
  if (i < 1 || i > l.length) return null
  return l[i - 1]
}

function sortList(l: Value[], desc: boolean): Value[] {
  const out = [...l]
  let allNum = true
  let allDate = true
  for (const v of out) {
    if (v === null || v === undefined) continue
    if (typeof v !== 'number') allNum = false
    if (!(v instanceof Date)) allDate = false
  }
  const idx = out.map((v, i) => ({ v, i }))
  idx.sort((x, y) => {
    const a = x.v
    const b = y.v
    const an = a === null || a === undefined
    const bn = b === null || b === undefined
    if (an !== bn) return an ? 1 : -1
    if (an) return x.i - y.i
    let c: number
    if (allNum) c = cmp(a as number, b as number)
    else if (allDate) c = cmp(unixSeconds(a as Date), unixSeconds(b as Date))
    else c = compareText(toText(a), toText(b))
    if (desc) c = -c
    return c !== 0 ? c : x.i - y.i
  })
  return idx.map((x) => x.v)
}

function randomUnit(): number {
  const b = new Uint8Array(6)
  crypto.getRandomValues(b)
  let u = 0
  for (const x of b) u = u * 256 + x
  return u / 2 ** 48
}

function uuid(): string {
  const b = new Uint8Array(16)
  crypto.getRandomValues(b)
  b[6] = (b[6] & 0x0f) | 0x40
  b[8] = (b[8] & 0x3f) | 0x80
  const h = Array.from(b, (x) => x.toString(16).padStart(2, '0')).join('')
  return `${h.slice(0, 8)}-${h.slice(8, 12)}-${h.slice(12, 16)}-${h.slice(16, 20)}-${h.slice(20)}`
}

const A: Type = 'any'
const N: Type = 'number'
const T: Type = 'text'
const B: Type = 'boolean'
const D: Type = 'date'

const functions: Record<string, Fn> = {
  // Logic
  IF: {
    params: [B, A, A], min: 2,
    retFn: (a) => (a.length < 3 ? unify(a[1], 'blank') : unify(a[1], a[2])),
    lazy: (ev, args) => {
      if (toBool(ev.eval(args[0]))) return ev.eval(args[1])
      return args.length > 2 ? ev.eval(args[2]) : null
    },
  },
  AND: {
    params: [B], min: 1, variadic: true, ret: B,
    lazy: (ev, args) => {
      for (const a of args) if (!toBool(ev.eval(a))) return false
      return true
    },
  },
  OR: {
    params: [B], min: 1, variadic: true, ret: B,
    lazy: (ev, args) => {
      for (const a of args) if (toBool(ev.eval(a))) return true
      return false
    },
  },
  NOT: { params: [B], min: 1, ret: B, eval: (_, a) => !toBool(a[0]) },
  ISBLANK: { params: [A], min: 1, ret: B, eval: (_, a) => a[0] === null || a[0] === '' || (Array.isArray(a[0]) && a[0].length === 0) },
  BLANKVALUE: { params: [A, A], min: 2, retFn: (a) => unify(a[0], a[1]), eval: (_, a) => (a[0] === null || a[0] === '' ? a[1] : a[0]) },
  IFERROR: {
    params: [A, A], min: 2, retFn: (a) => unify(a[0], a[1]),
    lazy: (ev, args) => {
      try {
        return ev.eval(args[0])
      } catch {
        return ev.eval(args[1])
      }
    },
  },
  SWITCH: {
    params: [A], min: 3, variadic: true,
    retFn: (a) => {
      let t: Type = 'blank'
      for (let i = 2; i < a.length; i += 2) t = unify(t, a[i])
      if (a.length % 2 === 0) t = unify(t, a[a.length - 1])
      return t
    },
    lazy: (ev, args) => {
      const x = ev.eval(args[0])
      const xt = ev.type(args[0])
      let i = 1
      for (; i + 1 < args.length; i += 2) {
        const [c, known] = compare(x, xt, ev.eval(args[i]), ev.type(args[i]))
        if (known && c === 0) return ev.eval(args[i + 1])
      }
      return i < args.length ? ev.eval(args[i]) : null
    },
  },
  IFS: {
    params: [B, A], min: 2, variadic: true,
    check: (a) => (a.length % 2 !== 0 ? 'IFS takes pairs of a condition and a value' : ''),
    argType: (i) => (i % 2 === 0 ? B : A),
    retFn: (a) => {
      let t: Type = 'blank'
      for (let i = 1; i < a.length; i += 2) t = unify(t, a[i])
      return t
    },
    lazy: (ev, args) => {
      for (let i = 0; i + 1 < args.length; i += 2) if (toBool(ev.eval(args[i]))) return ev.eval(args[i + 1])
      return null
    },
  },
  COALESCE: { params: [A], min: 1, variadic: true, retFn: firstType, eval: (_, a) => a.find((v) => !isBlank(v)) ?? null },
  ISNUMBER: { params: [A], min: 1, ret: B, eval: (_, a) => typeof a[0] === 'number' || (typeof a[0] === 'string' && parseNumber(a[0]) !== undefined) },

  // Text
  CONCAT: { params: [A], min: 1, variadic: true, ret: T, eval: (_, a) => a.map(toText).join('') },
  UPPER: { params: [A], min: 1, ret: T, eval: text1((s) => runes(s).map((c) => { const u = c.toUpperCase(); return runes(u).length === 1 ? u : c }).join('')) },
  LOWER: { params: [A], min: 1, ret: T, eval: text1((s) => runes(s).map((c) => { const u = c.toLowerCase(); return runes(u).length === 1 ? u : c }).join('')) },
  TRIM: { params: [A], min: 1, ret: T, eval: text1((s) => s.replace(/^ +| +$/g, '')) },
  LEN: { params: [A], min: 1, ret: N, eval: text1((s) => runes(s).length) },
  LEFT: { params: [A, N], min: 2, ret: T, eval: (_, a) => runeSlice(toText(a[0]), 0, Math.trunc(Math.max(toNumber(a[1]), 0))) },
  RIGHT: {
    params: [A, N], min: 2, ret: T,
    eval: (_, a) => {
      const n = toNumber(a[1])
      const s = toText(a[0])
      const l = runes(s).length
      const k = Math.min(Math.trunc(Math.max(n, 0)), l)
      return runeSlice(s, l - k, k)
    },
  },
  MID: { params: [A, N, N], min: 3, ret: T, eval: (_, a) => runeSlice(toText(a[0]), Math.trunc(toNumber(a[1])) - 1, Math.trunc(Math.max(toNumber(a[2]), 0))) },
  CONTAINS: {
    params: [A, A], min: 2, ret: B,
    eval: (_, a) => {
      if (Array.isArray(a[0])) {
        const want = toText(a[1])
        return a[0].some((v) => toText(v) === want)
      }
      return toText(a[0]).includes(toText(a[1]))
    },
  },
  STARTSWITH: { params: [A, A], min: 2, ret: B, eval: (_, a) => toText(a[0]).startsWith(toText(a[1])) },
  ENDSWITH: { params: [A, A], min: 2, ret: B, eval: (_, a) => toText(a[0]).endsWith(toText(a[1])) },
  REPLACE: {
    params: [A, A, A], min: 3, ret: T,
    eval: (_, a) => {
      const find = toText(a[1])
      return find === '' ? toText(a[0]) : toText(a[0]).split(find).join(toText(a[2]))
    },
  },
  TEXT: { params: [A], min: 1, ret: T, eval: (_, a) => toText(a[0]) },
  VALUE: { params: [A], min: 1, ret: N, eval: (_, a) => (a[0] === null || a[0] === '' ? null : toNumber(a[0])) },
  SPLIT: {
    params: [A, T], min: 2, ret: 'list',
    eval: (_, a) => {
      const s = toText(a[0])
      if (s === '') return []
      const sep = toText(a[1])
      return sep === '' ? runes(s) : s.split(sep)
    },
  },
  JOIN: { params: [A, T], min: 2, ret: T, eval: (_, a) => toList(a[0]).map(toText).join(toText(a[1])) },
  PROPER: {
    params: [A], min: 1, ret: T,
    eval: text1((s) => {
      let out = ''
      let prev = false
      for (const r of runes(s)) {
        out += prev ? r.toLowerCase() : r.toUpperCase()
        prev = /[\p{L}\p{Nd}']/u.test(r)
      }
      return out
    }),
  },
  PADLEFT: { params: [A, N, T], min: 2, ret: T, eval: (_, a) => padText(a, true) },
  PADRIGHT: { params: [A, N, T], min: 2, ret: T, eval: (_, a) => padText(a, false) },
  REPT: {
    params: [A, N], min: 2, ret: T,
    eval: (_, a) => {
      const n = toNumber(a[1])
      const s = toText(a[0])
      const k = Math.max(Math.trunc(n), 0)
      if (k * runes(s).length > MAX_TEXT) throw new Error(`REPT would make more than ${MAX_TEXT} characters`)
      return s.repeat(k)
    },
  },
  FIND: {
    params: [A, A, N], min: 2, ret: N,
    eval: (_, a) => {
      const start = a.length > 2 ? toNumber(a[2]) : 1
      const r = runes(toText(a[1]))
      const st = Math.max(Math.trunc(start), 1) - 1
      if (st > r.length) return 0
      const rest = r.slice(st).join('')
      const i = rest.indexOf(toText(a[0]))
      if (i < 0) return 0
      return st + runes(rest.slice(0, i)).length + 1
    },
  },
  REGEXMATCH: { params: [A, T], min: 2, ret: B, eval: (_, a) => { const re = compileRegexp(toText(a[1])); re.lastIndex = 0; return re.test(toText(a[0])) } },
  REGEXEXTRACT: { params: [A, T], min: 2, ret: T, eval: (_, a) => regexExtract(compileRegexp(toText(a[1])), toText(a[0])) },
  REGEXREPLACE: { params: [A, T, A], min: 3, ret: T, eval: (_, a) => regexReplace(compileRegexp(toText(a[1])), toText(a[0]), toText(a[2])) },
  FORMATDATE: {
    params: [D, T, T], min: 2, ret: T,
    eval: (_, a) => {
      const t = toDate(a[0])
      if (!t) return null
      const zone = a.length > 2 ? toText(a[2]) : ''
      return formatDatePattern(partsIn(t, zone), toText(a[1]))
    },
  },
  FORMATNUMBER: {
    params: [N, N, T], min: 1, ret: T,
    eval: (_, a) => {
      if (a[0] === null) return null
      const x = toNumber(a[0])
      const d = a.length > 1 ? toNumber(a[1]) : 0
      const sep = a.length > 2 ? toText(a[2]) : ','
      return formatNumberWith(x, Math.min(Math.max(Math.trunc(d), 0), 10), sep)
    },
  },

  // Numbers
  ROUND: { params: [N, N], min: 1, ret: N, eval: (_, a) => roundTo(toNumber(a[0]), a.length > 1 ? toNumber(a[1]) : 0) },
  FLOOR: { params: [N], min: 1, ret: N, eval: num1(Math.floor) },
  CEILING: { params: [N], min: 1, ret: N, eval: num1(Math.ceil) },
  ABS: { params: [N], min: 1, ret: N, eval: num1(Math.abs) },
  SQRT: {
    params: [N], min: 1, ret: N,
    eval: (_, a) => {
      const x = toNumber(a[0])
      if (x < 0) throw new Error('SQRT of a negative number')
      return Math.sqrt(x)
    },
  },
  POWER: { params: [N, N], min: 2, ret: N, eval: (_, a) => Math.pow(toNumber(a[0]), toNumber(a[1])) },
  MOD: {
    params: [N, N], min: 2, ret: N,
    eval: (_, a) => {
      const x = toNumber(a[0])
      const y = toNumber(a[1])
      return y === 0 ? null : x - y * Math.floor(x / y)
    },
  },
  SUM: { params: [A], min: 1, variadic: true, ret: N, eval: (_, a) => numbers(a).reduce((s, n) => s + n, 0) },
  AVERAGE: {
    params: [A], min: 1, variadic: true, ret: N,
    eval: (_, a) => {
      const ns = numbers(a)
      return ns.length ? ns.reduce((s, n) => s + n, 0) / ns.length : null
    },
  },
  MIN: { params: [A], min: 1, variadic: true, ret: N, eval: (_, a) => { const ns = numbers(a); return ns.length ? Math.min(...ns) : null } },
  MAX: { params: [A], min: 1, variadic: true, ret: N, eval: (_, a) => { const ns = numbers(a); return ns.length ? Math.max(...ns) : null } },
  COUNT: { params: [A], min: 1, ret: N, eval: (_, a) => toList(a[0]).filter((v) => v !== null && v !== '').length },
  ROUNDUP: { params: [N, N], min: 1, ret: N, eval: (_, a) => roundDir(a, Math.ceil) },
  ROUNDDOWN: { params: [N, N], min: 1, ret: N, eval: (_, a) => roundDir(a, Math.floor) },
  SIGN: { params: [N], min: 1, ret: N, eval: num1((x) => (x > 0 ? 1 : x < 0 ? -1 : 0)) },
  LN: {
    params: [N], min: 1, ret: N,
    eval: (_, a) => {
      const x = toNumber(a[0])
      if (x <= 0) throw new Error('LN needs a number above 0')
      return Math.log(x)
    },
  },
  LOG: {
    params: [N, N], min: 1, ret: N,
    eval: (_, a) => {
      const x = toNumber(a[0])
      const base = a.length > 1 ? toNumber(a[1]) : 10
      if (x <= 0 || base <= 0 || base === 1) throw new Error('LOG needs a number and a base above 0, and a base other than 1')
      return base === 10 ? Math.log10(x) : Math.log(x) / Math.log(base)
    },
  },
  EXP: { params: [N], min: 1, ret: N, eval: num1(Math.exp) },
  PI: { params: [], min: 0, ret: N, eval: () => Math.PI },
  RANDBETWEEN: {
    params: [N, N], min: 2, ret: N,
    eval: (_, a) => {
      const l = Math.ceil(toNumber(a[0]))
      const h = Math.floor(toNumber(a[1]))
      if (h < l) throw new Error('RANDBETWEEN needs low at most high')
      return l + Math.floor(randomUnit() * (h - l + 1))
    },
  },

  // Dates
  TODAY: { params: [], min: 0, ret: D, eval: (env) => new Date(Math.floor(env.now().getTime() / 86400000) * 86400000) },
  NOW: { params: [], min: 0, ret: D, eval: (env) => new Date(Math.floor(env.now().getTime() / 1000) * 1000) },
  DATE: {
    params: [N, N, N], min: 3, ret: D,
    eval: (_, a) => new Date(utc(Math.trunc(toNumber(a[0])), Math.trunc(toNumber(a[1])) - 1, Math.trunc(toNumber(a[2])))),
  },
  DATEVALUE: { params: [A], min: 1, ret: D, eval: (_, a) => toDate(a[0]) },
  YEAR: { params: [D], min: 1, ret: N, eval: datePart((t) => t.getUTCFullYear()) },
  MONTH: { params: [D], min: 1, ret: N, eval: datePart((t) => t.getUTCMonth() + 1) },
  DAY: { params: [D], min: 1, ret: N, eval: datePart((t) => t.getUTCDate()) },
  WEEKDAY: { params: [D], min: 1, ret: N, eval: datePart((t) => ((t.getUTCDay() + 6) % 7) + 1) },
  DATEADD: {
    params: [D, N, T], min: 3, ret: D,
    eval: (_, a) => {
      const t = toDate(a[0])
      if (!t) return null
      return addDate(t, toNumber(a[1]), toText(a[2]))
    },
  },
  DATEDIFF: {
    params: [D, D, T], min: 3, ret: N,
    eval: (_, a) => {
      const s = toDate(a[0])
      const e = toDate(a[1])
      if (!s || !e) return null
      return diffDates(s, e, toText(a[2]))
    },
  },
  EOMONTH: {
    params: [D, N], min: 1, ret: D,
    eval: (_, a) => {
      const t = toDate(a[0])
      if (!t) return null
      const m = a.length > 1 ? Math.trunc(toNumber(a[1])) : 0
      return new Date(utc(t.getUTCFullYear(), t.getUTCMonth() + m + 1, 0))
    },
  },
  WEEKNUM: { params: [D], min: 1, ret: N, eval: datePart(isoWeek) },
  QUARTER: { params: [D], min: 1, ret: N, eval: datePart((t) => Math.floor(t.getUTCMonth() / 3) + 1) },
  HOUR: { params: [D], min: 1, ret: N, eval: datePart((t) => t.getUTCHours()) },
  MINUTE: { params: [D], min: 1, ret: N, eval: datePart((t) => t.getUTCMinutes()) },
  WORKDAYS: {
    params: [D, D], min: 2, ret: N,
    eval: (_, a) => {
      const s = toDate(a[0])
      const e = toDate(a[1])
      if (!s || !e) return null
      return workdays(dayNumber(s), dayNumber(e))
    },
  },
  ADDWORKDAYS: {
    params: [D, N], min: 2, ret: D,
    eval: (_, a) => {
      const t = toDate(a[0])
      if (!t) return null
      let k = Math.trunc(toNumber(a[1]))
      if (k > 100000 || k < -100000) throw new Error('ADDWORKDAYS takes at most 100000 days')
      let d = dayNumber(t)
      let step = 1
      if (k < 0) [step, k] = [-1, -k]
      while (k > 0) {
        d += step
        if (weekday(d) < 5) k--
      }
      return new Date(d * 86400000)
    },
  },

  // Lists
  LIST: { params: [A], min: 0, variadic: true, ret: 'list', eval: (_, a) => [...a] },
  FIRST: { params: [A], min: 1, ret: A, eval: (_, a) => listItem(a[0], 1) },
  LAST: { params: [A], min: 1, ret: A, eval: (_, a) => listItem(a[0], -1) },
  INDEX: { params: [A, N], min: 2, ret: A, eval: (_, a) => listItem(a[0], Math.trunc(toNumber(a[1]))) },
  SLICE: {
    params: [A, N, N], min: 2, ret: 'list',
    eval: (_, a) => {
      const l = toList(a[0])
      const i = Math.min(Math.max(Math.trunc(toNumber(a[1])), 1) - 1, l.length)
      let j = l.length
      if (a.length > 2) j = Math.min(j, i + Math.max(Math.trunc(toNumber(a[2])), 0))
      return l.slice(i, j)
    },
  },
  SORT: { params: [A, B], min: 1, ret: 'list', eval: (_, a) => sortList(toList(a[0]), a.length > 1 ? toBool(a[1]) : false) },
  UNIQUE: {
    params: [A], min: 1, ret: 'list',
    eval: (_, a) => {
      const seen = new Set<string>()
      const out: Value[] = []
      for (const v of toList(a[0])) {
        const k = typeOf(v) + ':' + toText(v)
        if (!seen.has(k)) {
          seen.add(k)
          out.push(v)
        }
      }
      return out
    },
  },

  // Other
  UUID: { params: [], min: 0, ret: T, eval: () => uuid() },
}

// Rollups read linked table rows, which only tables have.
for (const name of ['SUMOF', 'AVERAGEOF', 'MINOF', 'MAXOF', 'COUNTOF']) {
  const max = name === 'COUNTOF' ? 2 : 3
  functions[name] = { params: Array(max).fill(A), min: max - 1, ret: N, rollup: true }
}

export const functionNames = Object.keys(functions).sort()

// ---------------------------------------------------------------- checker

export interface Checked {
  source: string
  root: Node
  type: Type
  types: Map<Node, Type>
}

const accepts = (want: Type, got: Type) => want === 'any' || got === 'any' || got === 'blank' || want === got

function comparable(a: Type, b: Type): boolean {
  if (a === 'list' || b === 'list') return false
  return a === b || a === 'any' || b === 'any' || a === 'blank' || b === 'blank' ||
    (a === 'text' && (b === 'date' || b === 'number')) || (b === 'text' && (a === 'date' || a === 'number'))
}

// check types a parsed formula. It throws the first problem found.
export function check(src: string, root: Node, scope: Scope): Checked {
  const types = new Map<Node, Type>()
  const errs: FormulaError[] = []
  const fail = (n: Node, msg: string) => errs.push(new FormulaError(msg, n.s, n.e))

  const chk = (n: Node): Type => {
    const t = typeOfNode(n)
    types.set(n, t)
    return t
  }
  const typeOfNode = (n: Node): Type => {
    switch (n.k) {
      case 'num': return 'number'
      case 'str': return 'text'
      case 'bool': return 'boolean'
      case 'null': return 'blank'
      case 'field': {
        const t = scope.fieldType(n.name)
        if (t === undefined) {
          fail(n, `there is no field named [${n.name}]`)
          return 'any'
        }
        return t
      }
      case 'link':
        fail(n, 'linked rows cannot be read here')
        return 'any'
      case 'name': {
        const t = scope.nameType(n.parts)
        if (t === undefined) {
          fail(n, `unknown name ${n.parts.join('.')}`)
          return 'any'
        }
        return t
      }
      case 'unary': {
        const x = chk(n.x)
        if (n.op === 'NOT') {
          if (!accepts('boolean', x)) fail(n.x, `NOT needs true or false, got ${typeName(x)}`)
          return 'boolean'
        }
        if (!accepts('number', x)) fail(n.x, `cannot negate ${typeName(x)}`)
        return 'number'
      }
      case 'binary': {
        const l = chk(n.l)
        const r = chk(n.r)
        switch (n.op) {
          case '+': case '-': case '*': case '/': case '^':
            for (const [side, t] of [[n.l, l], [n.r, r]] as [Node, Type][]) {
              if (!accepts('number', t)) {
                let hint = ''
                if (t === 'text' && n.op === '+') hint = ' (use & to join text)'
                else if (t === 'date') hint = ' (use DATEADD or DATEDIFF for dates)'
                fail(side, `${n.op} needs numbers, got ${typeName(t)}${hint}`)
              }
            }
            return 'number'
          case '&':
            for (const [side, t] of [[n.l, l], [n.r, r]] as [Node, Type][]) if (t === 'list') fail(side, '& cannot join a list (use JOIN)')
            return 'text'
          case 'AND': case 'OR':
            if (!accepts('boolean', l)) fail(n.l, `${n.op} needs true or false, got ${typeName(l)}`)
            if (!accepts('boolean', r)) fail(n.r, `${n.op} needs true or false, got ${typeName(r)}`)
            return 'boolean'
          default:
            if (!comparable(l, r)) fail(n, `cannot compare ${typeName(l)} with ${typeName(r)}`)
            return 'boolean'
        }
      }
      case 'in': {
        const x = chk(n.x)
        for (const item of n.list) {
          const t = chk(item)
          if (!comparable(x, t)) fail(item, `cannot compare ${typeName(x)} with ${typeName(t)}`)
        }
        return 'boolean'
      }
      case 'call': {
        const f = functions[n.name]
        if (!f) {
          fail(n, `unknown function ${n.name}`)
          n.args.forEach(chk)
          return 'any'
        }
        if (f.rollup) {
          fail(n, `${n.name} reads linked rows, which only table formulas can`)
          return 'number'
        }
        const args = n.args.map(chk)
        if (args.length < f.min || (!f.variadic && args.length > f.params.length)) {
          fail(n, `${n.name} takes ${arity(f)}`)
          return result(f, args)
        }
        if (f.check) {
          const msg = f.check(args)
          if (msg) {
            fail(n, msg)
            return result(f, args)
          }
        }
        args.forEach((t, i) => {
          const want = param(f, i)
          if (!accepts(want, t)) fail(n.args[i], `argument ${i + 1} of ${n.name} must be ${typeName(want)}, got ${typeName(t)}`)
        })
        return result(f, args)
      }
    }
  }
  const type = chk(root)
  if (errs.length) throw errs[0]
  return { source: src, root, type, types }
}

export function compile(src: string, scope: Scope): Checked {
  return check(src, parse(src), scope)
}

// ---------------------------------------------------------------- evaluator

class Evaluator {
  env: Env
  c: Checked
  constructor(env: Env, c: Checked) {
    this.env = env
    this.c = c
  }

  type(n: Node): Type {
    return this.c.types.get(n) ?? 'any'
  }

  fail(n: Node, e: unknown): FormulaError {
    if (e instanceof FormulaError) return e
    return new FormulaError(e instanceof Error ? e.message : String(e), n.s, n.e)
  }

  eval(n: Node): Value {
    switch (n.k) {
      case 'num': case 'str': case 'bool': return n.v
      case 'null': return null
      case 'field': return norm(this.env.field(n.name))
      case 'link': throw this.fail(n, new Error('linked rows cannot be read here'))
      case 'name': return norm(this.env.name(n.parts))
      case 'unary': {
        const x = this.eval(n.x)
        try {
          return n.op === 'NOT' ? !toBool(x) : -toNumber(x)
        } catch (e) {
          throw this.fail(n.x, e)
        }
      }
      case 'binary': return this.binary(n)
      case 'in': {
        const x = this.eval(n.x)
        const xt = this.type(n.x)
        for (const item of n.list) {
          const v = this.eval(item)
          let c: number
          let known: boolean
          try {
            ;[c, known] = compare(x, xt, v, this.type(item))
          } catch (e) {
            throw this.fail(n, e)
          }
          if (!known) {
            if (x === null && zeroOf(xt) === undefined) return false
            continue
          }
          if (c === 0) return !n.not
        }
        return n.not
      }
      case 'call': {
        const f = functions[n.name]
        try {
          if (f.lazy) return f.lazy(this, n.args)
          if (!f.eval) throw new Error(`${n.name} cannot run here`)
          const args = n.args.map((a) => this.eval(a))
          return f.eval(this.env, args)
        } catch (e) {
          throw this.fail(n, e)
        }
      }
    }
  }

  binary(n: Extract<Node, { k: 'binary' }>): Value {
    const l = this.eval(n.l)
    if (n.op === 'AND' || n.op === 'OR') {
      let lb: boolean
      try {
        lb = toBool(l)
      } catch (e) {
        throw this.fail(n.l, e)
      }
      if ((n.op === 'AND') !== lb) return lb
      const r = this.eval(n.r)
      try {
        return toBool(r)
      } catch (e) {
        throw this.fail(n.r, e)
      }
    }
    const r = this.eval(n.r)
    switch (n.op) {
      case '&': return toText(l) + toText(r)
      case '+': case '-': case '*': case '/': case '^': {
        let a: number
        let b: number
        try {
          a = toNumber(l)
        } catch (e) {
          throw this.fail(n.l, e)
        }
        try {
          b = toNumber(r)
        } catch (e) {
          throw this.fail(n.r, e)
        }
        switch (n.op) {
          case '+': return a + b
          case '-': return a - b
          case '*': return a * b
          case '/': return b === 0 ? null : a / b
          default: return Math.pow(a, b)
        }
      }
    }
    let c: number
    let known: boolean
    try {
      ;[c, known] = compare(l, this.type(n.l), r, this.type(n.r))
    } catch (e) {
      throw this.fail(n, e)
    }
    if (!known) return false
    switch (n.op) {
      case '=': return c === 0
      case '<>': return c !== 0
      case '<': return c < 0
      case '<=': return c <= 0
      case '>': return c > 0
      case '>=': return c >= 0
    }
    throw this.fail(n, new Error(`unknown operator ${n.op}`))
  }
}

const norm = (v: Value | undefined): Value => (v === undefined ? null : v)

// evaluate runs a checked formula; it throws a FormulaError when it fails.
export function evaluate(c: Checked, env: Env): Value {
  const v = new Evaluator(env, c).eval(c.root)
  if (typeof v === 'number' && !Number.isFinite(v)) return null
  return v
}
