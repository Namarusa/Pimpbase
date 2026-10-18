<script lang="ts">
  import { api, type App, type Table } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import AppNav from '../components/AppNav.svelte'
  import Grid from '../components/Grid.svelte'
  import Modal from '../components/Modal.svelte'

  let { app: slug, tableId }: { app: string; tableId?: string } = $props()

  let app = $state<App | null>(null)
  let builder = $state(false)
  let tables = $state<Table[]>([])
  let error = $state('')
  let creating = $state(false)
  let newName = $state('')
  let starter = $state(true)

  async function load() {
    try {
      const res = await api.app(slug)
      app = res.app
      builder = res.builder
      tables = await api.tables(app.id)
      if (!tableId && tables.length) navigate(`/apps/${slug}/data/${tables[0].id}`)
    } catch (e: any) { error = e.message }
  }
  load()

  const current = $derived(tables.find((t) => t.id === tableId))

  async function create(e: SubmitEvent) {
    e.preventDefault()
    try {
      const fields = starter ? [{ name: 'Name', type: 'text' as const }, { name: 'Notes', type: 'longtext' as const }] : []
      const t = await api.createTable(app!.id, newName, fields)
      tables = [...tables, t].sort((a, b) => a.name.localeCompare(b.name))
      creating = false
      navigate(`/apps/${slug}/data/${t.id}`)
    } catch (err: any) { error = err.message }
  }

  function replace(t: Table) {
    tables = tables.map((x) => (x.id === t.id ? t : x))
  }
</script>

{#if app}
  <AppNav {app} active="data" />
  <div class="layout">
    <aside>
      <div class="side-head"><span class="muted">Tables</span>
        {#if builder}<button class="ghost small" onclick={() => { creating = true; newName = '' }} title="New table">+</button>{/if}
      </div>
      {#each tables as t (t.id)}
        <a href={`/apps/${slug}/data/${t.id}`} class:active={t.id === tableId}
          onclick={(e) => { e.preventDefault(); navigate(`/apps/${slug}/data/${t.id}`) }}>{t.name}</a>
      {/each}
      {#if tables.length === 0}<p class="muted hint">{builder ? 'Create a table to start.' : 'No tables you can see.'}</p>{/if}
    </aside>
    <section>
      {#if error}<p class="error">{error}</p>{/if}
      {#if current}
        {#key current.id}
          <Grid app={app.id} table={current} {tables} {builder} onTable={replace}
            onDeleted={() => { tables = tables.filter((t) => t.id !== current?.id); navigate(`/apps/${slug}/data`) }} />
        {/key}
      {/if}
    </section>
  </div>
{:else if error}
  <p class="error">{error}</p>
{/if}

{#if creating}
  <Modal title="New table" onClose={() => (creating = false)}>
    <form id="new-table" onsubmit={create}>
      <div class="field"><label for="t-new">Name</label><input id="t-new" bind:value={newName} required /></div>
      <label class="check"><input type="checkbox" bind:checked={starter} /> Start with Name and Notes fields</label>
    </form>
    {#snippet footer()}
      <button onclick={() => (creating = false)}>Cancel</button>
      <button class="primary" form="new-table">Create table</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .layout { display: flex; height: calc(100% - 44px); }
  aside { width: 220px; flex: none; border-right: 1px solid var(--line); padding: 12px 8px; overflow: auto; background: var(--panel); }
  .side-head { display: flex; justify-content: space-between; align-items: center; padding: 0 8px 8px; font-size: 12px; text-transform: uppercase; letter-spacing: 0.04em; }
  aside a { display: block; padding: 6px 10px; border-radius: var(--radius); color: var(--text); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  aside a:hover { background: var(--hover); }
  aside a.active { background: var(--accent-soft); color: var(--accent); font-weight: 550; }
  .hint { padding: 0 10px; font-size: 13px; }
  section { flex: 1; min-width: 0; }
  .check { display: flex; gap: 8px; align-items: center; color: var(--text); }
  @media (max-width: 700px) { aside { width: 140px; } }
</style>
