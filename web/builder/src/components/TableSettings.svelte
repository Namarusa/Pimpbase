<script lang="ts">
  import { api, roles, type Role, type Table } from '../lib/api'
  import Modal from './Modal.svelte'

  let { app, table, onClose, onSaved, onDeleted }: {
    app: string; table: Table; onClose: () => void; onSaved: (t: Table) => void; onDeleted: () => void
  } = $props()

  let name = $state(table.name)
  let primary = $state(table.primaryField ?? '')
  let read = $state<Role>(table.permissions.read)
  let write = $state<Role>(table.permissions.write)
  let error = $state('')
  const labelFields = table.fields.filter((f) => f.type !== 'link' && f.type !== 'multi_select')

  async function save(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    try {
      const p: Record<string, unknown> = { name, permissions: { read, write } }
      if (primary) p.primaryField = primary
      onSaved(await api.updateTable(app, table.id, p))
    } catch (err: any) { error = err.message }
  }

  async function remove() {
    if (!confirm(`Delete the table ${table.name} and all its rows? This cannot be undone.`)) return
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
    <div class="row">
      <div class="field grow"><label for="t-read">Who can read rows</label>
        <select id="t-read" bind:value={read}>{#each roles as r}<option value={r}>{r} and up</option>{/each}</select></div>
      <div class="field grow"><label for="t-write">Who can write rows</label>
        <select id="t-write" bind:value={write}>{#each roles as r}<option value={r}>{r} and up</option>{/each}</select></div>
    </div>
    <p class="muted small">PUBLIC means anyone, even without signing in.</p>
  </form>
  {#snippet footer()}
    <button class="danger ghost" onclick={remove}>Delete table</button>
    <div class="grow"></div>
    <button onclick={onClose}>Cancel</button>
    <button class="primary" form="table-form">Save</button>
  {/snippet}
</Modal>

<style>
  .small { font-size: 12px; margin: 0 0 8px; }
</style>
