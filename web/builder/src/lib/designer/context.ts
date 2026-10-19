// What the settings panel offers for a component: the table its field
// pickers list, and the {{ bindings }} it can use.
import { registry } from '../screen/registry'
import { pathTo } from '../screen/context.svelte'
import { all } from './tree'
import type { Query, Table } from '../api'
import type { Node } from '../screen/types'
import { queryTable } from '../apis/bindings'

// queryTables describes API queries as tables ("query:<id>"), so field
// pickers offer their columns.
export const withQueries = (tables: Table[], queries: Query[]): Table[] => [...tables, ...queries.map((q) => queryTable(q))]

const tableOf = (n: Node, tables: Table[]) => tables.find((t) => t.id === n.props.table)

// contextTable is the table a component's field settings refer to: its own
// table, else the nearest provider, form or repeater around it.
export function contextTable(root: Node, id: string, tables: Table[]): Table | undefined {
  const path = pathTo(root, id) ?? []
  const self = path[path.length - 1]
  if (self && registry[self.type]?.settings.some((s) => s.key === 'table')) {
    const own = tableOf(self, tables)
    if (own || self.type === 'form') return own
  }
  for (let i = path.length - 2; i >= 0; i--) {
    const n = path[i]
    if (['form', 'data_provider', 'row_provider', 'repeater'].includes(n.type)) {
      const t = tableOf(n, tables)
      if (t) return t
    }
  }
  return undefined
}

export interface BindingGroup { label: string; items: { path: string; hint?: string }[] }

// bindingsFor lists the bindings a component can read.
export function bindingsFor(root: Node, id: string, tables: Table[], route: string, forActions = false): BindingGroup[] {
  const groups: BindingGroup[] = []
  const path = pathTo(root, id) ?? []
  const self = path[path.length - 1]

  const fields = (t: Table | undefined, prefix: string) => (t?.fields ?? []).map((f) => ({ path: `${prefix}.${f.name}`, hint: f.type }))

  // The row a component sits in: a repeater, a row provider, or a table
  // or list's row when it is clicked.
  for (let i = path.length - 2; i >= 0; i--) {
    const n = path[i]
    if (n.type === 'repeater' || n.type === 'row_provider') {
      const t = tableOf(n, tables) ?? (n.type === 'repeater' ? providerTable(path.slice(0, i), tables) : undefined)
      groups.push({ label: n.type === 'repeater' ? 'Repeated row' : 'Loaded row', items: [{ path: 'row._id', hint: 'id' }, ...fields(t, 'row'), { path: 'index', hint: 'position' }] })
      break
    }
  }
  if (forActions && self && ['table', 'list', 'cards'].includes(self.type)) {
    const t = tableOf(self, tables) ?? providerTable(path.slice(0, -1), tables)
    groups.unshift({ label: 'Clicked row', items: [{ path: 'row._id', hint: 'id' }, ...fields(t, 'row')] })
  }
  const form = [...path].reverse().find((n) => n.type === 'form')
  if (form) {
    const t = tableOf(form, tables)
    groups.push({ label: 'Form values', items: fields(t, 'form') })
    if (forActions && self?.type === 'form') groups.push({ label: 'Saved row', items: [{ path: 'result._id', hint: 'id' }, ...fields(t, 'result')] })
  }
  const provider = [...path].slice(0, -1).reverse().find((n) => n.type === 'data_provider')
  if (provider) groups.push({ label: 'Data provider', items: [{ path: 'data.total', hint: 'rows matching' }, { path: 'data.rows', hint: 'list' }, { path: 'data.rows | count', hint: 'rows loaded' }] })
  if (path.some((n) => n.type === 'dialog' || n.type === 'side_panel')) {
    groups.push({ label: 'Row the dialog was opened from', items: [{ path: 'row._id', hint: 'id' }] })
  }

  const params = route.split('/').filter((s) => s.startsWith(':')).map((s) => ({ path: `url.${s.slice(1)}`, hint: 'route' }))
  if (params.length) groups.push({ label: 'Route', items: params })

  const state = new Set<string>()
  for (const n of all(root)) {
    for (const v of Object.values(n.props)) {
      if (Array.isArray(v)) for (const a of v) if (a?.type === 'set_state' && a.key) state.add(a.key)
    }
    if (n.type.endsWith('_input') && !path.some((p) => p.type === 'form') && n.props.field && !(pathTo(root, n.id) ?? []).some((p) => p.type === 'form')) state.add(n.props.stateKey || n.props.field)
  }
  if (state.size) groups.push({ label: 'State', items: [...state].map((s) => ({ path: `state.${s}` })) })

  groups.push({ label: 'Signed-in user', items: [{ path: 'user.name' }, { path: 'user.email' }, { path: 'user.id' }] })
  groups.push({ label: 'App', items: [{ path: 'app.name' }] })
  return groups
}

function providerTable(path: Node[], tables: Table[]): Table | undefined {
  for (let i = path.length - 1; i >= 0; i--) {
    if (path[i].type === 'data_provider') return tableOf(path[i], tables)
  }
  return undefined
}

export const pipeHelp = 'Format with a pipe: {{ row.Price | currency }}, | number:2, | date, | upper, | default:"none", | count.'
