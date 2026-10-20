// Actions run in order when a button is clicked, a row is chosen or a form
// is saved. A failing action stops the rest.
import { api, rowKey } from '../api'
import { resolve, resolveText } from './bindings'
import type { Runtime } from './context.svelte'
import type { Action, Scope } from './types'

export interface ActionInfo {
  type: string
  label: string
  // fields are the settings the action editor shows.
  fields: { key: string; label: string; kind: 'text' | 'table' | 'dialog' | 'select' | 'values' | 'boolean' | 'component' | 'query' | 'params' | 'flow' | 'flowparams'; options?: { value: string; label: string }[]; placeholder?: string }[]
}

export const actionTypes: ActionInfo[] = [
  { type: 'navigate', label: 'Go to screen', fields: [{ key: 'route', label: 'Route', kind: 'text', placeholder: '/orders/{{ row._id }}' }] },
  { type: 'open_url', label: 'Open web address', fields: [{ key: 'url', label: 'Address', kind: 'text', placeholder: 'https://…' }, { key: 'newTab', label: 'In a new tab', kind: 'boolean' }] },
  { type: 'save_row', label: 'Save row', fields: [
    { key: 'table', label: 'Table', kind: 'table' },
    { key: 'rowId', label: 'Row id (blank adds a row)', kind: 'text', placeholder: '{{ row._id }}' },
    { key: 'values', label: 'Values', kind: 'values' },
  ] },
  { type: 'delete_row', label: 'Delete row', fields: [
    { key: 'table', label: 'Table', kind: 'table' },
    { key: 'rowId', label: 'Row id', kind: 'text', placeholder: '{{ row._id }}' },
  ] },
  { type: 'run_query', label: 'Run API query', fields: [
    { key: 'query', label: 'Query', kind: 'query' },
    { key: 'params', label: 'Parameters', kind: 'params' },
    { key: 'stateKey', label: 'Keep the answer in state (name)', kind: 'text', placeholder: 'weather' },
  ] },
  { type: 'run_flow', label: 'Run flow', fields: [
    { key: 'flow', label: 'Flow', kind: 'flow' },
    { key: 'params', label: 'Parameters', kind: 'flowparams' },
    { key: 'stateKey', label: 'Keep the result in state (name)', kind: 'text', placeholder: 'quote' },
  ] },
  { type: 'open_dialog', label: 'Open dialog', fields: [{ key: 'target', label: 'Dialog', kind: 'dialog' }] },
  { type: 'close_dialog', label: 'Close dialog', fields: [{ key: 'target', label: 'Dialog (blank closes all)', kind: 'dialog' }] },
  { type: 'toast', label: 'Show notification', fields: [
    { key: 'message', label: 'Message', kind: 'text' },
    { key: 'kind', label: 'Kind', kind: 'select', options: [{ value: 'success', label: 'Success' }, { value: 'info', label: 'Info' }, { value: 'error', label: 'Error' }] },
  ] },
  { type: 'refresh', label: 'Refresh data', fields: [{ key: 'target', label: 'Component (blank refreshes all)', kind: 'component' }] },
  { type: 'set_state', label: 'Set state value', fields: [{ key: 'key', label: 'Name', kind: 'text', placeholder: 'selectedId' }, { key: 'value', label: 'Value', kind: 'text', placeholder: '{{ row._id }}' }] },
  { type: 'confirm', label: 'Ask to confirm', fields: [{ key: 'title', label: 'Title', kind: 'text' }, { key: 'message', label: 'Message', kind: 'text' }] },
  { type: 'export_csv', label: 'Export CSV', fields: [{ key: 'table', label: 'Table', kind: 'table' }, { key: 'filter', label: 'Filter formula', kind: 'text' }] },
  { type: 'print', label: 'Print / save as PDF', fields: [{ key: 'title', label: 'File name when saved as PDF', kind: 'text', placeholder: 'Order {{ row._id }}' }] },
  { type: 'sign_out', label: 'Sign out', fields: [] },
]

export const actionInfo = (type: string) => actionTypes.find((a) => a.type === type)

// describe summarises an action for the action list.
export function describe(a: Action, rt?: { table: (id?: string) => { name: string } | undefined; flows?: { id: string; name: string }[] }): string {
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
    case 'set_state': return `Set state.${a.key || '…'}`
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
          rt.toast(a.kind ?? 'success', resolveText(a.message, scope) || 'Done')
          break
        case 'refresh':
          rt.reload(a.target || undefined)
          break
        case 'set_state':
          if (a.key) rt.state[a.key] = resolve(a.value, scope)
          break
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
