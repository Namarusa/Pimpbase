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
import SparklesIcon from '@lucide/svelte/icons/sparkles'
import TableIcon from '@lucide/svelte/icons/table-2'
import ClockIcon from '@lucide/svelte/icons/clock'
import WebhookIcon from '@lucide/svelte/icons/webhook'
import MousePointerClickIcon from '@lucide/svelte/icons/mouse-pointer-click'
import type { Flow, FlowStep, Query, Table, TriggerType, Variable } from '../api'
import type { BindingGroup } from '../designer/context'

export type InputKind =
  | 'text' | 'long' | 'template' | 'formula' | 'table' | 'field' | 'attachment_field' | 'values' | 'pairs'
  | 'select' | 'boolean' | 'query' | 'params' | 'ai_fields' | 'email_template' | 'email_data'

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
  ai: 'bg-violet-500/12 text-violet-700 dark:text-violet-300',
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
      return [{ path: 'rows', hint: 'list' }, ...(q?.fields ?? []).slice(0, 12).map((f) => ({ path: `rows.0.${f.name}`, hint: f.type })), { path: 'body', hint: 'answer' }, { path: 'status' }, { path: 'ok' }, { path: 'headers' }]
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
      { key: 'items', label: 'Items', kind: 'text', placeholder: '{{ steps.Found.rows }}', help: 'A list, such as {{ steps.Api.rows }}, or a number to repeat that many times.' },
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
    description: 'Send a mail from a template or written here, with files attached.',
    inputs: [
      { key: 'mode', label: 'Message', kind: 'select', default: 'write', options: [{ value: 'template', label: 'Use an email template' }, { value: 'write', label: 'Write it here' }] },
      { key: 'template', label: 'Template', kind: 'email_template', when: (i) => i.mode === 'template' },
      { key: 'data', label: 'Fill its fields', kind: 'email_data', when: (i) => i.mode === 'template' && !!i.template },
      { key: 'to', label: 'To', kind: 'text', placeholder: '{{ trigger.row.Email }}', help: 'Addresses separated by commas, or a list of rows or users with an email.' },
      { key: 'cc', label: 'Cc', kind: 'text' },
      { key: 'bcc', label: 'Bcc', kind: 'text' },
      { key: 'replyTo', label: 'Reply to', kind: 'text' },
      { key: 'subject', label: 'Subject', kind: 'text', placeholder: 'Your order {{ trigger.row._id }}', when: (i) => i.mode !== 'template' },
      { key: 'format', label: 'Format', kind: 'select', default: 'text', options: [{ value: 'text', label: 'Plain text' }, { value: 'html', label: 'HTML' }], when: (i) => i.mode !== 'template' },
      { key: 'body', label: 'Body', kind: 'template', when: (i) => i.mode !== 'template' },
      { key: 'attachments', label: 'Attachments', kind: 'text', placeholder: '{{ steps.Invoice.file }}' },
    ],
    outputs: () => [{ path: 'delivery', hint: 'how it went' }, { path: 'subject' }, { path: 'to', hint: 'list' }, { path: 'via' }, { path: 'messageId' }, { path: 'templateVersion' }],
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
      { key: 'content', label: 'Content', kind: 'template', help: 'Markdown: # headings, **bold**, - lists, | tables |, ---. Repeat rows with {{#each steps.Found.rows}} … {{ loop.item.Name }} … {{/each}}.' },
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

// aiOutputs lists what an AI step gives later steps.
const aiOutputs = (s: FlowStep) => {
  const i = s.inputs ?? {}
  if (systemOneStep(i)) {
    const per: Record<string, string[]> = { choice: ['choice', 'confidence', 'probabilities'], score: ['score', 'confidence', 'probabilities', 'legend'], noul: ['noul'] }
    return [{ path: 'data', hint: 'the answers' }, ...((i.questions ?? []) as AIQuestion[]).filter((q) => q.name).flatMap((q) => (per[q.type] ?? []).map((k) => ({ path: `data.${q.name}.${k}`, hint: k === 'noul' ? '0 to 1' : k === 'score' ? 'from 0' : k === 'confidence' ? '0 to 1' : '' }))),
      { path: 'text', hint: 'answers in words' }, { path: 'model' }, { path: 'provider' }, { path: 'usage.input', hint: 'tokens in' }, { path: 'usage.cost', hint: 'USD, OpenRouter' }]
  }
  const data = i.output === 'fields'
    ? [{ path: 'data', hint: 'the fields' }, ...((i.fields ?? []) as AIField[]).filter((f) => f.name).map((f) => ({ path: `data.${f.name}`, hint: f.type || 'text' }))]
    : i.output === 'json' ? [{ path: 'data', hint: 'parsed JSON' }] : []
  return [...data, { path: 'text', hint: 'the answer' }, { path: 'model' }, { path: 'provider' }, { path: 'usage.input', hint: 'tokens in' }, { path: 'usage.output', hint: 'tokens out' }, { path: 'truncated', hint: 'cut off' }]
}

export interface AIField { name: string; type: 'text' | 'number' | 'boolean' | 'list'; description?: string }

// System One models (TypeSafe's Jev, directly or through OpenRouter)
// answer typed questions about a state instead of a prompt.
export interface AIQuestion {
  name: string; type: 'choice' | 'score' | 'noul'; instructions: string
  options?: { name: string; description?: string }[]; levels?: string[]; true?: string; false?: string
}
export const systemOneModel = (provider: string, model: string, prefixes?: string[]) =>
  (prefixes ?? (provider === 'typesafe' ? [''] : provider === 'openrouter' ? ['typesafe/', '~typesafe/'] : [])).some((p) => model.startsWith(p))
const systemOneStep = (i: Record<string, any>) => !!i.questions?.length && systemOneModel(i.provider ?? '', i.model || (i.provider === 'typesafe' ? 'jev-latest' : ''))

// The AI step's settings are drawn by AIStepSettings, which shows only
// those the chosen provider takes; this list names them for drafts.
stepTypes.splice(stepTypes.findIndex((s) => s.type === 'generate_pdf') + 1, 0, {
  type: 'ai', label: 'AI prompt', group: 'Actions', icon: SparklesIcon, tone: tones.ai, name: 'Ai',
  description: 'Ask an AI model for text or fields, or a System One model (Jev) typed questions.',
  inputs: [
    { key: 'provider', label: 'Provider', kind: 'text', help: 'anthropic, openai, gemini, azure, mistral, xai, deepseek, groq, openrouter, typesafe or openai_compatible; empty uses the workspace’s.' },
    { key: 'model', label: 'Model', kind: 'text', help: 'Empty uses the provider’s saved model. For Azure, the deployment.' },
    { key: 'system', label: 'Instructions', kind: 'template' },
    { key: 'prompt', label: 'Prompt', kind: 'template', placeholder: 'Summarise this ticket: {{ trigger.row.Description }}' },
    { key: 'output', label: 'Answer', kind: 'select', default: 'text', options: [{ value: 'text', label: 'Text' }, { value: 'json', label: 'JSON' }, { value: 'fields', label: 'Fields' }] },
    { key: 'fields', label: 'Fields', kind: 'ai_fields', help: 'List of {name, type: text|number|boolean|list, description}; later steps read steps.Name.data.<name>.', when: (i) => i.output === 'fields' },
    { key: 'maxTokens', label: 'Length limit (tokens)', kind: 'text' },
    { key: 'temperature', label: 'Temperature', kind: 'text', help: 'Not for Anthropic.' },
    { key: 'topP', label: 'Top P', kind: 'text', help: 'Not for Anthropic.' },
    { key: 'effort', label: 'Effort', kind: 'text', help: 'Anthropic: low, medium, high, xhigh, max. OpenAI and Azure reasoning models: minimal, low, medium, high.' },
    { key: 'thinking', label: 'Thinking', kind: 'text', help: 'Gemini only: off, low or high.' },
    { key: 'seed', label: 'Seed', kind: 'text' },
    { key: 'stop', label: 'Stop sequences', kind: 'long', help: 'One per line.' },
    { key: 'apiVersion', label: 'API version', kind: 'text', help: 'Azure only.' },
    { key: 'state', label: 'State', kind: 'template', help: 'System One models (provider typesafe, or openrouter with a typesafe/ model) only: the data the questions are about; a whole binding is sent as JSON.' },
    { key: 'questions', label: 'Questions', kind: 'ai_fields', help: 'System One only, instead of a prompt: list of {name, type: choice|score|noul, instructions, options: [{name, description}] for choice, levels: [text] for score, true/false texts for noul}. Read as steps.Name.data.<name>.choice|score|noul, .confidence, .probabilities.' },
  ],
  outputs: aiOutputs,
})

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

export function stepBindings(f: Flow, id: string | null, tables: Table[], queries: Query[], variables: Variable[], scope: Record<string, any> | null = null): BindingGroup[] {
  const groups: BindingGroup[] = [triggerBindings(f, tables)]
  const ctx = id ? before(f.steps, id) : { done: [], loops: [] }
  if (ctx?.loops.length) {
    const loop = ctx.loops[ctx.loops.length - 1]
    // The keys of each item come from the step that made the list.
    const src = /steps\.(\w+)\.rows\b/.exec(loop.inputs?.items ?? '')?.[1]
    let named: FlowStep | undefined
    if (src) walk(f.steps, (s) => { if (s.name === src) named = s })
    const t = named?.type === 'find_rows' ? tableOf(named, tables) : undefined
    const q = named?.type === 'run_query' ? queries.find((x) => x.id === named!.inputs?.query || x.name === named!.inputs?.query) : undefined
    const keys = t ? fieldPaths('loop.item', t) : (q?.fields ?? []).map((x) => ({ path: `loop.item.${x.name}`, hint: x.type }))
    const items = loop.inputs?.items ? `of ${loop.inputs.items.replace(/^\s*\{\{\s*|\s*\}\}\s*$/g, '')}` : ''
    groups.unshift({
      label: `Loop ${loop.name} · current item`, open: true,
      note: scope?.loop?.item !== undefined ? `Each item ${items}; values below are from the first one.` : `Each item ${items}. Run a test or get sample data to see its keys.`,
      items: [{ path: 'loop.item', hint: 'this item' }, ...keys, { path: 'loop.number', hint: 'from 1' }, { path: 'loop.index', hint: 'from 0' }, { path: 'loop.count', hint: 'items' }],
    })
  }
  const vars = new Set<string>()
  for (const s of ctx?.done ?? []) {
    const info = stepInfo(s.type)
    if (!info) continue
    const outs = info.outputs(s, tables, queries)
    if (s.continueOnError) outs.push({ path: 'error', hint: 'when it failed' })
    // Keys the sample has that the step type does not list.
    const sample = scope?.steps?.[s.name]
    if (sample && typeof sample === 'object' && !Array.isArray(sample)) {
      for (const k of Object.keys(sample)) if (!outs.some((o) => o.path.split('.')[0].toLowerCase() === k.toLowerCase())) outs.push({ path: /^[A-Za-z_]\w*$/.test(k) ? k : `[${JSON.stringify(k)}]` })
    }
    if (outs.length) groups.push({ label: `${s.name} · ${info.label}`, items: outs.map((o) => ({ path: `steps.${s.name}${o.path.startsWith('[') ? '' : '.'}${o.path}`, hint: o.hint })) })
  }
  walk(f.steps, (s) => { if (s.type === 'set_variable' && s.inputs?.name && s.id !== id) vars.add(s.inputs.name) })
  if (vars.size) groups.push({ label: 'Variables', items: [...vars].map((v) => ({ path: `vars.${v}` })) })
  if (variables.length) groups.push({ label: 'App variables', items: variables.map((v) => ({ path: `env.${v.name}`, hint: v.secret ? 'secret' : '' })) })
  groups.push({ label: 'Who started it', items: [{ path: 'user.email' }, { path: 'user.name' }, { path: 'user.id' }] })
  groups.push({ label: 'App', items: [{ path: 'app.name' }, { path: 'app.slug' }] })
  return groups
}

export const flowHelp = 'Pipes: | upper, | number:2, | default:"x", | json, | join:", ". Formulas: {{ = trigger.row.Qty * 2 }}. Repeat: {{#each list}}{{ loop.item.Name }}{{/each}}; branch: {{#if x}}…{{else}}…{{/if}}.'

// summary is a step's one-line description on the canvas.
export function summary(s: FlowStep, tables: Table[], queries: Query[], templateName?: (id: string) => string | undefined): string {
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
    case 'send_email': return `To ${i.to || '…'}${i.mode === 'template' ? ` · ${templateName?.(i.template) ?? 'choose a template'}` : i.subject ? ` · ${i.subject}` : ''}`
    case 'http_request': return `${i.method ?? 'POST'} ${i.url || '…'}`
    case 'generate_pdf': return `${i.title || i.fileName || 'Untitled'}${i.field ? ` → ${i.field}` : ''}`
    case 'ai': {
      const who = i.model || i.provider || 'AI'
      if (systemOneStep(i)) return `${who} asks ${((i.questions ?? []) as AIQuestion[]).map((q) => q.name).filter(Boolean).join(', ') || 'questions'}`
      if (i.output === 'fields') return `${who} → ${((i.fields ?? []) as AIField[]).map((f) => f.name).filter(Boolean).join(', ') || 'fields'}`
      return `${who}${i.output === 'json' ? ' → JSON' : ''}: ${String(i.prompt || '…').replace(/\s+/g, ' ').slice(0, 80)}`
    }
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
