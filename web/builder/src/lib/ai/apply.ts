// Applying a reviewed draft: it goes through the same API calls the
// builder's own editors make, so every check and permission applies.
import { api, type AIDraft, type Field, type Table } from '../api'
import { fillDefaults } from './catalog'

// comparable is what the diff shows: the draft, and the current object
// reduced to the same keys.
export function comparable(d: AIDraft): { before: string; after: string } {
  const after = d.kind === 'screen' ? { ...d.draft, definition: fillDefaults(d.draft.definition) } : d.draft
  let before: any = null
  if (d.current) {
    before = Object.fromEntries(Object.keys(after).map((k) => [k, d.current[k]]))
    if (d.kind === 'screen' && before.definition) before.definition = fillDefaults(before.definition)
  }
  return { before: before ? text(before) : '', after: text(after) }
}

// text prints settings in name order, so that a reordered setting does not
// show as a change.
function text(v: any): string {
  return JSON.stringify(
    v,
    function (this: any, key, val) {
      if (key === 'props' && val && typeof val === 'object' && !Array.isArray(val)) {
        return Object.fromEntries(Object.keys(val).sort().map((k) => [k, val[k]]))
      }
      return val
    },
    2,
  )
}

// tableDiff is the table draft as before/after text, field by field.
export function tableText(tables: Table[], d: AIDraft): { before: string; after: string } {
  const line = (f: Partial<Field> & { linkTable?: string }) => {
    let extra = ''
    if (f.options?.length) extra = ` (${f.options.join(', ')})`
    if (f.type === 'link') {
      const t = String(f.linkTable ?? '')
      extra = ` → ${t.startsWith('draft:') ? t.slice(6) : tables.find((x) => x.id === t)?.name ?? t}${f.linkMany ? ' (many)' : ''}`
    }
    if (f.type === 'formula') extra = ` = ${f.formula}`
    return `  ${f.name}: ${f.type}${extra}${f.required ? ' · required' : ''}`
  }
  const before: string[] = [], after: string[] = []
  for (const t of d.draft.tables as any[]) {
    const existing = t.existing ? tables.find((x) => x.id === t.id) : undefined
    if (existing) {
      const head = `${existing.name} (existing table)`
      before.push(head, ...existing.fields.map(line), '')
      after.push(head, ...existing.fields.map(line), ...t.fields.map(line), '')
    } else after.push(`${t.name} (new table)`, ...t.fields.map(line), '')
  }
  return { before: before.join('\n'), after: after.join('\n') }
}

// applyDraft makes the change and returns where to look at it.
export async function applyDraft(app: { id: string; slug: string }, d: AIDraft): Promise<string> {
  const base = `/apps/${app.slug}`
  switch (d.kind) {
    case 'table': {
      const made: Record<string, string> = {}
      const later: { table: () => string; field: any }[] = []
      const drafts = d.draft.tables as any[]
      // New tables first, without links to other new tables; those come
      // once every table exists.
      for (const t of drafts.filter((x) => !x.existing)) {
        const now = t.fields.filter((f: any) => !(f.type === 'link' && String(f.linkTable).startsWith('draft:')))
        const created = await api.createTable(app.id, t.name, now)
        made['draft:' + t.name.toLowerCase()] = created.id
        for (const f of t.fields) if (!now.includes(f)) later.push({ table: () => created.id, field: f })
      }
      for (const t of drafts.filter((x) => x.existing)) for (const f of t.fields) later.push({ table: () => t.id, field: f })
      for (const { table, field } of later) {
        const f = { ...field }
        if (f.type === 'link' && String(f.linkTable).startsWith('draft:')) f.linkTable = made[f.linkTable]
        await api.addField(app.id, table(), f)
      }
      const first = Object.values(made)[0] ?? drafts[0]?.id
      return first ? `${base}/data/${first}` : `${base}/data`
    }
    case 'screen': {
      const body = { ...d.draft, definition: fillDefaults(d.draft.definition) }
      const s = d.target ? await api.updateScreen(app.id, d.target, body) : await api.createScreen(app.id, body)
      return `${base}/design/${s.id}`
    }
    case 'query': {
      const q = d.target ? await api.updateQuery(app.id, d.target, d.draft) : await api.createQuery(app.id, d.draft)
      return `${base}/apis/${q.id}`
    }
    case 'flow': {
      const f = d.target ? await api.updateFlow(app.id, d.target, d.draft) : await api.createFlow(app.id, d.draft)
      return `${base}/flows/${f.id}`
    }
  }
}
