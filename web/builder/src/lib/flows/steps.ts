// What flows are made of: the trigger and step types, their settings, and
// helpers to edit the step tree and list the bindings a step can read.
import type { Component } from 'svelte'
import SquarePlusIcon from '@lucide/svelte/icons/square-plus'
import SquarePenIcon from '@lucide/svelte/icons/square-pen'
import TrashIcon from '@lucide/svelte/icons/trash'
import SearchIcon from '@lucide/svelte/icons/search'
import PlugIcon from '@lucide/svelte/icons/plug'
import GitBranchIcon from '@lucide/svelte/icons/git-branch'
import RepeatIcon from '@lucide/svelte/icons/repeat'
import HourglassIcon from '@lucide/svelte/icons/hourglass'
import MailIcon from '@lucide/svelte/icons/mail'
import GlobeIcon from '@lucide/svelte/icons/globe'
import VariableIcon from '@lucide/svelte/icons/variable'
import FileTextIcon from '@lucide/svelte/icons/file-text'
import CornerDownLeftIcon from '@lucide/svelte/icons/corner-down-left'
import TableIcon from '@lucide/svelte/icons/table-2'
import ClockIcon from '@lucide/svelte/icons/clock'
import WebhookIcon from '@lucide/svelte/icons/webhook'
import MousePointerClickIcon from '@lucide/svelte/icons/mouse-pointer-click'
import type { Flow, FlowStep, Query, Table, TriggerType, Variable } from '../api'
import type { BindingGroup } from '../designer/context'

export type InputKind =
  | 'text' | 'long' | 'template' | 'formula' | 'table' | 'field' | 'attachment_field' | 'values' | 'pairs'
  | 'select' | 'boolean' | 'query' | 'params'

export interface InputSpec {
  key: string; label: string; kind: InputKind; placeholder?: string; help?: string
  options?: { value: string; label: string }[]; default?: any
  // when hides the input unless it holds for the step's inputs.
  when?: (inputs: Record<string, any>) => boolean
}

export type StepGroup = 'Data' | 'Logic' | 'Actions'

export interface StepInfo {
  type: string; label: string; group: StepGroup; icon: Component; tone: string
  description: string; inputs: InputSpec[]
  // name is the stem of new steps' binding names.
  name: string
  // outputs lists what later steps read as steps.<Name>.…
  outputs: (s: FlowStep, tables: Table[], queries: Query[]) => { path: string; hint?: string }[]
}

const tones = {
  data: 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-300',
  logic: 'bg-amber-500/14 text-amber-700 dark:text-amber-300',
  action: 'bg-sky-500/12 text-sky-700 dark:text-sky-300',
  doc: 'bg-rose-500/12 text-rose-700 dark:text-rose-300',
}

const fieldPaths = (prefix: string, t?: Table) => (t?.fields ?? []).map((f) => ({ path: `${prefix}.${f.name}`, hint: f.type }))
const tableOf = (s: FlowStep, tables: Table[]) => tables.find((t) => t.id === s.inputs?.table)

export const operators = [
  { value: 'equals', label: 'equals' }, { value: 'not_equals', label: 'does not equal' },
  { value: 'gt', label: 'is greater than' }, { value: 'gte', label: 'is at least' },
  { value: 'lt', label: 'is less than' }, { value: 'lte', label: 'is at most' },
  { value: 'contains', label: 'contains' }, { value: 'not_contains', label: 'does not contain' },
  { value: 'empty', label: 'is empty' }, { value: 'not_empty', label: 'is not empty' },
  { value: 'true', label: 'is true' }, { value: 'false', label: 'is false' },
]
export const unary = (op?: string) => ['empty', 'not_empty', 'true', 'false'].includes(op ?? '')

export const stepTypes: StepInfo[] = [
  {
    type: 'create_row', label: 'Create row', group: 'Data', icon: SquarePlusIcon, tone: tones.data, name: 'NewRow',
    description: 'Add a row to a table.',
    inputs: [{ key: 'table', label: 'Table', kind: 'table' }, { key: 'values', label: 'Values', kind: 'values' }],
    outputs: (s, tables) => [{ path: 'id', hint: 'new row id' }, { path: 'row._id', hint: 'id' }, ...fieldPaths('row', tableOf(s, tables))],
  },
  {
    type: 'update_row', label: 'Update row', group: 'Data', icon: SquarePenIcon, tone: tones.data, name: 'Updated',
    description: 'Change fields of a row.',
    inputs: [{ key: 'table', label: 'Table', kind: 'table' }, { key: 'rowId', label: 'Row id', kind: 'text', placeholder: '{{ trigger.row._id }}' }, { key: 'values', label: 'Values', kind: 'values' }],
    outputs: (s, tables) => [{ path: 'row._id', hint: 'id' }, ...fieldPaths('row', tableOf(s, tables))],
  },
  {
    type: 'delete_row', label: 'Delete row', group: 'Data', icon: TrashIcon, tone: tones.data, name: 'Deleted',
    description: 'Delete a row and its links.',
    inputs: [{ key: 'table', label: 'Table', kind: 'table' }, { key: 'rowId', label: 'Row id', kind: 'text', placeholder: '{{ trigger.row._id }}' },
      { key: 'mustExist', label: 'Fail when the row does not exist', kind: 'boolean', default: true }],
    outputs: () => [{ path: 'deleted', hint: 'rows deleted' }, { path: 'id' }],
  },
  {
    type: 'find_rows', label: 'Find rows', group: 'Data', icon: SearchIcon, tone: tones.data, name: 'Found',
    description: 'Look rows up with a filter formula.',
    inputs: [
      { key: 'table', label: 'Table', kind: 'table' },
      { key: 'filter', label: 'Filter formula', kind: 'formula', placeholder: '[Status] = "Open" AND [Customer] = {{ trigger.row.Customer }}', help: 'Bound values are put in as quoted text or numbers.' },
      { key: 'sort', label: 'Sort by', kind: 'field' },
      { key: 'desc', label: 'Newest / largest first', kind: 'boolean' },
      { key: 'limit', label: 'At most', kind: 'text', placeholder: '100' },
    ],
    outputs: (s, tables) => [{ path: 'rows', hint: 'list' }, { path: 'count', hint: 'found' }, { path: 'total', hint: 'matching' }, { path: 'first._id', hint: 'id' }, ...fieldPaths('first', tableOf(s, tables))],
  },
  {
    type: 'run_query', label: 'Run API query', group: 'Data', icon: PlugIcon, tone: tones.data, name: 'Api',
    description: 'Call a query from the APIs tab.',
    inputs: [{ key: 'query', label: 'Query', kind: 'query' }, { key: 'params', label: 'Parameters', kind: 'params' }, { key: 'failOnError', label: 'Fail when the API answers with an error', kind: 'boolean', default: true }],
    outputs: (s, _t, queries) => {
      const q = queries.find((x) => x.id === s.inputs?.query || x.name === s.inputs?.query)
      return [{ path: 'body', hint: 'answer' }, { path: 'rows', hint: 'list' }, ...(q?.fields ?? []).slice(0, 12).map((f) => ({ path: `rows.0.${f.name}`, hint: f.type })), { path: 'status' }, { path: 'ok' }]
    },
  },
  {
    type: 'condition', label: 'Condition', group: 'Logic', icon: GitBranchIcon, tone: tones.logic, name: 'Check',
    description: 'Run one branch or the other.',
    inputs: [
      { key: 'left', label: 'Value', kind: 'text', placeholder: '{{ trigger.row.Amount }}' },
      { key: 'operator', label: 'Test', kind: 'select', options: operators, default: 'equals' },
      { key: 'right', label: 'Compared with', kind: 'text', placeholder: '100', when: (i) => !unary(i.operator) },
    ],
    outputs: () => [{ path: 'result', hint: 'true or false' }],
  },
  {
    type: 'loop', label: 'Loop', group: 'Logic', icon: RepeatIcon, tone: tones.logic, name: 'Each',
    description: 'Run steps once per item of a list.',
    inputs: [
      { key: 'items', label: 'Items', kind: 'text', placeholder: '{{ steps.Found.rows }}', help: 'A list, or a number to repeat that many times. Inside, read {{ loop.item }}, {{ loop.number }}.' },
      { key: 'max', label: 'At most', kind: 'text', placeholder: '5000' },
    ],
    outputs: () => [{ path: 'count', hint: 'items done' }],
  },
  {
    type: 'delay', label: 'Delay', group: 'Logic', icon: HourglassIcon, tone: tones.logic, name: 'Wait',
    description: 'Wait before going on (up to an hour).',
    inputs: [
      { key: 'duration', label: 'Wait for', kind: 'text', placeholder: '30' },
      { key: 'unit', label: 'Unit', kind: 'select', default: 'seconds', options: [{ value: 'seconds', label: 'Seconds' }, { value: 'minutes', label: 'Minutes' }, { value: 'hours', label: 'Hours' }] },
    ],
    outputs: () => [],
  },
  {
    type: 'set_variable', label: 'Set variable', group: 'Logic', icon: VariableIcon, tone: tones.logic, name: 'Set',
    description: 'Keep a value for later steps as {{ vars.name }}.',
    inputs: [
      { key: 'name', label: 'Variable name', kind: 'text', placeholder: 'total' },
      { key: 'mode', label: 'How', kind: 'select', default: 'set', options: [{ value: 'set', label: 'Set to the value' }, { value: 'add', label: 'Add the value (numbers)' }, { value: 'append', label: 'Append the value to a list' }] },
      { key: 'value', label: 'Value', kind: 'text', placeholder: '{{ trigger.row.Amount }}' },
    ],
    outputs: () => [{ path: 'value' }],
  },
  {
    type: 'return', label: 'Return result', group: 'Logic', icon: CornerDownLeftIcon, tone: tones.logic, name: 'Result',
    description: 'End the flow, giving a value back to the button or webhook.',
    inputs: [{ key: 'value', label: 'Value', kind: 'text', placeholder: '{{ steps.NewRow.row }}' }],
    outputs: () => [],
  },
  {
    type: 'send_email', label: 'Send email', group: 'Actions', icon: MailIcon, tone: tones.action, name: 'Email',
    description: 'Send a mail, with files attached.',
    inputs: [
      { key: 'to', label: 'To', kind: 'text', placeholder: '{{ trigger.row.Email }}' },
      { key: 'cc', label: 'Cc', kind: 'text' },
      { key: 'replyTo', label: 'Reply to', kind: 'text' },
      { key: 'subject', label: 'Subject', kind: 'text', placeholder: 'Your order {{ trigger.row._id }}' },
      { key: 'format', label: 'Format', kind: 'select', default: 'text', options: [{ value: 'text', label: 'Plain text' }, { value: 'html', label: 'HTML' }] },
      { key: 'body', label: 'Message', kind: 'template' },
      { key: 'attachments', label: 'Attachments', kind: 'text', placeholder: '{{ steps.Invoice.file }}' },
    ],
    outputs: () => [{ path: 'delivery', hint: 'how it went' }],
  },
  {
    type: 'http_request', label: 'HTTP request', group: 'Actions', icon: GlobeIcon, tone: tones.action, name: 'Request',
    description: 'Call a webhook or any web address.',
    inputs: [
      { key: 'method', label: 'Method', kind: 'select', default: 'POST', options: ['GET', 'POST', 'PUT', 'PATCH', 'DELETE'].map((m) => ({ value: m, label: m })) },
      { key: 'url', label: 'Address', kind: 'text', placeholder: 'https://hooks.example.com/orders' },
      { key: 'headers', label: 'Headers', kind: 'pairs' },
      { key: 'bodyType', label: 'Body', kind: 'select', default: 'json', options: [{ value: 'json', label: 'JSON' }, { value: 'text', label: 'Text' }, { value: 'none', label: 'None' }] },
      { key: 'body', label: 'Body', kind: 'template', when: (i) => i.bodyType !== 'none' },
      { key: 'failOnError', label: 'Fail when the answer is an error', kind: 'boolean', default: true },
    ],
    outputs: () => [{ path: 'status' }, { path: 'body', hint: 'answer' }, { path: 'ok' }],
  },
  {
    type: 'generate_pdf', label: 'Generate PDF', group: 'Actions', icon: FileTextIcon, tone: tones.doc, name: 'Pdf',
    description: 'Make a PDF from a template and keep it.',
    inputs: [
      { key: 'title', label: 'Title', kind: 'text', placeholder: 'Invoice {{ trigger.row._id }}' },
      { key: 'content', label: 'Content', kind: 'template', help: 'Markdown: # headings, **bold**, - lists, | tables |, ---. Repeat rows with {{#each steps.Found.rows}} … {{ item.Name }} … {{/each}}.' },
      { key: 'footer', label: 'Footer', kind: 'text', placeholder: '{{ app.name }}' },
      { key: 'fileName', label: 'File name', kind: 'text', placeholder: 'invoice-{{ trigger.row._id }}' },
      { key: 'pageSize', label: 'Page', kind: 'select', default: 'A4', options: [{ value: 'A4', label: 'A4' }, { value: 'Letter', label: 'Letter' }] },
      { key: 'orientation', label: 'Orientation', kind: 'select', default: 'portrait', options: [{ value: 'portrait', label: 'Portrait' }, { value: 'landscape', label: 'Landscape' }] },
      { key: 'table', label: 'Store with table', kind: 'table', help: 'Reading the PDF needs read access to this table.' },
      { key: 'rowId', label: 'Attach to row (id)', kind: 'text', placeholder: '{{ trigger.row._id }}' },
      { key: 'field', label: 'Attachment field', kind: 'attachment_field' },
      { key: 'attachMode', label: 'Existing files', kind: 'select', default: 'append', options: [{ value: 'append', label: 'Keep them, add the PDF' }, { value: 'replace', label: 'Replace them' }], when: (i) => !!i.field },
    ],
    outputs: () => [{ path: 'file', hint: 'the PDF' }, { path: 'file.url', hint: 'address' }, { path: 'file.name' }, { path: 'attached' }],
  },
]

export const stepInfo = (type: string) => stepTypes.find((s) => s.type === type)

export interface TriggerInfo { type: TriggerType; label: string; icon: Component; description: string }
export const triggerTypes: TriggerInfo[] = [
  { type: 'row_created', label: 'Row created', icon: TableIcon, description: 'When a row is added to a table' },
  { type: 'row_updated', label: 'Row updated', icon: TableIcon, description: 'When a row of a table changes' },
  { type: 'row_deleted', label: 'Row deleted', icon: TableIcon, description: 'When a row of a table is deleted' },
  { type: 'schedule', label: 'Schedule', icon: ClockIcon, description: 'At set times, such as every morning' },
  { type: 'webhook', label: 'Webhook', icon: WebhookIcon, description: 'When another service calls the flow’s address' },
  { type: 'manual', label: 'Button', icon: MousePointerClickIcon, description: 'When a screen button runs it' },
]
export const triggerInfo = (type: string) => triggerTypes.find((t) => t.type === type) ?? triggerTypes[5]

// Tree helpers. Branches are a step's then, else or do lists.
export type Branch = 'then' | 'else' | 'do'
export interface Place { list: FlowStep[]; index: number }

export function walk(steps: FlowStep[], fn: (s: FlowStep) => void) {
  for (const s of steps) {
    fn(s)
    for (const b of ['then', 'else', 'do'] as Branch[]) if (s[b]) walk(s[b]!, fn)
  }
}

export function findStep(steps: FlowStep[], id: string): FlowStep | undefined {
  let out: FlowStep | undefined
  walk(steps, (s) => { if (s.id === id) out = s })
  return out
}

// locate finds the list holding a step.
export function locate(steps: FlowStep[], id: string): Place | null {
  const i = steps.findIndex((s) => s.id === id)
  if (i >= 0) return { list: steps, index: i }
  for (const s of steps) {
    for (const b of ['then', 'else', 'do'] as Branch[]) {
      const p = s[b] ? locate(s[b]!, id) : null
      if (p) return p
    }
  }
  return null
}

const newId = () => Math.random().toString(36).slice(2, 10)

export function uniqueName(steps: FlowStep[], stem: string): string {
  const taken = new Set<string>()
  walk(steps, (s) => taken.add(s.name.toLowerCase()))
  if (!taken.has(stem.toLowerCase())) return stem
  for (let i = 2; ; i++) if (!taken.has(`${stem}${i}`.toLowerCase())) return `${stem}${i}`
}

export function newStep(type: string, all: FlowStep[], tables: Table[]): FlowStep {
  const info = stepInfo(type)!
  const inputs: Record<string, any> = {}
  for (const i of info.inputs) if (i.default !== undefined) inputs[i.key] = i.default
  if (info.inputs.some((i) => i.key === 'table') && tables[0]) inputs.table = tables[0].id
  if (info.inputs.some((i) => i.kind === 'values')) inputs.values = []
  const s: FlowStep = { id: newId(), name: uniqueName(all, info.name), type, inputs }
  if (type === 'condition') { s.then = []; s.else = [] }
  if (type === 'loop') s.do = []
  return s
}

// copyStep duplicates a step with new ids and names.
export function copyStep(s: FlowStep, all: FlowStep[]): FlowStep {
  const taken = JSON.parse(JSON.stringify(all)) as FlowStep[]
  const c = JSON.parse(JSON.stringify(s)) as FlowStep
  walk([c], (x) => {
    x.id = newId()
    x.name = uniqueName(taken, x.name.replace(/\d+$/, '') || 'Step')
    taken.push({ ...x, then: undefined, else: undefined, do: undefined })
  })
  return c
}

// Bindings. before lists the steps that have run when step id runs: the
// earlier steps of each list on the way to it, and the conditions and
// loops around it.
export function before(steps: FlowStep[], id: string): { done: FlowStep[]; loops: FlowStep[] } | null {
  for (let i = 0; i < steps.length; i++) {
    const s = steps[i]
    if (s.id === id) return { done: steps.slice(0, i), loops: [] }
    for (const b of ['then', 'else', 'do'] as Branch[]) {
      const inner = s[b] ? before(s[b]!, id) : null
      if (inner) {
        const earlier = steps.slice(0, i)
        return { done: [...earlier, ...(b === 'do' ? [] : [s]), ...inner.done], loops: b === 'do' ? [s, ...inner.loops] : inner.loops }
      }
    }
  }
  return null
}

export function triggerBindings(f: Flow, tables: Table[]): BindingGroup {
  const t = tables.find((x) => x.id === f.trigger.table)
  switch (f.trigger.type) {
    case 'row_created':
    case 'row_deleted':
      return { label: 'Trigger row', items: [{ path: 'trigger.row._id', hint: 'id' }, ...fieldPaths('trigger.row', t), { path: 'trigger.table.name' }] }
    case 'row_updated':
      return { label: 'Trigger row', items: [{ path: 'trigger.row._id', hint: 'id' }, ...fieldPaths('trigger.row', t), ...fieldPaths('trigger.old', t).map((x) => ({ ...x, hint: 'before' })), { path: 'trigger.changed', hint: 'field names' }] }
    case 'schedule':
      return { label: 'Schedule', items: [{ path: 'trigger.time', hint: 'when it fired' }, { path: 'trigger.date', hint: 'day' }] }
    case 'webhook':
      return { label: 'Webhook call', items: [{ path: 'trigger.body', hint: 'JSON sent' }, { path: 'trigger.query', hint: '?a=b' }, { path: 'trigger.headers' }, { path: 'trigger.method' }] }
  }
  return { label: 'Button', items: (f.trigger.parameters ?? []).filter((p) => p.name).map((p) => ({ path: `trigger.params.${p.name}`, hint: p.type })) }
}

export function stepBindings(f: Flow, id: string | null, tables: Table[], queries: Query[], variables: Variable[]): BindingGroup[] {
  const groups: BindingGroup[] = [triggerBindings(f, tables)]
  const ctx = id ? before(f.steps, id) : { done: [], loops: [] }
  if (ctx?.loops.length) {
    const loop = ctx.loops[ctx.loops.length - 1]
    const src = /steps\.(\w+)\.rows/.exec(loop.inputs?.items ?? '')?.[1]
    let named: FlowStep | undefined
    if (src) walk(f.steps, (s) => { if (s.name === src) named = s })
    const t = named?.type === 'find_rows' ? tableOf(named, tables) : undefined
    groups.push({ label: `Loop ${loop.name}`, items: [{ path: 'loop.item', hint: 'this item' }, ...fieldPaths('loop.item', t), { path: 'loop.number', hint: 'from 1' }, { path: 'loop.index', hint: 'from 0' }, { path: 'loop.count' }] })
  }
  const vars = new Set<string>()
  for (const s of ctx?.done ?? []) {
    const info = stepInfo(s.type)
    if (!info) continue
    const outs = info.outputs(s, tables, queries)
    if (s.continueOnError) outs.push({ path: 'error', hint: 'when it failed' })
    if (outs.length) groups.push({ label: `${s.name} · ${info.label}`, items: outs.map((o) => ({ path: `steps.${s.name}.${o.path}`, hint: o.hint })) })
  }
  walk(f.steps, (s) => { if (s.type === 'set_variable' && s.inputs?.name && s.id !== id) vars.add(s.inputs.name) })
  if (vars.size) groups.push({ label: 'Variables', items: [...vars].map((v) => ({ path: `vars.${v}` })) })
  if (variables.length) groups.push({ label: 'App variables', items: variables.map((v) => ({ path: `env.${v.name}`, hint: v.secret ? 'secret' : '' })) })
  groups.push({ label: 'Who started it', items: [{ path: 'user.email' }, { path: 'user.name' }, { path: 'user.id' }] })
  groups.push({ label: 'App', items: [{ path: 'app.name' }, { path: 'app.slug' }] })
  return groups
}

export const flowHelp = 'Pipes: | upper, | number:2, | default:"x", | json, | join:", ". Formulas: {{ = trigger.row.Qty * 2 }}. Repeat: {{#each list}}{{ item.Name }}{{/each}}; branch: {{#if x}}…{{else}}…{{/if}}.'

// summary is a step's one-line description on the canvas.
export function summary(s: FlowStep, tables: Table[], queries: Query[]): string {
  const i = s.inputs ?? {}
  const t = tables.find((x) => x.id === i.table)?.name
  switch (s.type) {
    case 'create_row': return `Add a row to ${t ?? '…'}`
    case 'update_row': return `Change row ${i.rowId || '…'} of ${t ?? '…'}`
    case 'delete_row': return `Delete row ${i.rowId || '…'} of ${t ?? '…'}`
    case 'find_rows': return `${t ?? '…'}${i.filter ? ` where ${i.filter}` : ''}`
    case 'run_query': return queries.find((q) => q.id === i.query)?.name ?? 'Choose a query'
    case 'condition': return `${i.left || '…'} ${operators.find((o) => o.value === i.operator)?.label ?? ''}${unary(i.operator) ? '' : ` ${i.right ?? ''}`}`
    case 'loop': return `For each of ${i.items || '…'}`
    case 'delay': return `Wait ${i.duration || '…'} ${i.unit ?? 'seconds'}`
    case 'set_variable': return `vars.${i.name || '…'} ${i.mode === 'add' ? '+=' : i.mode === 'append' ? '+= [' : '='} ${i.value ?? ''}${i.mode === 'append' ? ' ]' : ''}`
    case 'return': return `Give back ${i.value || 'nothing'}`
    case 'send_email': return `To ${i.to || '…'}${i.subject ? ` · ${i.subject}` : ''}`
    case 'http_request': return `${i.method ?? 'POST'} ${i.url || '…'}`
    case 'generate_pdf': return `${i.title || i.fileName || 'Untitled'}${i.field ? ` → ${i.field}` : ''}`
  }
  return s.type
}

// describeCron reads common schedules in words.
export function describeCron(cron = ''): string {
  const p = cron.trim().split(/\s+/)
  if (p.length !== 5) return cron
  const [m, h, dom, mon, dow] = p
  const time = /^\d+$/.test(m) && /^\d+$/.test(h) ? `${h.padStart(2, '0')}:${m.padStart(2, '0')}` : ''
  const days: Record<string, string> = { '*': 'every day', '1-5': 'on weekdays', 'mon-fri': 'on weekdays', '0,6': 'at weekends', '6,0': 'at weekends' }
  if (/^\*\/\d+$/.test(m) && h === '*' && dom === '*' && mon === '*' && dow === '*') return `Every ${m.slice(2)} minutes`
  if (/^\d+$/.test(m) && h === '*' && dom === '*' && mon === '*' && dow === '*') return `Every hour at :${m.padStart(2, '0')}`
  if (time && dom === '*' && mon === '*' && days[dow]) return `${days[dow][0].toUpperCase()}${days[dow].slice(1)} at ${time}`
  const names = ['Sunday', 'Monday', 'Tuesday', 'Wednesday', 'Thursday', 'Friday', 'Saturday']
  if (time && dom === '*' && mon === '*' && /^[0-6](,[0-6])*$/.test(dow)) return `Every ${dow.split(',').map((d) => names[+d]).join(', ')} at ${time}`
  if (time && /^\d+$/.test(dom) && mon === '*' && dow === '*') return `Monthly on day ${dom} at ${time}`
  return cron
}

// normalize gives conditions and loops their lists, which the server
// leaves out when empty.
export function normalize(f: Flow): Flow {
  walk(f.steps ?? (f.steps = []), (s) => {
    s.inputs ??= {}
    if (s.type === 'condition') { s.then ??= []; s.else ??= [] }
    if (s.type === 'loop') s.do ??= []
  })
  f.trigger.parameters ??= []
  f.trigger.fields ??= []
  return f
}
