<script lang="ts">
  import { api, type ExtLink, type Table } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import Modal from './Modal.svelte'

  let { app, table, tables = [], onClose, onSaved, onDeleted }: {
    app: string; table: Table; tables?: Table[]; onClose: () => void; onSaved: (t: Table) => void; onDeleted: () => void
  } = $props()

  let name = $state(table.name)
  let primary = $state(table.primaryField ?? '')
  let error = $state('')
  const labelFields = table.fields.filter((f) => f.type !== 'link' && f.type !== 'multi_select')
  // External tables choose which database columns apps see.
  const ext = table.external
  let hidden = $state<string[]>(ext?.columns.filter((c) => c.hidden).map((c) => c.name) ?? [])
  const toggle = (n: string, show: boolean) => (hidden = show ? hidden.filter((h) => h !== n) : [...hidden, n])
  // A column can link to a built-in table or to an external table with a
  // single-column key; found foreign keys come preset.
  const targets = tables.filter((t) => t.id !== table.id && (!t.external || t.external.primaryKey.length === 1))
  const linkable = ext?.columns.filter((c) => !c.binary) ?? []
  let links = $state<Record<string, ExtLink>>(Object.fromEntries((ext?.links ?? []).map((l) => [l.column, { ...l }])))
  const setTarget = (c: string, id: string) => {
    const { [c]: old, ...rest } = links
    links = id ? { ...rest, [c]: { column: c, table: id, reverse: old?.reverse ?? '', detected: old?.table === id && old?.detected } } : rest
  }

  async function save(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    try {
      const p: Record<string, unknown> = { name }
      if (primary) p.primaryField = primary
      if (ext) { p.hidden = hidden; p.links = Object.values(links) }
      onSaved(await api.updateTable(app, table.id, p))
    } catch (err: any) { error = err.message }
  }

  async function remove() {
    if (!confirm(ext ? `Remove ${table.name} from the app? The table and its rows stay in the database; screens that use it stop showing data.` : `Delete the table ${table.name} and all its rows? This cannot be undone.`)) return
    try { await api.deleteTable(app, table.id); onDeleted() } catch (err: any) { error = err.message }
  }
</script>

<Modal title="Table settings" {onClose}>
  <form id="table-form" onsubmit={save}>
    {#if error}<p class="error">{error}</p>{/if}
    <div class="field"><label for="t-name">Name</label><input id="t-name" bind:value={name} required /></div>
    <div class="field"><label for="t-primary">Row label</label>
      <select id="t-primary" bind:value={primary}>
        <option value="">Row number</option>
        {#each labelFields as f}<option value={f.id}>{f.name}</option>{/each}
      </select>
      <small class="muted">Shown when other tables link to a row.</small>
    </div>
    {#if ext}
      <div class="field"><span class="label">Columns apps see</span>
        <div class="cols">
          {#each ext.columns as c (c.name)}
            <label class="check" class:off={c.binary}>
              <input type="checkbox" checked={!hidden.includes(c.name)} disabled={c.binary || ext.primaryKey.includes(c.name)} onchange={(e) => toggle(c.name, e.currentTarget.checked)} />
              <span>{c.name}</span><small class="muted">{c.dbType}{c.binary ? ' · binary, not shown' : ext.primaryKey.includes(c.name) ? ' · key' : ''}</small>
            </label>
          {/each}
        </div>
      </div>
    {/if}
    {#if ext && linkable.length && targets.length}
      <div class="field"><span class="label">Links</span>
        <div class="links">
          {#each linkable as c (c.name)}
            {@const l = links[c.name]}
            <div class="link">
              <span class="col">{c.name}</span>
              <select aria-label={`${c.name} links to`} value={l?.table ?? ''} onchange={(e) => setTarget(c.name, e.currentTarget.value)}>
                <option value="">Not a link</option>
                {#each targets as t (t.id)}<option value={t.id}>{t.name}{t.external ? '' : ' (built-in)'}</option>{/each}
              </select>
              {#if l}
                <input aria-label={`Field on ${c.name}'s target`} placeholder="Field on the other table" bind:value={links[c.name].reverse} disabled={!tables.find((t) => t.id === l.table)?.external} />
              {/if}
              {#if c.references}<small class="muted hint">foreign key to {c.references.table}</small>{/if}
            </div>
          {/each}
        </div>
        <small class="muted">A link column shows the linked row's label. The field name adds a list of linking rows to the other table (external tables only).</small>
      </div>
    {/if}
    <p class="muted small">Who may read, change and delete rows, and which rows, is set on the
      <a href={`/apps/${app}/access/permissions`} onclick={(e) => { e.preventDefault(); onClose(); navigate(`/apps/${app}/access/permissions`) }}>Access</a> tab.</p>
  </form>
  {#snippet footer()}
    <button class="danger ghost" onclick={remove}>{ext ? 'Remove from app' : 'Delete table'}</button>
    <div class="grow"></div>
    <button onclick={onClose}>Cancel</button>
    <button class="primary" form="table-form">Save</button>
  {/snippet}
</Modal>

<style>
  .small { font-size: 12px; margin: 0 0 8px; }
  .label { display: block; font-size: 13px; font-weight: 550; margin-bottom: 6px; }
  .cols { display: flex; flex-direction: column; gap: 4px; max-height: 220px; overflow: auto; border: 1px solid var(--line); border-radius: var(--radius); padding: 6px 10px; }
  .check { display: flex; gap: 8px; align-items: center; font-size: 13px; }
  .check small { margin-left: auto; font-size: 11px; }
  .off { opacity: 0.6; }
  .links { display: flex; flex-direction: column; gap: 6px; max-height: 260px; overflow: auto; border: 1px solid var(--line); border-radius: var(--radius); padding: 8px 10px; }
  .link { display: grid; grid-template-columns: minmax(80px, 1fr) minmax(120px, 1.3fr) minmax(120px, 1.3fr); gap: 6px; align-items: center; font-size: 13px; }
  .link .col { font-weight: 500; overflow: hidden; text-overflow: ellipsis; }
  .link .hint { grid-column: 2 / -1; font-size: 11px; margin-top: -4px; }
</style>
