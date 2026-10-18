<script lang="ts">
  import { api, typeInfo, type Row, type Table } from '../lib/api'
  import { display, toDateInput, writable } from '../lib/format'
  import Modal from './Modal.svelte'
  import LinkPicker from './LinkPicker.svelte'

  let { app, table, tables, row, canWrite, onClose, onSaved }: {
    app: string; table: Table; tables: Table[]; row: Row | null; canWrite: boolean
    onClose: () => void; onSaved: (r: Row) => void
  } = $props()

  const values = $state<Record<string, any>>(
    Object.fromEntries(table.fields.map((f) => [f.name, row ? structuredClone($state.snapshot(row[f.name]) ?? null) : f.type === 'boolean' ? false : null])),
  )
  let error = $state('')
  let errorField = $state('')
  let busy = $state(false)

  function payload() {
    const out: Record<string, any> = {}
    for (const f of table.fields) {
      if (!writable(f)) continue
      let v = values[f.name]
      if (row && JSON.stringify(v) === JSON.stringify(row[f.name] ?? null)) continue
      if (f.type === 'link') v = Array.isArray(v) ? v.map((l) => l._id) : v ? v._id : null
      if (f.type === 'number' && v === '') v = null
      out[f.name] = v
    }
    return out
  }

  async function save(e: SubmitEvent) {
    e.preventDefault()
    error = errorField = ''
    busy = true
    try {
      onSaved(row ? await api.updateRow(app, table.id, row._id, payload()) : await api.createRow(app, table.id, payload()))
    } catch (err: any) {
      error = err.message
      errorField = err.body?.field ?? ''
    } finally {
      busy = false
    }
  }

  const targetOf = (id?: string) => tables.find((t) => t.id === id)
</script>

<Modal title={row ? `Row ${row._id}` : `New row in ${table.name}`} {onClose} wide>
  <form id="row-form" onsubmit={save}>
    {#if error}<p class="error">{error}</p>{/if}
    {#each table.fields as f (f.id)}
      <div class="field" class:bad={errorField === f.name}>
        <label for={`v-${f.id}`}><span class="icon">{typeInfo(f.type).icon}</span> {f.name}{f.required ? ' *' : ''}</label>
        {#if !writable(f) || !canWrite}
          <div class="readonly">{display(f, values[f.name]) || '—'}</div>
        {:else if f.type === 'longtext'}
          <textarea id={`v-${f.id}`} rows="3" bind:value={values[f.name]}></textarea>
        {:else if f.type === 'number'}
          <input id={`v-${f.id}`} type="number" step="any" bind:value={values[f.name]} />
        {:else if f.type === 'boolean'}
          <input id={`v-${f.id}`} type="checkbox" bind:checked={values[f.name]} />
        {:else if f.type === 'date'}
          <input id={`v-${f.id}`} type="date" value={toDateInput(values[f.name])} onchange={(e) => (values[f.name] = e.currentTarget.value || null)} />
        {:else if f.type === 'single_select'}
          <select id={`v-${f.id}`} bind:value={values[f.name]}>
            <option value={null}></option>
            {#each f.options ?? [] as o}<option value={o}>{o}</option>{/each}
          </select>
        {:else if f.type === 'multi_select'}
          <div class="opts">
            {#each f.options ?? [] as o}
              <label class="opt"><input type="checkbox" checked={(values[f.name] ?? []).includes(o)}
                onchange={(e) => { const cur = values[f.name] ?? []; values[f.name] = e.currentTarget.checked ? [...cur, o] : cur.filter((x: string) => x !== o) }} /> {o}</label>
            {/each}
          </div>
        {:else if f.type === 'link' && targetOf(f.linkTable)}
          <LinkPicker {app} target={targetOf(f.linkTable)!} many={!!f.linkMany} bind:value={values[f.name]} />
        {:else}
          <input id={`v-${f.id}`} bind:value={values[f.name]} />
        {/if}
      </div>
    {/each}
  </form>
  {#snippet footer()}
    <button onclick={onClose}>{canWrite ? 'Cancel' : 'Close'}</button>
    {#if canWrite}<button class="primary" form="row-form" disabled={busy}>{row ? 'Save' : 'Add row'}</button>{/if}
  {/snippet}
</Modal>

<style>
  .icon { display: inline-block; width: 18px; color: var(--accent); }
  .readonly { padding: 6px 0; }
  .bad input, .bad select, .bad textarea { border-color: var(--danger); }
  .opts { display: flex; flex-wrap: wrap; gap: 12px; }
  .opt { display: flex; gap: 6px; align-items: center; color: var(--text); margin: 0; }
</style>
