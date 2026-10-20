// RowSource loads the rows a data component shows: from its own table
// setting, or from the data provider around it when it has none.
import { untrack } from 'svelte'
import { api, type AggGroup, type AggQuery, type Row, type Table } from '../api'
import { queryRows } from '../apis/bindings'
import { resolve, resolveFilter, toText } from './bindings'
import type { Runtime } from './context.svelte'
import type { Scope } from './types'

export interface SourceProps {
  table?: string
  filter?: string
  // params are an API query's parameters: name → value with bindings.
  params?: Record<string, string>
  sort?: string
  desc?: boolean
  limit?: number
}

export class RowSource {
  rows = $state<Row[]>([])
  total = $state(0)
  loading = $state(true)
  error = $state('')
  table = $state<Table | undefined>()
  // fromParent is true when the rows come from a surrounding provider.
  fromParent = $state(false)
  // groups holds the server's answer when the source was asked to
  // aggregate its own table; null otherwise.
  groups = $state<AggGroup[] | null>(null)
  private seq = 0
  // The last API answer, so paging and sorting do not send the query again.
  private cache: { key: string; rows: Row[] } | null = null

  // get returns everything the rows depend on; the source reloads when
  // any of it changes.
  //
  // With agg, a component's own table is grouped and reduced on the
  // server and the answer is in groups; rows from a surrounding provider
  // or an API query stay in rows, to be reduced in the browser.
  constructor(rt: Runtime, id: string, get: () => { props: SourceProps; scope: Scope; offset?: number; sort?: string; desc?: boolean; search?: string; limit?: number; agg?: AggQuery }) {
    $effect(() => {
      const { props, scope, offset, sort, desc, search, limit, agg } = get()
      if (!agg) this.groups = null
      const own = rt.table(props.table)
      // Reading the counters makes the effect rerun on refresh.
      void rt.refresh.all
      void rt.refresh[id]
      if (!own) {
        const parent = scope.data
        this.fromParent = !!parent
        this.table = parent?.table
        this.rows = parent?.rows ?? []
        this.total = parent?.total ?? 0
        this.loading = parent?.loading ?? false
        this.error = props.table ? 'This component’s table no longer exists.' : ''
        return
      }
      this.fromParent = false
      this.table = own
      if (own.id.startsWith('query:')) {
        const q = rt.query(own.id.slice(6))!
        const params: Record<string, any> = {}
        for (const [k, v] of Object.entries(props.params ?? {})) if (v !== '' && v !== undefined) params[k] = resolve(v, scope)
        // A refresh sends the query again.
        const key = JSON.stringify([q.id, params, rt.env, rt.refresh.all, rt.refresh[id] ?? 0])
        // Search, sort and paging happen here, over everything loaded.
        const view = (all: Row[]) => {
          let rows = all
          const s = search?.trim().toLowerCase()
          if (s) rows = rows.filter((r) => own.fields.some((f) => toText(r[f.name]).toLowerCase().includes(s)))
          const by = sort ?? props.sort
          if (by) {
            const d = (desc ?? props.desc) ? -1 : 1
            rows = [...rows].sort((a, b) => {
              const x = a[by], y = b[by]
              return d * (typeof x === 'number' && typeof y === 'number' ? x - y : toText(x).localeCompare(toText(y)))
            })
          }
          const from = offset ?? 0
          this.total = rows.length
          this.rows = rows.slice(from, from + (limit ?? props.limit ?? 50))
        }
        if (this.cache?.key === key) {
          view(this.cache.rows)
          this.loading = false
          return
        }
        const n = ++this.seq
        this.loading = true
        const h = setTimeout(async () => {
          try {
            const res = await api.runQuery(rt.appId, q.id, params, rt.env)
            if (n !== this.seq) return
            if (!res.ok) throw new Error(`${q.name} answered ${res.status} ${res.statusText}`.trim())
            const rows = queryRows(res.rows, res.fields.length ? res.fields : q.fields) as Row[]
            this.cache = { key, rows }
            view(rows)
            this.error = ''
          } catch (e: any) {
            if (n !== this.seq) return
            this.error = e.message
            this.rows = []
            this.total = 0
          } finally {
            if (n === this.seq) this.loading = false
          }
        }, 120)
        return () => clearTimeout(h)
      }
      let filter = resolveFilter(props.filter, scope)
      if (search) {
        const sf = searchFilter(own, search, untrack(() => this.total))
        if (sf) filter = filter ? `(${filter}) AND ${sf}` : sf
      }
      if (agg) {
        const n = ++this.seq
        this.loading = true
        const h = setTimeout(async () => {
          try {
            const res = await api.aggregate(rt.appId, own.id, { ...agg, filter })
            if (n !== this.seq) return
            this.groups = res.groups
            this.total = res.rows
            this.error = ''
          } catch (e: any) {
            if (n !== this.seq) return
            this.error = e.message
            this.groups = []
          } finally {
            if (n === this.seq) this.loading = false
          }
        }, 120)
        return () => clearTimeout(h)
      }
      const q = { filter, sort: sort ?? props.sort, desc: desc ?? props.desc, limit: limit ?? props.limit ?? 50, offset: offset ?? 0 }
      const n = ++this.seq
      this.loading = true
      const h = setTimeout(async () => {
        try {
          const page = await api.rows(rt.appId, own.id, q)
          if (n !== this.seq) return
          this.rows = page.rows
          this.total = page.total
          this.error = ''
        } catch (e: any) {
          if (n !== this.seq) return
          this.error = e.message
          this.rows = []
          this.total = 0
        } finally {
          if (n === this.seq) this.loading = false
        }
      }, 120)
      return () => clearTimeout(h)
    })
  }
}

// scanLimit is the most rows the server filters without SQL; searches
// over bigger tables leave out fields whose test only runs in Go.
const scanLimit = 100_000

// searchFilter matches rows holding the search text in a text field, or
// equal to it in a number field. Text is compared as stored, so the
// database can run it; lists and computed text are searched only on
// tables small enough to scan.
export function searchFilter(t: Table, search: string, total: number): string {
  const q = search.trim().toLowerCase().replaceAll('"', '""')
  if (!q) return ''
  const small = total <= scanLimit
  const parts: string[] = []
  for (const f of t.fields) {
    const name = `[${f.name}]`
    if (['text', 'longtext', 'single_select'].includes(f.type) || (f.type === 'formula' && f.resultType === 'text' && f.formulaMode === 'static')) {
      parts.push(`CONTAINS(LOWER(${name}), "${q}")`)
    } else if (small && (f.type === 'multi_select' || (f.type === 'formula' && f.resultType === 'text'))) {
      parts.push(`CONTAINS(LOWER(TEXT(${name})), "${q}")`)
    } else if ((f.type === 'number' || f.type === 'autonumber') && /^-?\d+(\.\d+)?$/.test(q)) {
      parts.push(`${name} = ${q}`)
    }
  }
  return parts.length > 1 ? `OR(${parts.join(', ')})` : (parts[0] ?? '')
}

// aggregate reduces rows to one number.
export function aggregate(rows: Row[], op: string, field?: string): number {
  if (op === 'count' || !field) return rows.length
  const nums = rows.map((r) => r[field]).filter((v) => v !== null && v !== undefined && v !== '').map(Number).filter((v) => !isNaN(v))
  if (!nums.length) return 0
  switch (op) {
    case 'sum': return nums.reduce((a, b) => a + b, 0)
    case 'avg': return nums.reduce((a, b) => a + b, 0) / nums.length
    case 'min': return Math.min(...nums)
    case 'max': return Math.max(...nums)
  }
  return rows.length
}

export const aggregateOptions = [
  { value: 'count', label: 'Count rows' },
  { value: 'sum', label: 'Sum' },
  { value: 'avg', label: 'Average' },
  { value: 'min', label: 'Minimum' },
  { value: 'max', label: 'Maximum' },
]
