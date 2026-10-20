<script lang="ts">
  import { api, typeInfo, type Field, type Page, type Row, type Table } from '../lib/api'
  import { display, toDateInput, writable } from '../lib/format'
  import FormulaInput from './FormulaInput.svelte'
  import FieldEditor from './FieldEditor.svelte'
  import TableSettings from './TableSettings.svelte'
  import RowForm from './RowForm.svelte'
  import Modal from './Modal.svelte'

  let { app, table, tables, builder, onTable, onDeleted }: {
    app: string; table: Table; tables: Table[]; builder: boolean
    onTable: (t: Table) => void; onDeleted: () => void
  } = $props()

  const limit = 100
  let page = $state<Page | null>(null)
  let error = $state('')
  let filterDraft = $state('')
  let filter = $state('')
  let sort = $state('')
  let desc = $state(false)
  let offset = $state(0)
  let selected = $state<Set<number>>(new Set())
  let editing = $state<{ id: number; field: string } | null>(null)
  let editValue = $state<any>(null)
  let fieldModal = $state<Field | 'new' | null>(null)
  let settingsOpen = $state(false)
  let rowModal = $state<Row | 'new' | null>(null)
  let importOpen = $state(false)
  let importFile = $state<File | null>(null)
  let importAdd = $state(true)
  let importResult = $state<any>(null)

  async function load() {
    try {
      page = await api.rows(app, table.id, { filter, sort, desc, limit, offset })
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }

  $effect(() => {
    table.id
    filter; sort; desc; offset
    load()
  })

  function applyFilter() {
    filter = filterDraft
    offset = 0
  }

  function sortBy(name: string) {
    if (sort === name) {
      if (desc) { sort = ''; desc = false } else desc = true
    } else { sort = name; desc = false }
    offset = 0
  }

  function startEdit(row: Row, f: Field) {
    if (!writable(f)) return
    if (f.type === 'link' || f.type === 'multi_select' || f.type === 'longtext' || f.type === 'attachment') { rowModal = row; return }
    if (f.type === 'boolean') { saveCell(row, f, !row[f.name]); return }
    editing = { id: row._id, field: f.id }
    editValue = f.type === 'date' ? toDateInput(row[f.name]) : row[f.name] ?? ''
  }

  async function saveCell(row: Row, f: Field, value: any) {
    editing = null
    if (value === '' || value === undefined) value = null
    if ((row[f.name] ?? null) === value) return
    try {
      const updated = await api.updateRow(app, table.id, row._id, { [f.name]: value })
      const i = page!.rows.findIndex((r) => r._id === row._id)
      page!.rows[i] = updated
      error = ''
    } catch (e: any) {
      error = e.message
    }
  }

  function cellKey(e: KeyboardEvent, row: Row, f: Field) {
    if (e.key === 'Enter') saveCell(row, f, f.type === 'number' && editValue !== null && editValue !== '' ? Number(editValue) : editValue)
    if (e.key === 'Escape') editing = null
  }

  async function deleteSelected() {
    if (!confirm(`Delete ${selected.size} row(s)?`)) return
    try {
      await api.deleteRows(app, table.id, [...selected])
      selected = new Set()
      load()
    } catch (e: any) { error = e.message }
  }

  async function runImport(e: SubmitEvent) {
    e.preventDefault()
    if (!importFile) return
    try {
      importResult = await api.importCSV(app, table.id, importFile, importAdd && builder)
      if (importResult.newFields.length) onTable({ ...table, ...(await api.tables(app)).find((t) => t.id === table.id)! })
      load()
    } catch (err: any) { importResult = { error: err.message } }
  }

  function toggleAll(on: boolean) {
    selected = new Set(on ? page?.rows.map((r) => r._id) : [])
  }

  const canWrite = true // the server enforces table permissions on every write
</script>

<div class="grid-wrap">
  <div class="toolbar">
    <h2>{table.name}</h2>
    {#if builder}<button class="ghost small" onclick={() => (settingsOpen = true)} title="Table settings">⚙</button>{/if}
    <div class="filter">
      <FormulaInput bind:value={filterDraft} {app} table={table.id} placeholder={'Filter, e.g. [Status] = "Open"'} onSubmit={applyFilter} />
    </div>
    {#if filterDraft !== filter}<button class="small" onclick={applyFilter}>Apply</button>{/if}
    {#if filter}<button class="small ghost" onclick={() => { filterDraft = ''; applyFilter() }}>Clear</button>{/if}
    <div class="grow"></div>
    {#if selected.size}<button class="danger small" onclick={deleteSelected}>Delete {selected.size}</button>{/if}
    <button class="small" onclick={() => { importOpen = true; importResult = null; importFile = null }}>Import</button>
    <a class="btn small" href={api.exportURL(app, table.id, filter)}>Export</a>
    {#if builder}<button class="small" onclick={() => (fieldModal = 'new')}>+ Field</button>{/if}
    <button class="primary small" onclick={() => (rowModal = 'new')}>+ Row</button>
  </div>
  {#if error}<p class="error banner">{error}</p>{/if}

  <div class="scroll">
    <table>
      <thead>
        <tr>
          <th class="sel"><input type="checkbox" aria-label="Select all" checked={!!page?.rows.length && selected.size === page.rows.length} onchange={(e) => toggleAll(e.currentTarget.checked)} /></th>
          <th class="id"><button class="head" onclick={() => sortBy('')}>#</button></th>
          {#each table.fields as f (f.id)}
            <th>
              <div class="th">
                <button class="head grow" onclick={() => sortBy(f.name)} title={`Sort by ${f.name}`}>
                  <span class="type">{typeInfo(f.type).icon}</span>{f.name}
                  {#if sort === f.name}<span class="arrow">{desc ? '↓' : '↑'}</span>{/if}
                </button>
                {#if builder}<button class="ghost small edit" onclick={() => (fieldModal = f)} aria-label={`Edit ${f.name}`}>⋯</button>{/if}
              </div>
            </th>
          {/each}
          <th class="fill"></th>
        </tr>
      </thead>
      <tbody>
        {#each page?.rows ?? [] as row (row._id)}
          <tr class:picked={selected.has(row._id)}>
            <td class="sel"><input type="checkbox" aria-label={`Select row ${row._id}`} checked={selected.has(row._id)}
              onchange={(e) => { const s = new Set(selected); e.currentTarget.checked ? s.add(row._id) : s.delete(row._id); selected = s }} /></td>
            <td class="id"><button class="ghost small" onclick={() => (rowModal = row)} title="Open row">{row._id}</button></td>
            {#each table.fields as f (f.id)}
              {@const v = row[f.name]}
              <td class={`t-${f.type === 'formula' ? f.resultType : f.type}`} class:ro={!writable(f)} ondblclick={() => startEdit(row, f)}>
                {#if editing?.id === row._id && editing.field === f.id}
                  {#if f.type === 'single_select'}
                    <!-- svelte-ignore a11y_autofocus -->
                    <select autofocus bind:value={editValue} onchange={() => saveCell(row, f, editValue)} onblur={() => (editing = null)}>
                      <option value=""></option>
                      {#each f.options ?? [] as o}<option value={o}>{o}</option>{/each}
                    </select>
                  {:else}
                    <!-- svelte-ignore a11y_autofocus -->
                    <input autofocus type={f.type === 'number' ? 'number' : f.type === 'date' ? 'date' : 'text'} step="any"
                      bind:value={editValue} onkeydown={(e) => cellKey(e, row, f)}
                      onblur={() => saveCell(row, f, f.type === 'number' && editValue !== '' && editValue !== null ? Number(editValue) : editValue)} />
                  {/if}
                {:else if f.type === 'boolean' || (f.type === 'formula' && f.resultType === 'boolean')}
                  <button class="ghost check" disabled={!writable(f)} onclick={() => startEdit(row, f)} aria-label={v ? 'Yes' : 'No'}>{v ? '✓' : ''}</button>
                {:else if f.type === 'single_select' && v}
                  <span class="chip">{v}</span>
                {:else if f.type === 'attachment' && v}
                  {#each v as file (file.id)}<a class="chip file" href={file.url} target="_blank" rel="noopener" title={file.name}>{file.name}</a>{/each}
                {:else if (f.type === 'multi_select' || f.type === 'link') && v}
                  {#each Array.isArray(v) ? v : [v] as item}<span class="chip">{typeof item === 'object' ? item.label : item}</span>{/each}
                {:else}
                  {display(f, v)}
                {/if}
              </td>
            {/each}
            <td class="fill"></td>
          </tr>
        {/each}
      </tbody>
    </table>
    {#if page && page.rows.length === 0}
      <div class="empty muted">{filter ? 'No rows match this filter.' : 'No rows yet. Add one, or import a CSV file.'}</div>
    {/if}
  </div>

  <div class="footer muted">
    {#if page}
      <span>{page.total === 0 ? 0 : offset + 1}–{Math.min(offset + limit, page.total)} of {page.total}</span>
      <button class="small" disabled={offset === 0} onclick={() => (offset = Math.max(0, offset - limit))}>Previous</button>
      <button class="small" disabled={offset + limit >= page.total} onclick={() => (offset += limit)}>Next</button>
      {#if filter || sort}
        <span class="badge" title="Where the filter and sort ran">{page.engine === 'sql' ? 'filtered in the database' : 'filtered in Bubibase'}</span>
      {/if}
    {/if}
    <span class="grow"></span>
    <span>Double-click a cell to edit it.</span>
  </div>
</div>

{#if fieldModal}
  <FieldEditor {app} {table} {tables} field={fieldModal === 'new' ? null : fieldModal}
    onClose={() => (fieldModal = null)} onSaved={(t) => { fieldModal = null; onTable(t); load() }} />
{/if}
{#if settingsOpen}
  <TableSettings {app} {table} onClose={() => (settingsOpen = false)}
    onSaved={(t) => { settingsOpen = false; onTable(t); load() }} onDeleted={() => { settingsOpen = false; onDeleted() }} />
{/if}
{#if rowModal}
  <RowForm {app} {table} {tables} row={rowModal === 'new' ? null : rowModal} {canWrite}
    onClose={() => (rowModal = null)} onSaved={() => { rowModal = null; load() }} />
{/if}
{#if importOpen}
  <Modal title={`Import into ${table.name}`} onClose={() => (importOpen = false)}>
    {#if importResult?.error}<p class="error">{importResult.error}</p>{/if}
    {#if importResult && !importResult.error}
      <p><strong>{importResult.imported}</strong> row(s) imported{importResult.skipped ? `, ${importResult.skipped} skipped` : ''}.</p>
      {#if importResult.newFields.length}<p class="muted">New fields: {importResult.newFields.join(', ')}</p>{/if}
      {#if importResult.ignoredFields.length}<p class="muted">Ignored columns: {importResult.ignoredFields.join(', ')}</p>{/if}
      {#each importResult.errors as e}<div class="muted small">{e}</div>{/each}
    {:else}
      <form id="import-form" onsubmit={runImport}>
        <p class="muted">The first line of the CSV must name the fields. Values are matched to fields by name.</p>
        <div class="field"><input type="file" accept=".csv,text/csv" onchange={(e) => (importFile = e.currentTarget.files?.[0] ?? null)} /></div>
        {#if builder}<label class="check"><input type="checkbox" bind:checked={importAdd} /> Add text fields for unknown columns</label>{/if}
      </form>
    {/if}
    {#snippet footer()}
      {#if importResult && !importResult.error}
        <button class="primary" onclick={() => (importOpen = false)}>Done</button>
      {:else}
        <button onclick={() => (importOpen = false)}>Cancel</button>
        <button class="primary" form="import-form" disabled={!importFile}>Import</button>
      {/if}
    {/snippet}
  </Modal>
{/if}

<style>
  .grid-wrap { display: flex; flex-direction: column; height: 100%; min-width: 0; }
  .toolbar { display: flex; align-items: flex-start; gap: 8px; padding: 10px 16px; border-bottom: 1px solid var(--line); background: var(--panel); flex-wrap: wrap; }
  .toolbar h2 { line-height: 32px; }
  .toolbar > button, .toolbar > a { margin-top: 3px; }
  .filter { flex: 1 1 320px; max-width: 560px; display: flex; }
  .banner { margin: 8px 16px 0; }
  .scroll { flex: 1; overflow: auto; }
  table { border-collapse: separate; border-spacing: 0; min-width: 100%; }
  th { position: sticky; top: 0; background: var(--panel); z-index: 2; font-weight: 500; text-align: left; border-bottom: 1px solid var(--line-strong); }
  th, td { border-right: 1px solid var(--line); white-space: nowrap; }
  td { border-bottom: 1px solid var(--line); padding: 0 10px; height: 34px; max-width: 320px; overflow: hidden; text-overflow: ellipsis; background: var(--panel); }
  tr.picked td { background: var(--accent-soft); }
  td.ro { color: var(--muted-foreground); }
  td.t-number, td.t-autonumber { text-align: right; font-variant-numeric: tabular-nums; }
  td input, td select { border-radius: 0; padding: 4px 6px; margin: 0 -10px; width: calc(100% + 20px); }
  .th { display: flex; align-items: center; min-width: 140px; }
  .head { border: none; background: none; padding: 8px 10px; font-weight: 500; color: var(--text); border-radius: 0; }
  .type { color: var(--primary); width: 16px; display: inline-block; text-align: center; }
  .arrow { color: var(--primary); }
  .edit { opacity: 0; margin-right: 4px; }
  th:hover .edit { opacity: 1; }
  .sel { width: 36px; text-align: center; padding: 0; }
  .id { width: 56px; color: var(--muted-foreground); }
  .id button { color: var(--muted-foreground); }
  .fill { width: 100%; border-right: none; }
  .check { padding: 0 6px; color: var(--ok); font-weight: 700; }
  a.chip { text-decoration: none; }
  .chip { display: inline-block; background: var(--accent-soft); color: var(--primary); border-radius: 99px; padding: 1px 8px; font-size: 12px; margin-right: 4px; }
  .empty { padding: 40px; text-align: center; }
  .footer { display: flex; align-items: center; gap: 8px; padding: 8px 16px; border-top: 1px solid var(--line); background: var(--panel); font-size: 13px; }
  .check-label, .check { display: inline-flex; }
  label.check { display: flex; gap: 8px; align-items: center; color: var(--text); }
  .small { font-size: 12px; }
</style>
