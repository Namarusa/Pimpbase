<script lang="ts">
  import { api, type App, type ImportPlan, type Quotas, type Usage } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import { isAdmin } from '../lib/session.svelte'
  import AppNav from '../components/AppNav.svelte'
  import ImportApp from '../components/ImportApp.svelte'

  let { app: slug }: { app: string } = $props()
  let app = $state<App | null>(null)
  let name = $state('')
  let urlName = $state('')
  let error = $state('')
  let saved = $state('')

  let usage = $state<Usage | null>(null)
  let quotas = $state<Quotas>({ maxRows: 0, maxFileMB: 0, requestsPerMinute: 0, flowRunsPerHour: 0 })
  let quotaSaved = $state('')
  let withData = $state(false)
  let withSecrets = $state(false)
  let importing = $state(false)
  let updated = $state('')

  async function load() {
    try {
      const res = await api.app(slug)
      app = res.app
      name = app.name
      urlName = app.slug
      usage = await api.usage(app.id)
      quotas = { ...usage.quotas }
    } catch (e: any) { error = e.message }
  }

  async function saveQuotas(e: SubmitEvent) {
    e.preventDefault()
    error = quotaSaved = ''
    try {
      const clean = Object.fromEntries(Object.entries(quotas).map(([k, v]) => [k, Math.max(0, Math.floor(Number(v) || 0))])) as unknown as Quotas
      await api.setQuotas(app!.id, clean)
      usage = await api.usage(app!.id)
      quotas = { ...usage.quotas }
      quotaSaved = 'Saved.'
    } catch (err: any) { error = err.message }
  }

  function imported(plan: ImportPlan) {
    importing = false
    const n = plan.changes.filter((c) => c.action !== 'keep').length
    updated = `Applied ${n} change${n === 1 ? '' : 's'} from the file.`
    load()
  }

  const mb = (b: number) => (b / (1 << 20)).toLocaleString(undefined, { maximumFractionDigits: 1 })
  const of = (used: number, max: number, unit = '') => `${used.toLocaleString()}${unit}` + (max > 0 ? ` of ${max.toLocaleString()}${unit}` : '')
  const pct = (used: number, max: number) => (max > 0 ? Math.min(100, (used / max) * 100) : 0)
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
    {#if usage}
      <section class="card">
        <h2>Usage and limits</h2>
        <div class="usage">
          {#each [
            { label: 'Rows', used: usage.rows, max: usage.quotas.maxRows, text: of(usage.rows, usage.quotas.maxRows) },
            { label: 'Files', used: usage.fileBytes / (1 << 20), max: usage.quotas.maxFileMB, text: `${of(Number(mb(usage.fileBytes)), usage.quotas.maxFileMB, ' MB')} · ${usage.files} files` },
            { label: 'Flow runs, last hour', used: usage.flowRunsLastHour, max: usage.quotas.flowRunsPerHour, text: of(usage.flowRunsLastHour, usage.quotas.flowRunsPerHour) },
            { label: 'App requests, this minute', used: usage.requestsThisMinute, max: usage.quotas.requestsPerMinute, text: of(usage.requestsThisMinute, usage.quotas.requestsPerMinute) },
          ] as u}
            <div>
              <div class="row"><span class="grow">{u.label}</span><span class="mono">{u.text}</span></div>
              {#if u.max > 0}<div class="bar"><div style:width={`${pct(u.used, u.max)}%`} class:full={pct(u.used, u.max) >= 90}></div></div>{/if}
            </div>
          {/each}
        </div>
        {#if isAdmin()}
          <form onsubmit={saveQuotas} class="quotas">
            <p class="muted">Limits for this app; 0 means no limit. Requests count people using the app, not builders, on each server.</p>
            <div class="grid2">
              <div class="field"><label for="q-rows">Rows, all tables</label><input id="q-rows" type="number" min="0" bind:value={quotas.maxRows} /></div>
              <div class="field"><label for="q-files">Files, MB</label><input id="q-files" type="number" min="0" bind:value={quotas.maxFileMB} /></div>
              <div class="field"><label for="q-req">Requests a minute</label><input id="q-req" type="number" min="0" bind:value={quotas.requestsPerMinute} /></div>
              <div class="field"><label for="q-runs">Flow runs an hour</label><input id="q-runs" type="number" min="0" bind:value={quotas.flowRunsPerHour} /></div>
            </div>
            <div class="row"><button class="primary">Save limits</button><span class="muted">{quotaSaved}</span></div>
          </form>
        {:else}
          <p class="muted">Workspace owners and admins set this app's limits.</p>
        {/if}
      </section>
    {/if}
    <section class="card">
      <h2>Move this app</h2>
      <p class="muted">Download the app as one <span class="mono">.bubiapp</span> file to copy it to another Pimpbase, or to hand a new version from development to production.</p>
      <label class="check"><input type="checkbox" bind:checked={withData} /> Include rows and files</label>
      <label class="check"><input type="checkbox" bind:checked={withSecrets} disabled={!isAdmin()} /><span>Include secret values and data source passwords {#if !isAdmin()}<span class="muted">(admins only)</span>{/if}</span></label>
      <div class="row">
        <a class="btn" href={api.exportAppURL(app.id, withData, withSecrets)} download>Download {app.slug}.bubiapp</a>
        <button onclick={() => { importing = true; updated = '' }}>Update from a file…</button>
        <span class="muted">{updated}</span>
      </div>
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

{#if importing && app}
  <ImportApp target={app} onClose={() => (importing = false)} onDone={imported} />
{/if}

<style>
  .page { max-width: 720px; margin: 0 auto; padding: 24px 16px; display: grid; gap: 16px; }
  section { padding: 20px; }
  h2 { margin-bottom: 12px; }
  .usage { display: grid; gap: 10px; margin-bottom: 16px; }
  .bar { height: 6px; border-radius: 99px; background: var(--hover); margin-top: 4px; overflow: hidden; }
  .bar div { height: 100%; background: var(--primary); }
  .bar div.full { background: var(--danger); }
  .quotas { border-top: 1px solid var(--line); padding-top: 14px; }
  .quotas p { margin: 0 0 12px; }
  .grid2 { display: grid; grid-template-columns: repeat(auto-fill, minmax(150px, 1fr)); gap: 0 12px; }
  .check { display: flex; gap: 8px; align-items: center; color: var(--text); font-size: 14px; margin: 10px 0; }
  section > p.muted { margin: 0 0 4px; }
</style>
