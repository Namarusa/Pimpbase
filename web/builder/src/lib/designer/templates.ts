// Ready-made screens: a list with an add dialog, a detail page with an edit
// form, and a dashboard, all made from one table.
import { fieldComponent, newNode } from '../screen/registry'
import { writable } from '../format'
import type { Table } from '../api'
import type { Node } from '../screen/types'

export const slug = (s: string) => s.toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'screen'

const singular = (s: string) => (s.endsWith('ies') ? s.slice(0, -3) + 'y' : s.endsWith('s') && !s.endsWith('ss') ? s.slice(0, -1) : s)

function root(children: Node[]): Node {
  return { id: 'root', type: 'container', props: { direction: 'column', gap: 'lg', padding: 'lg', align: 'stretch', justify: 'start', background: 'none', maxWidth: 'xl' }, children }
}

const n = (type: string, props: Record<string, any> = {}, children?: Node[]): Node => {
  const node = newNode(type, props)
  if (children) node.children = children
  return node
}

// formFields makes a field component for every column people can edit.
export function formFields(t: Table): Node[] {
  return t.fields.filter(writable).map((f) => {
    const type = fieldComponent(f.type)
    if (type === 'file_input') return n(type, { field: f.name, label: f.name, required: !!f.required, accept: 'any', span: 'full' })
    return type ? n(type, { field: f.name, label: f.name, required: !!f.required }) : null
  }).filter(Boolean) as Node[]
}

const primaryName = (t: Table) => t.fields.find((f) => f.id === t.primaryField)?.name ?? t.fields[0]?.name ?? '_id'

export function listScreen(t: Table): { name: string; route: string; definition: Node } {
  const s = slug(t.name)
  const dlg = n('dialog', { title: `New ${singular(t.name).toLowerCase()}`, description: `Add a row to ${t.name}.`, size: 'lg' })
  dlg.children = [n('form', { table: t.id, mode: 'create', columns: '2', submitText: `Add ${singular(t.name).toLowerCase()}`, successMessage: `${singular(t.name)} added`, onSubmit: [{ type: 'close_dialog', target: dlg.id }] }, formFields(t).map((f) => (f.type === 'textarea_input' ? { ...f, props: { ...f.props, span: 'full' } } : f)))]
  return {
    name: t.name,
    route: `/${s}`,
    definition: root([
      n('container', { direction: 'row', align: 'center', justify: 'between', gap: 'md' }, [
        n('heading', { text: t.name, subtitle: `Every ${singular(t.name).toLowerCase()} in one place.`, level: 'h2' }),
        n('button', { text: `New ${singular(t.name).toLowerCase()}`, icon: 'plus', actions: [{ type: 'open_dialog', target: dlg.id }] }),
      ]),
      n('table', {
        table: t.id, columns: t.fields.slice(0, 6).map((f) => f.name), search: true, allowDelete: true, allowExport: true,
        onRowClick: [{ type: 'navigate', route: `/${s}/{{ row._id }}` }],
      }),
      dlg,
    ]),
  }
}

export function detailScreen(t: Table): { name: string; route: string; definition: Node } {
  const s = slug(t.name)
  return {
    name: `${singular(t.name)} details`,
    route: `/${s}/:id`,
    definition: root([
      n('row_provider', { table: t.id, rowId: '{{ url.id }}', gap: 'lg' }, [
        n('container', { direction: 'row', align: 'center', gap: 'sm' }, [
          n('button', { text: `All ${t.name.toLowerCase()}`, variant: 'ghost', size: 'sm', icon: 'arrow-left', printHide: true, actions: [{ type: 'navigate', route: `/${s}` }] }),
          n('spacer', { grow: true }),
          n('button', { text: 'Print', variant: 'outline', size: 'sm', icon: 'printer', printHide: true, actions: [{ type: 'print', title: `${singular(t.name)} {{ row.${primaryName(t)} }}` }] }),
          n('button', {
            text: 'Delete', printHide: true, variant: 'outline', size: 'sm', icon: 'trash',
            actions: [
              { type: 'confirm', title: `Delete this ${singular(t.name).toLowerCase()}?`, message: 'This cannot be undone.' },
              { type: 'delete_row', table: t.id, rowId: '{{ url.id }}' },
              { type: 'toast', message: 'Deleted', kind: 'success' },
              { type: 'navigate', route: `/${s}` },
            ],
          }),
        ]),
        n('heading', { text: `{{ row.${primaryName(t)} }}`, subtitle: `${singular(t.name)} #{{ row._id }}`, level: 'h2' }),
        n('tabs', {}, [
          n('tab', { title: 'Overview' }, [n('details', { columns: '2', fields: t.fields.filter((f) => f.type !== 'attachment').map((f) => f.name) })]),
          // Each attachment field gets a tab that shows its PDFs.
          ...t.fields.filter((f) => f.type === 'attachment').map((f) => n('tab', { title: f.name }, [n('pdf_viewer', { field: f.name, height: 'xl' })])),
          n('tab', { title: 'Edit' }, [
            n('card', { title: `Edit ${singular(t.name).toLowerCase()}`, description: 'Changes are saved to the table.' }, [
              n('form', { table: t.id, mode: 'update', rowId: '{{ url.id }}', columns: '2', submitText: 'Save changes', successMessage: 'Changes saved' }, formFields(t)),
            ]),
          ]),
        ]),
      ]),
    ]),
  }
}

export function dashboardScreen(t: Table): { name: string; route: string; definition: Node } {
  const num = t.fields.find((f) => f.type === 'number' || (f.type === 'formula' && f.resultType === 'number'))
  const select = t.fields.find((f) => f.type === 'single_select') ?? t.fields.find((f) => f.type === 'text')
  const date = t.fields.find((f) => f.type === 'date')
  const stats = [n('stat', { title: `${t.name}`, table: t.id, op: 'count', icon: 'layers', caption: 'Rows in the table' })]
  if (num) {
    stats.push(n('stat', { title: `Total ${num.name.toLowerCase()}`, table: t.id, op: 'sum', field: num.name, icon: 'trending-up', format: 'decimal', caption: `Sum of ${num.name}` }))
    stats.push(n('stat', { title: `Average ${num.name.toLowerCase()}`, table: t.id, op: 'avg', field: num.name, icon: 'activity', format: 'decimal', caption: `Mean of ${num.name}` }))
  } else if (select?.options?.length) {
    stats.push(n('stat', { title: select.options[0], table: t.id, filter: `[${select.name}] = "${select.options[0]}"`, op: 'count', icon: 'check' }))
  }
  const charts: Node[] = []
  if (select) charts.push(n('chart', { table: t.id, title: `${t.name} by ${select.name.toLowerCase()}`, chartType: 'bar', labelField: select.name, op: num ? 'sum' : 'count', valueFields: num ? [num.name] : [], sortBy: 'value' }))
  if (date) charts.push(n('chart', { table: t.id, title: `${t.name} over time`, chartType: 'area', labelField: date.name, op: num ? 'sum' : 'count', valueFields: num ? [num.name] : [], dateGroup: 'month' }))
  if (select) charts.push(n('chart', { table: t.id, title: `Share by ${select.name.toLowerCase()}`, chartType: 'donut', labelField: select.name, op: 'count' }))
  return {
    name: `${t.name} dashboard`,
    route: `/${slug(t.name)}-dashboard`,
    definition: root([
      n('heading', { text: `${t.name} overview`, subtitle: 'Live numbers from your data.', level: 'h2' }),
      n('grid', { columns: String(Math.min(3, stats.length)) }, stats),
      ...(charts.length ? [n('grid', { columns: charts.length > 1 ? '2' : '1' }, charts)] : []),
      n('list', { table: t.id, title: `Latest ${t.name.toLowerCase()}`, titleField: primaryName(t), subtitleField: t.fields[1]?.name ?? '', badgeField: select?.type === 'single_select' ? select.name : '', sort: '', limit: 5 }),
    ]),
  }
}

export function blankScreen(name: string, route: string): { name: string; route: string; definition: Node } {
  return { name, route, definition: root([n('heading', { text: name, level: 'h2', subtitle: '' })]) }
}
