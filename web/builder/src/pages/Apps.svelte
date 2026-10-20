<script lang="ts">
  import { api, type App } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import { isBuilder } from '../lib/session.svelte'
  import Modal from '../components/Modal.svelte'
  import ImportApp from '../components/ImportApp.svelte'

  let apps = $state<App[] | null>(null)
  let error = $state('')
  let creating = $state(false)
  let name = $state('')
  let importing = $state(false)

  async function load() {
    try { apps = await api.apps() } catch (e: any) { error = e.message }
  }
  load()

  async function create(e: SubmitEvent) {
    e.preventDefault()
    try {
      const a = await api.createApp(name)
      navigate(`/apps/${a.slug}/data`)
    } catch (err: any) { error = err.message }
  }
</script>

<div class="page">
  <div class="row top">
    <h1 class="grow">Apps</h1>
    {#if isBuilder()}
      <button onclick={() => (importing = true)}>Import app…</button>
      <button class="primary" onclick={() => { creating = true; name = '' }}>+ New app</button>
    {/if}
  </div>
  {#if error}<p class="error">{error}</p>{/if}
  {#if apps && apps.length === 0}
    <div class="empty card">
      <h2>No apps yet</h2>
      <p class="muted">{isBuilder() ? 'Create an app, then add tables to hold its data.' : 'Ask a builder to give you access to an app.'}</p>
    </div>
  {/if}
  <div class="grid">
    {#each apps ?? [] as a (a.id)}
      <a class="app card" href={`/apps/${a.slug}/data`} onclick={(e) => { e.preventDefault(); navigate(`/apps/${a.slug}/data`) }}>
        <div class="icon">{a.name.slice(0, 1).toUpperCase()}</div>
        <div><div class="name">{a.name}</div><div class="muted mono">/{a.slug}</div></div>
      </a>
    {/each}
  </div>
</div>

{#if creating}
  <Modal title="New app" onClose={() => (creating = false)}>
    <form id="new-app" onsubmit={create}>
      <div class="field"><label for="app-name">Name</label><input id="app-name" bind:value={name} required /></div>
    </form>
    {#snippet footer()}
      <button onclick={() => (creating = false)}>Cancel</button>
      <button class="primary" form="new-app">Create app</button>
    {/snippet}
  </Modal>
{/if}

{#if importing}
  <ImportApp onClose={() => (importing = false)} onDone={(plan) => navigate(`/apps/${plan.app.slug}/data`)} />
{/if}

<style>
  .page { max-width: 1000px; margin: 0 auto; padding: 32px 16px; }
  .top { margin-bottom: 24px; }
  .grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(240px, 1fr)); gap: 12px; }
  .app { display: flex; gap: 12px; align-items: center; padding: 16px; color: var(--text); }
  .app:hover { border-color: var(--primary); }
  .icon { width: 40px; height: 40px; border-radius: 10px; background: var(--accent-soft); color: var(--primary); display: grid; place-items: center; font-weight: 700; font-size: 18px; }
  .name { font-weight: 600; }
  .empty { padding: 40px; text-align: center; }
</style>
