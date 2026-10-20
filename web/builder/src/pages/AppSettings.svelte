<script lang="ts">
  import { api, type App } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import AppNav from '../components/AppNav.svelte'

  let { app: slug }: { app: string } = $props()
  let app = $state<App | null>(null)
  let name = $state('')
  let urlName = $state('')
  let error = $state('')
  let saved = $state('')

  async function load() {
    try {
      const res = await api.app(slug)
      app = res.app
      name = app.name
      urlName = app.slug
    } catch (e: any) { error = e.message }
  }
  load()

  async function save(e: SubmitEvent) {
    e.preventDefault()
    error = saved = ''
    try {
      const a = await api.updateApp(app!.id, { name, slug: urlName })
      saved = 'Saved.'
      if (a.slug !== slug) navigate(`/apps/${a.slug}/settings`)
      app = a
    } catch (err: any) { error = err.message }
  }


  async function remove() {
    if (!app || prompt(`This deletes ${app.name} and all its data. Type the app's URL name (${app.slug}) to confirm.`) !== app.slug) return
    await api.deleteApp(app.id)
    navigate('/')
  }
</script>

{#if app}
  <AppNav {app} active="settings" />
  <div class="page">
    {#if error}<p class="error">{error}</p>{/if}
    <section class="card">
      <h2>General</h2>
      <form onsubmit={save}>
        <div class="field"><label for="s-name">Name</label><input id="s-name" bind:value={name} required /></div>
        <div class="field"><label for="s-slug">URL name</label><input id="s-slug" class="mono" bind:value={urlName} pattern="[a-z0-9][a-z0-9-]*" required /></div>
        <div class="row"><button class="primary">Save</button><span class="muted">{saved}</span></div>
      </form>
    </section>
    <section class="card">
      <h2>Access</h2>
      <p class="muted">Members, roles and what each role may see and do are on the <a href={`/apps/${app.slug}/access`} onclick={(e) => { e.preventDefault(); navigate(`/apps/${app!.slug}/access`) }}>Access</a> tab.</p>
    </section>
    <section class="card danger-zone">
      <h2>Delete app</h2>
      <p class="muted">Deletes every table and row in this app. This cannot be undone.</p>
      <button class="danger" onclick={remove}>Delete {app.name}</button>
    </section>
  </div>
{:else if error}
  <p class="error">{error}</p>
{/if}

<style>
  .page { max-width: 720px; margin: 0 auto; padding: 24px 16px; display: grid; gap: 16px; }
  section { padding: 20px; }
  h2 { margin-bottom: 12px; }
</style>
