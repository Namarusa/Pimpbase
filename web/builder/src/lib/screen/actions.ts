// Actions run in order when a button is clicked, a row is chosen or a form
// is saved. A failing action stops the rest.
import { api, rowKey } from '../api'
import { checkCondition, conditionOps, resolve, resolveText } from './bindings'
import type { Runtime } from './context.svelte'
import type { Action, Scope } from './types'
import { firstValue, type Variable } from './variables'
import { buildPath, navTarget, routeParams, type QueryValue, type ScreenRef } from './navigation'

export interface ActionInfo {
  type: string
  label: string
  // fields are the settings the action editor shows.
  fields: ActionField[]
  // group places the action in the Add action menu.
  group: 'Data' | 'Screen' | 'Form' | 'Logic' | 'App'
}

export interface ActionField {
  key: string
  label: string
  // component picks one of the screen's components: data ones to
  // refresh; input ones (fields) to set; form ones to clear or check;
  // any to scroll to.
  kind: 'text' | 'table' | 'dialog' | 'select' | 'values' | 'boolean' | 'component' | 'input' | 'form' | 'anycomponent' | 'query' | 'params' | 'flow' | 'flowparams' | 'screen'
  options?: { value: string; label: string }[]
  placeholder?: string
  // show hides the field unless it returns true.
  show?: (a: Action) => boolean
}

const variableModes = [{ value: 'set', label: 'Set it to a value' }, { value: 'reset', label: 'Reset it to its first value' }, { value: 'delete', label: 'Clear it' }]
const opsNeedingValue = new Set(['equals', 'not_equals', 'contains', 'gt', 'lt'])

export const actionTypes: ActionInfo[] = [
  // Data
  { type: 'save_row', group: 'Data', label: 'Save row', fields: [
    { key: 'table', label: 'Table', kind: 'table' },
    { key: 'rowId', label: 'Row id (blank adds a row)', kind: 'text', placeholder: '{{ row._id }}' },
    { key: 'values', label: 'Values', kind: 'values' },
  ] },
  { type: 'duplicate_row', group: 'Data', label: 'Duplicate row', fields: [
    { key: 'table', label: 'Table', kind: 'table' },
    { key: 'rowId', label: 'Row id', kind: 'text', placeholder: '{{ row._id }}' },
  ] },
  { type: 'delete_row', group: 'Data', label: 'Delete row', fields: [
    { key: 'table', label: 'Table', kind: 'table' },
    { key: 'rowId', label: 'Row id', kind: 'text', placeholder: '{{ row._id }}' },
  ] },
  { type: 'fetch_row', group: 'Data', label: 'Fetch row', fields: [
    { key: 'table', label: 'Table', kind: 'table' },
    { key: 'rowId', label: 'Row id', kind: 'text', placeholder: '{{ url.id }}' },
    { key: 'stateKey', label: 'Keep it in a variable (name)', kind: 'text', placeholder: 'customer' },
  ] },
  { type: 'run_query', group: 'Data', label: 'Run API query', fields: [
    { key: 'query', label: 'Query', kind: 'query' },
    { key: 'params', label: 'Parameters', kind: 'params' },
    { key: 'stateKey', label: 'Keep the answer in a variable (name)', kind: 'text', placeholder: 'weather' },
  ] },
  { type: 'run_flow', group: 'Data', label: 'Run flow', fields: [
    { key: 'flow', label: 'Flow', kind: 'flow' },
    { key: 'params', label: 'Parameters', kind: 'flowparams' },
    { key: 'stateKey', label: 'Keep the result in a variable (name)', kind: 'text', placeholder: 'quote' },
  ] },
  { type: 'refresh', group: 'Data', label: 'Refresh data', fields: [{ key: 'target', label: 'Component (blank refreshes all)', kind: 'component' }] },
  { type: 'set_state', group: 'Data', label: 'Update variable', fields: [
    { key: 'key', label: 'Variable', kind: 'text', placeholder: 'search' },
    { key: 'mode', label: 'Change', kind: 'select', options: variableModes },
    { key: 'value', label: 'Value', kind: 'text', placeholder: '{{ value }}', show: (a) => !a.mode || a.mode === 'set' },
  ] },
  { type: 'export_csv', group: 'Data', label: 'Export CSV', fields: [{ key: 'table', label: 'Table', kind: 'table' }, { key: 'filter', label: 'Filter formula', kind: 'text' }] },
  { type: 'clear_selection', group: 'Data', label: 'Clear row selection', fields: [{ key: 'target', label: 'Table', kind: 'anycomponent' }] },
  { type: 'checkout', group: 'Data', label: 'Take a payment', fields: [
    { key: 'flow', label: 'Payment flow', kind: 'flow' },
    { key: 'params', label: 'Parameters', kind: 'flowparams' },
  ] },
  // Screen
  { type: 'navigate', group: 'Screen', label: 'Go to screen', fields: [{ key: 'screen', label: 'Screen', kind: 'screen' }] },
  { type: 'open_url', group: 'Screen', label: 'Open web address', fields: [{ key: 'url', label: 'Address', kind: 'text', placeholder: 'https://…' }, { key: 'newTab', label: 'In a new tab', kind: 'boolean' }] },
  { type: 'open_dialog', group: 'Screen', label: 'Open dialog or side panel', fields: [{ key: 'target', label: 'Dialog', kind: 'dialog' }] },
  { type: 'close_dialog', group: 'Screen', label: 'Close dialog or side panel', fields: [{ key: 'target', label: 'Dialog (blank closes all)', kind: 'dialog' }] },
  { type: 'toast', group: 'Screen', label: 'Show notification', fields: [
    { key: 'message', label: 'Message', kind: 'text' },
    { key: 'kind', label: 'Kind', kind: 'select', options: [{ value: 'success', label: 'Success' }, { value: 'info', label: 'Info' }, { value: 'warning', label: 'Warning' }, { value: 'error', label: 'Error' }] },
  ] },
  { type: 'scroll_to', group: 'Screen', label: 'Scroll to component', fields: [{ key: 'target', label: 'Component', kind: 'anycomponent' }] },
  { type: 'copy', group: 'Screen', label: 'Copy to clipboard', fields: [{ key: 'text', label: 'Text', kind: 'text', placeholder: '{{ row.Email }}' }] },
  { type: 'download', group: 'Screen', label: 'Download file', fields: [{ key: 'url', label: 'File address', kind: 'text', placeholder: '{{ row.Invoice.url }}' }, { key: 'name', label: 'File name', kind: 'text', placeholder: 'invoice.pdf' }] },
  { type: 'print', group: 'Screen', label: 'Print / save as PDF', fields: [{ key: 'title', label: 'File name when saved as PDF', kind: 'text', placeholder: 'Order {{ row._id }}' }] },
  // Form
  { type: 'set_value', group: 'Form', label: 'Update field value', fields: [
    { key: 'target', label: 'Field', kind: 'input' },
    { key: 'mode', label: 'Change', kind: 'select', options: [{ value: 'set', label: 'Set it to a value' }, { value: 'reset', label: 'Reset it to its default' }] },
    { key: 'value', label: 'Value', kind: 'text', show: (a) => !a.mode || a.mode === 'set' },
  ] },
  { type: 'focus', group: 'Form', label: 'Focus field', fields: [{ key: 'target', label: 'Field', kind: 'input' }] },
  { type: 'clear_form', group: 'Form', label: 'Clear form', fields: [{ key: 'target', label: 'Form', kind: 'form' }] },
  { type: 'validate_form', group: 'Form', label: 'Check form', fields: [{ key: 'target', label: 'Form', kind: 'form' }] },
  // Logic
  { type: 'continue_if', group: 'Logic', label: 'Continue only if', fields: [
    { key: 'left', label: 'Value', kind: 'text', placeholder: '{{ value }}' },
    { key: 'op', label: 'Test', kind: 'select', options: conditionOps },
    { key: 'right', label: 'Compared with', kind: 'text', show: (a) => opsNeedingValue.has(a.op ?? 'equals') },
  ] },
  { type: 'confirm', group: 'Logic', label: 'Ask to confirm', fields: [{ key: 'title', label: 'Title', kind: 'text' }, { key: 'message', label: 'Message', kind: 'text' }] },
  // App
  { type: 'sign_out', group: 'App', label: 'Sign out', fields: [] },
]

export const actionInfo = (type: string) => actionTypes.find((a) => a.type === type)

// describe summarises an action for the action list.
export function describe(a: Action, rt?: { table: (id?: string) => { name: string } | undefined; flows?: { id: string; name: string }[]; screens?: ScreenRef[] }): string {
  if (a.type === 'navigate' && rt?.screens) {
    const t = navTarget(a, rt.screens)
    const s = t && rt.screens.find((x) => x.id === t.screen)
    if (s) return `Go to ${s.name || s.route}`
    if (a.screen) return 'Go to a deleted screen'
  }
  if (a.type === 'checkout') return `Take a payment with ${(a.flow && rt?.flows?.find((f) => f.id === a.flow)?.name) || '…'}`
  if (a.type === 'run_flow') return `Run ${(a.flow && rt?.flows?.find((f) => f.id === a.flow)?.name) || 'flow'}`
  if (a.type === 'run_query') return `Run ${(a.query && rt?.table(`query:${a.query}`)?.name) || 'API query'}`
  switch (a.type) {
    case 'navigate': return `Go to ${a.route || '…'}`
    case 'open_url': return `Open ${a.url || '…'}`
    case 'save_row': return `Save row in ${rt?.table(a.table)?.name ?? '…'}`
    case 'delete_row': return `Delete row in ${rt?.table(a.table)?.name ?? '…'}`
    case 'open_dialog': return 'Open dialog'
    case 'close_dialog': return 'Close dialog'
    case 'toast': return `Notify “${a.message || '…'}”`
    case 'refresh': return 'Refresh data'
    case 'set_state': return a.mode === 'delete' ? `Clear ${a.key || 'variable'}` : a.mode === 'reset' ? `Reset ${a.key || 'variable'}` : `Set ${a.key || 'variable'}`
    case 'duplicate_row': return `Duplicate row in ${rt?.table(a.table)?.name ?? '…'}`
    case 'fetch_row': return `Fetch row from ${rt?.table(a.table)?.name ?? '…'}`
    case 'set_value': return a.mode === 'reset' ? 'Reset field' : 'Update field value'
    case 'focus': return 'Focus field'
    case 'clear_form': return 'Clear form'
    case 'validate_form': return 'Check form'
    case 'continue_if': return `Continue if ${a.left || '…'} ${conditionOps.find((o) => o.value === (a.op ?? 'equals'))?.label ?? ''}${opsNeedingValue.has(a.op ?? 'equals') ? ` ${a.right ?? ''}` : ''}`
    case 'clear_selection': return 'Clear row selection'
    case 'scroll_to': return 'Scroll to component'
    case 'copy': return 'Copy to clipboard'
    case 'download': return 'Download file'
    case 'confirm': return `Confirm “${a.title || a.message || '…'}”`
    case 'export_csv': return `Export ${rt?.table(a.table)?.name ?? '…'} as CSV`
    case 'print': return 'Print / save as PDF'
    case 'sign_out': return 'Sign out'
  }
  return a.type
}

const idOf = (v: any) => {
  if (v && typeof v === 'object' && '_id' in v) return Number(v._id)
  const n = Number(v)
  return isNaN(n) || v === '' || v === null || v === undefined ? null : n
}

// runActions runs a list of actions. It returns false when one failed or
// the person cancelled a confirmation.
export async function runActions(actions: Action[] | undefined, rt: Runtime, base: Scope, extra: Partial<Scope> = {}): Promise<boolean> {
  if (!actions?.length) return true
  if (rt.editing) return true
  const scope: Scope = { ...base, ...extra }
  for (const a of actions) {
    try {
      switch (a.type) {
        case 'navigate': {
          if (a.screen) {
            const s = rt.screens.find((x) => x.id === a.screen)
            if (!s) throw new Error('the screen this goes to was deleted')
            const params = Object.fromEntries(routeParams(s.route).map((p) => [p, resolveText(a.params?.[p] ?? '', scope)]))
            const missing = Object.keys(params).filter((p) => !params[p])
            if (missing.length) throw new Error(`${s.name || s.route} needs a value for ${missing.join(', ')}`)
            const query = ((a.query ?? []) as QueryValue[]).map((q) => ({ key: q.key, value: resolveText(q.value, scope) }))
            rt.navigate(buildPath(s.route, params, query))
            break
          }
          const route = resolveText(a.route, scope)
          if (route) rt.navigate(route.startsWith('/') ? route : '/' + route)
          break
        }
        case 'open_url': {
          const url = resolveText(a.url, scope)
          if (/^(https?:|mailto:|tel:)/.test(url)) {
            if (a.newTab) window.open(url, '_blank', 'noopener')
            else location.href = url
          }
          break
        }
        case 'save_row': {
          const t = rt.table(a.table)
          if (!t) throw new Error('choose the table this action saves to')
          const values: Record<string, any> = {}
          for (const v of (a.values ?? []) as { field: string; value: string }[]) {
            if (!v.field) continue
            const f = t.fields.find((x) => x.name === v.field)
            let val = resolve(v.value, scope)
            if (f?.type === 'link') val = Array.isArray(val) ? val.map(idOf) : idOf(val)
            if (f?.type === 'number' && typeof val === 'string') val = val === '' ? null : Number(val)
            if (f?.type === 'boolean' && typeof val === 'string') val = val === 'true' || val === 'Yes'
            values[v.field] = val
          }
          const id = rowKey(resolve(a.rowId, scope))
          scope.result = id ? await api.updateRow(rt.appId, t.id, id, values) : await api.createRow(rt.appId, t.id, values)
          rt.reload()
          break
        }
        case 'delete_row': {
          const t = rt.table(a.table)
          const id = rowKey(resolve(a.rowId, scope))
          if (!t || !id) throw new Error('the delete action needs a table and a row id')
          await api.deleteRows(rt.appId, t.id, [id])
          rt.reload()
          break
        }
        case 'run_query': {
          const q = rt.query(a.query)
          if (!q) throw new Error('choose the API query this action runs')
          const params: Record<string, any> = {}
          for (const [k, v] of Object.entries((a.params ?? {}) as Record<string, string>)) if (v !== '' && v !== undefined) params[k] = resolve(v, scope)
          const res = await api.runQuery(rt.appId, q.id, params, rt.env)
          if (!res.ok) throw new Error(`${q.name} answered ${res.status} ${res.statusText}`.trim())
          // Later actions read the answer as {{ result }}.
          scope.result = res.body
          if (a.stateKey) rt.state[a.stateKey] = res.body
          rt.reload()
          break
        }
        case 'run_flow': {
          const f = rt.flow(a.flow)
          if (!f) throw new Error('choose the flow this action runs')
          const params: Record<string, any> = {}
          for (const [k, v] of Object.entries((a.params ?? {}) as Record<string, string>)) if (v !== '' && v !== undefined) params[k] = resolve(v, scope)
          const res = await api.runFlow(rt.appId, f.id, params)
          if (res.status === 'failed') throw new Error(res.error || `${f.name} failed`)
          // Later actions read what the flow returned as {{ result }}.
          scope.result = res.output
          if (a.stateKey) rt.state[a.stateKey] = res.output
          rt.reload()
          break
        }
        case 'checkout': {
          // The flow makes a checkout page (Stripe Checkout, a payment
          // link) and returns its address; the buyer goes there.
          const f = rt.flow(a.flow)
          if (!f) throw new Error('choose the payment flow this action runs')
          const params: Record<string, any> = {}
          for (const [k, v] of Object.entries((a.params ?? {}) as Record<string, string>)) if (v !== '' && v !== undefined) params[k] = resolve(v, scope)
          const res = await api.runFlow(rt.appId, f.id, params)
          if (res.status === 'failed') throw new Error(res.error || `${f.name} failed`)
          const out = res.output
          const url = typeof out === 'string' ? out : out?.url
          if (typeof url !== 'string' || !/^https:\/\//.test(url)) throw new Error(`${f.name} did not return a payment page address`)
          location.href = url
          return false
        }
        case 'open_dialog':
          if (a.target) {
            rt.dialogScopes[a.target] = extra
            rt.dialogs[a.target] = true
          }
          break
        case 'close_dialog':
          if (a.target) rt.dialogs[a.target] = false
          else for (const k of Object.keys(rt.dialogs)) rt.dialogs[k] = false
          break
        case 'toast':
          rt.toast(a.kind === 'warning' ? 'warning' : a.kind ?? 'success', resolveText(a.message, scope) || 'Done')
          break
        case 'refresh':
          rt.reload(a.target || undefined)
          break
        case 'set_state': {
          const key = resolveText(a.key, scope).trim()
          if (!key) break
          if (a.mode === 'delete') delete rt.state[key]
          else if (a.mode === 'reset') {
            const v = (rt.variables as Variable[]).find((x) => x.name === key)
            rt.state[key] = v ? firstValue(v, scope) : ''
          } else rt.state[key] = resolve(a.value, scope)
          // Later actions see the new value.
          scope.state = rt.state
          break
        }
        case 'duplicate_row': {
          const t = rt.table(a.table)
          const id = rowKey(resolve(a.rowId, scope))
          if (!t || !id) throw new Error('the duplicate action needs a table and a row id')
          const row = await api.row(rt.appId, t.id, id)
          const values: Record<string, any> = {}
          for (const f of t.fields) {
            if (f.type === 'formula' || f.type === 'autonumber' || !(f.name in row)) continue
            const v = row[f.name]
            values[f.name] = f.type === 'link' ? (Array.isArray(v) ? v.map(idOf) : idOf(v)) : v
          }
          scope.result = await api.createRow(rt.appId, t.id, values)
          rt.reload()
          break
        }
        case 'fetch_row': {
          const t = rt.table(a.table)
          const id = rowKey(resolve(a.rowId, scope))
          if (!t || !id) throw new Error('the fetch action needs a table and a row id')
          scope.result = await api.row(rt.appId, t.id, id)
          if (a.stateKey) rt.state[a.stateKey] = scope.result
          break
        }
        case 'set_value': {
          const c = a.target && rt.control(a.target)
          if (!c) break // the field is not on the screen now
          if (a.mode === 'reset') c.clear?.()
          else c.setValue?.(resolve(a.value, scope))
          break
        }
        case 'focus':
          if (a.target) rt.control(a.target)?.focus?.()
          break
        case 'clear_form':
          if (a.target) rt.control(a.target)?.clear?.()
          break
        case 'validate_form': {
          const c = a.target && rt.control(a.target)
          if (c && c.validate && !c.validate()) return false
          break
        }
        case 'clear_selection':
          if (a.target) rt.control(a.target)?.clear?.()
          break
        case 'continue_if':
          if (!checkCondition({ left: a.left ?? '', op: a.op ?? 'equals', right: a.right }, scope)) return false
          break
        case 'scroll_to':
          if (a.target) document.querySelector(`[data-node="${CSS.escape(a.target)}"]`)?.scrollIntoView({ behavior: 'smooth', block: 'center' })
          break
        case 'copy':
          await navigator.clipboard.writeText(resolveText(a.text, scope))
          rt.toast('success', 'Copied')
          break
        case 'download': {
          const url = resolveText(a.url, scope)
          if (!url) throw new Error('there is no file to download')
          const link = document.createElement('a')
          link.href = url
          link.download = resolveText(a.name, scope) || ''
          link.rel = 'noopener'
          document.body.appendChild(link)
          link.click()
          link.remove()
          break
        }
        case 'confirm':
          if (!(await rt.confirm(resolveText(a.title, scope) || 'Are you sure?', resolveText(a.message, scope)))) return false
          break
        case 'export_csv': {
          const t = rt.table(a.table)
          if (t) window.open(api.exportURL(rt.appId, t.id, resolveText(a.filter, scope)), '_blank')
          break
        }
        case 'print':
          printScreen(resolveText(a.title, scope))
          break
        case 'sign_out':
          await api.logout()
          location.reload()
          break
      }
    } catch (e: any) {
      rt.toast('error', e?.message ?? String(e))
      return false
    }
  }
  return true
}

// printScreen opens the browser's print dialog, where "Save as PDF" makes a
// PDF of the screen. The app's navigation and components marked "hide when
// printing" are left out, and dark themes print light so text stays legible.
function printScreen(title: string) {
  const html = document.documentElement
  const wasDark = html.classList.contains('dark')
  const oldTitle = document.title
  if (title) document.title = title.replace(/[\\/:*?"<>|]+/g, '-')
  html.classList.remove('dark')
  const restore = () => {
    if (wasDark) html.classList.add('dark')
    document.title = oldTitle
    window.removeEventListener('afterprint', restore)
  }
  window.addEventListener('afterprint', restore)
  window.print()
}
