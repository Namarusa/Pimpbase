// The flow editor's shared state: the flow being edited, the selection
// and the edits the canvas and settings panel make.
import { getContext, setContext } from 'svelte'
import { api, type App, type Flow, type FlowRun, type FlowStep, type Query, type RoleDef, type Table, type Variable } from '../api'
import { preview } from '../preview'
import { scopeFor, type Samples } from './sample'
import { copyStep, locate, newStep, walk } from './steps'

export class FlowEditor {
  flow = $state<Flow | null>(null)
  tables = $state<Table[]>([])
  queries = $state<Query[]>([])
  variables = $state<Variable[]>([])
  roles = $state<RoleDef[]>([])
  // selected is "trigger" or a step id.
  selected = $state<string>('trigger')
  // errorStep is the step the last save refused.
  errorStep = $state<string>('')
  // lastRun maps step ids to how they did in the run being shown.
  lastRun = $state<Record<string, 'ok' | 'failed' | 'skipped'>>({})
  // samples is the data previews use: the latest run, and outputs got
  // with "Get sample data".
  app = $state<App | null>(null)
  samples = $state<Samples>({ run: null, fetched: {} })
  onChange: () => void = () => {}

  // scope is what bindings read when the step runs, from the samples.
  scope(id: string | null) {
    return this.flow ? scopeFor(this.flow, id, this.samples, this.app, this.variables) : null
  }

  // loadSamples takes the flow's latest run as sample data.
  async loadSamples() {
    const f = this.flow
    if (!f) return
    try {
      const runs = await api.flowRuns(f.appId, f.id)
      const latest = runs.find((r) => r.status !== 'running')
      const run = latest ? await api.flowRun(f.appId, latest.id) : null
      if (this.flow?.id === f.id) this.samples = { run, fetched: {} }
    } catch { /* previews stay empty */ }
  }

  useRun(run: FlowRun) { this.samples = { run, fetched: {} } }

  // fetchSample runs a step's query, or reads a few rows of its table, so
  // later steps can be written against real data. Nothing is changed.
  async fetchSample(s: FlowStep) {
    const f = this.flow
    if (!f) return
    const scope = this.scope(s.id)
    if (s.type === 'run_query') {
      const params: Record<string, any> = {}
      for (const [k, src] of Object.entries((s.inputs.params ?? {}) as Record<string, string>)) {
        if (!src) continue
        const p = preview(src, scope)
        if (p.kind === 'value' && p.found) params[k] = p.value
        else if (p.kind === 'text' && !p.missing.length) params[k] = p.text
        else if (p.kind === 'none') params[k] = src
      }
      const r = await api.runQuery(f.appId, s.inputs.query, params, 'development')
      this.samples.fetched[s.id] = { ok: r.ok, status: r.status, body: r.body, rows: r.rows, headers: r.headers }
    } else if (s.type === 'find_rows' && s.inputs.table) {
      const page = await api.rows(f.appId, s.inputs.table, { sort: s.inputs.sort || undefined, desc: !!s.inputs.desc, limit: 5 })
      this.samples.fetched[s.id] = { rows: page.rows, count: page.rows.length, total: page.total, first: page.rows[0] ?? null }
    }
  }

  table(id?: string) { return this.tables.find((t) => t.id === id) }

  changed() { this.onChange() }

  insert(list: FlowStep[], index: number, type: string) {
    if (!this.flow) return
    const s = newStep(type, this.flow.steps, this.tables)
    list.splice(index, 0, s)
    this.selected = s.id
    this.changed()
  }

  remove(id: string) {
    if (!this.flow) return
    const p = locate(this.flow.steps, id)
    if (!p) return
    p.list.splice(p.index, 1)
    if (this.selected === id) this.selected = p.list[Math.min(p.index, p.list.length - 1)]?.id ?? 'trigger'
    this.changed()
  }

  move(id: string, by: number) {
    if (!this.flow) return
    const p = locate(this.flow.steps, id)
    if (!p) return
    const to = p.index + by
    if (to < 0 || to >= p.list.length) return
    const [s] = p.list.splice(p.index, 1)
    p.list.splice(to, 0, s)
    this.changed()
  }

  duplicate(id: string) {
    if (!this.flow) return
    const p = locate(this.flow.steps, id)
    if (!p) return
    const c = copyStep(p.list[p.index], this.flow.steps)
    p.list.splice(p.index + 1, 0, c)
    this.selected = c.id
    this.changed()
  }

  step(id: string): FlowStep | undefined {
    let out: FlowStep | undefined
    if (this.flow) walk(this.flow.steps, (s) => { if (s.id === id) out = s })
    return out
  }
}

const key = Symbol('flow-editor')
export const setEditor = (e: FlowEditor) => setContext(key, e)
export const getEditor = () => getContext<FlowEditor>(key)
