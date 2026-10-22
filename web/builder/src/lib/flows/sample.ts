// Sample data for the flow editor: what each step gave in the latest run
// (or a "Get sample data" call), so the binding picker can offer the keys
// that are really there and show a preview of every binding.
import type { App, Flow, FlowRun, FlowStep, Variable } from '../api'
import { preview } from '../preview'
import { before, walk as walkSteps } from './steps'

export interface Samples {
  // run is the run the samples come from.
  run: FlowRun | null
  // fetched holds outputs got with "Get sample data", by step id.
  fetched: Record<string, any>
}

// outputs maps each step id to its first good output in the run, and
// keeps the loops' first items.
export function outputs(run: FlowRun | null): { steps: Record<string, any>; firstItems: Record<string, any> } {
  const steps: Record<string, any> = {}
  const firstItems: Record<string, any> = {}
  for (const l of run?.steps ?? []) {
    if (l.status !== 'ok' || !l.stepId || l.stepId in steps) continue
    steps[l.stepId] = l.output
    if (l.type === 'loop' && l.inputs?.firstItem !== undefined) firstItems[l.stepId] = l.inputs.firstItem
  }
  return { steps, firstItems }
}

const truncatedLog = (v: any) => v && typeof v === 'object' && !Array.isArray(v) && v.truncated === true && 'preview' in v

// scopeFor builds what bindings read when step id runs: the trigger, the
// steps before it, and the item of each loop around it (its first item).
export function scopeFor(f: Flow, id: string | null, s: Samples, app: App | null, variables: Variable[]): Record<string, any> | null {
  const { steps: outs, firstItems } = outputs(s.run)
  const byId: Record<string, any> = { ...outs, ...s.fetched }
  if (!s.run && !Object.keys(byId).length) return null
  const steps: Record<string, any> = {}
  walkSteps(f.steps, (x) => {
    const v = byId[x.id]
    if (v !== undefined && !truncatedLog(v)) steps[x.name] = v
  })
  const scope: Record<string, any> = {
    trigger: s.run?.trigger ?? {},
    steps,
    vars: {},
    env: Object.fromEntries(variables.map((v) => [v.name, v.secret ? '••••••' : v.development])),
    app: app ? { name: app.name, slug: app.slug } : {},
  }
  // Variables as the run left them are not logged; show what set them.
  for (const l of s.run?.steps ?? []) if (l.type === 'set_variable' && l.status === 'ok' && l.output && l.inputs?.name) scope.vars[l.inputs.name] = l.output.value
  walkSteps(f.steps, (x) => {
    if (x.type === 'set_variable' && x.inputs?.name && byId[x.id]?.value !== undefined) scope.vars[x.inputs.name] = byId[x.id].value
  })
  const ctx = id ? before(f.steps, id) : null
  for (const loop of ctx?.loops ?? []) {
    const info = loopItems(loop, scope, firstItems)
    scope.loop = { item: info.first, index: 0, number: 1, count: info.count }
  }
  return scope
}

// loopItems works out a loop's list from the sample, or falls back on the
// first item the run logged.
export function loopItems(loop: FlowStep, scope: Record<string, any>, firstItems: Record<string, any> = {}): { first: any; count: number | undefined; found: boolean } {
  const p = preview(loop.inputs?.items ?? '', scope)
  if (p.kind === 'value' && p.found) {
    const v = p.value
    if (Array.isArray(v)) return { first: v[0], count: v.length, found: v.length > 0 }
    if (typeof v === 'number') return { first: 1, count: v, found: true }
  }
  if (loop.id in firstItems) return { first: firstItems[loop.id], count: undefined, found: true }
  return { first: undefined, count: undefined, found: false }
}
