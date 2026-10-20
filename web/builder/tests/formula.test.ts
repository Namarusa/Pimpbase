// Checks the browser formula engine against the results Go gives, from
// internal/formula/testdata/vectors.json. Run with: npm test
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { compile, evaluate, parseDate, type Type, type Value } from '../src/lib/formula/formula.ts'

const file = new URL('../../../internal/formula/testdata/vectors.json', import.meta.url)
const vectors = JSON.parse(readFileSync(file, 'utf8'))

const decode = (v: any): Value => {
  if (Array.isArray(v)) return v.map(decode)
  if (v && typeof v === 'object' && 'date' in v) return parseDate(v.date)
  return v
}

const fields: Record<string, Value> = {}
for (const [k, v] of Object.entries(vectors.fields)) fields[k] = decode(v)
const types = vectors.types as Record<string, Type>

const scope = {
  fieldType: (name: string) => types[name.toLowerCase()],
  nameType: (parts: string[]) => (parts.join('.').toLowerCase() === 'current.user.email' ? ('text' as Type) : undefined),
}
const env = {
  field: (name: string) => fields[name.toLowerCase()] ?? null,
  name: () => vectors.user.email,
  now: () => parseDate(vectors.now),
}

function same(a: Value, b: Value): boolean {
  if (typeof a === 'number' && typeof b === 'number') return a === b || Math.abs(a - b) < 1e-12 * Math.max(1, Math.abs(b))
  if (a instanceof Date && b instanceof Date) return a.getTime() === b.getTime()
  if (Array.isArray(a) && Array.isArray(b)) return a.length === b.length && a.every((x, i) => same(x, b[i]))
  return a === b
}

const show = (v: Value) => JSON.stringify(v instanceof Date ? { date: v.toISOString() } : v)

for (const c of vectors.cases as { src: string; want: any }[]) {
  test(c.src, () => {
    const checked = compile(c.src, scope)
    if (c.want && typeof c.want === 'object' && c.want.error) {
      assert.throws(() => evaluate(checked, env), undefined, `${c.src} should fail`)
      return
    }
    const got = evaluate(checked, env)
    const want = decode(c.want)
    assert.ok(same(got, want), `${c.src}: got ${show(got)}, want ${show(want)}`)
  })
}
