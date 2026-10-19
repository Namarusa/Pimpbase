// The bindings API queries can use, and helpers shared by the API studio
// and the screens that show query results.
import type { Pair, Query, QueryField, QueryParam, Table, Variable, Field, FieldType } from '../api'
import type { BindingGroup } from '../designer/context'

export const apiHelp = 'Pipes: | upper, | lower, | default:"x", | json, | xml:"item", | urlencode, | base64, | join:",", | raw. Formulas: {{ = CONCAT("Bearer ", env.TOKEN) }}.'

// queryBindings lists what a query's text can read.
export function queryBindings(q: { name: string; parameters: QueryParam[] }, variables: Variable[], queries: Query[]): BindingGroup[] {
  const groups: BindingGroup[] = []
  if (q.parameters.length) groups.push({ label: 'Parameters', items: q.parameters.filter((p) => p.name).map((p) => ({ path: `params.${p.name}`, hint: p.type })) })
  if (variables.length) groups.push({ label: 'Variables', items: variables.map((v) => ({ path: `env.${v.name}`, hint: v.secret ? 'secret' : '' })) })
  const others = queries.filter((x) => x.name !== q.name)
  if (others.length) {
    groups.push({
      label: 'Other queries’ results',
      items: others.flatMap((x) => [
        { path: `queries.${x.name}.body`, hint: 'body' },
        ...x.fields.slice(0, 12).map((f) => ({ path: `queries.${x.name}.rows.0.${f.name}`, hint: f.type })),
        { path: `queries.${x.name}.status`, hint: 'status' },
      ]),
    })
  }
  groups.push({ label: 'Signed-in user', items: [{ path: 'user.email' }, { path: 'user.name' }, { path: 'user.id' }, { path: 'user.role' }] })
  groups.push({ label: 'App', items: [{ path: 'app.name' }, { path: 'app.slug' }, { path: 'app.id' }] })
  return groups
}

export const countOn = (ps: Pair[]) => ps.filter((p) => !p.off && p.key.trim()).length

// paramsUsed finds {{ params.x }} names in a query's text, to suggest
// declaring them.
export function paramsUsed(q: Query): string[] {
  const text = [q.path, q.body, ...[...q.query, ...q.headers, ...q.form].flatMap((p) => [p.key, p.value])].join('\n')
  const out = new Set<string>()
  for (const m of text.matchAll(/\{\{[^}]*?params\.([A-Za-z_][A-Za-z0-9_]*)/g)) out.add(m[1])
  for (const m of text.matchAll(/\{\{\s*=[^}]*?\[([A-Za-z_][A-Za-z0-9_]*)\]/g)) out.add(m[1])
  return [...out]
}

const fieldType = (t: QueryField['type']): FieldType => (t === 'number' ? 'number' : t === 'boolean' ? 'boolean' : t === 'date' ? 'date' : 'text')

// queryTable describes a query's rows as a table, so the tables, lists,
// charts and forms built for tables show query results too.
export function queryTable(q: { id: string; name: string; fields: QueryField[] }, appId = ''): Table {
  const fields: Field[] = q.fields.map((f) => ({ id: f.name, name: f.name, type: fieldType(f.type) }))
  return { id: `query:${q.id}`, appId, name: q.name, fields, primaryField: fields.find((f) => f.type === 'text')?.id, permissions: { read: 'PUBLIC', write: 'ADMIN' } }
}

// queryRows gives rows an _id, turns numbers sent as text (as XML does)
// into numbers and days into the dates tables use.
export function queryRows(rows: Record<string, any>[], fields: QueryField[]): Record<string, any>[] {
  const nums = fields.filter((f) => f.type === 'number').map((f) => f.name)
  const dates = fields.filter((f) => f.type === 'date').map((f) => f.name)
  return rows.map((r, i) => {
    const out: Record<string, any> = { ...r }
    for (const n of nums) if (typeof out[n] === 'string' && out[n] !== '') out[n] = Number(out[n])
    // A day without a time reads as that day, as table dates do.
    for (const n of dates) if (typeof out[n] === 'string' && /^\d{4}-\d\d-\d\d$/.test(out[n])) out[n] += 'T00:00:00Z'
    if (out._id === undefined) out._id = out.id ?? out.Id ?? out['@id'] ?? i + 1
    return out
  })
}
