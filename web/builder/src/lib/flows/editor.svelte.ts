// The flow editor's shared state: the flow being edited, the selection
// and the edits the canvas and settings panel make.
import { getContext, setContext } from 'svelte'
import type { Flow, FlowStep, Query, RoleDef, Table, Variable } from '../api'
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
  onChange: () => void = () => {}

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
