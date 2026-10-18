<script lang="ts">
  import { api, roles, type App, type User, type Role } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import AppNav from '../components/AppNav.svelte'

  let { app: slug }: { app: string } = $props()
  let app = $state<App | null>(null)
  let name = $state('')
  let urlName = $state('')
  let users = $state<User[]>([])
  let members = $state<Record<string, Role>>({})
  let error = $state('')
  let saved = $state('')

  async function load() {
    try {
      const res = await api.app(slug)
      app = res.app
      name = app.name
      urlName = app.slug
      users = (await api.users()).filter((u) => u.role === 'member')
      members = Object.fromEntries((await api.members(app.id)).map((m) => [m.userId, m.role]))
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

  async function setRole(userId: string, role: string) {
    error = ''
    try {
      await api.setMember(app!.id, userId, role as Role)
      members[userId] = role as Role
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
      <p class="muted">Builders and admins always have full access. Give members a role: Basic can use the app, Power and Admin get more where tables allow it.</p>
      {#if users.length === 0}<p class="muted">There are no members yet. Add them on the Users page.</p>{/if}
      {#each users as u (u.id)}
        <div class="row member">
          <div class="grow"><div>{u.name}</div><div class="muted">{u.email}</div></div>
          <select value={members[u.id] ?? 'NONE'} onchange={(e) => setRole(u.id, e.currentTarget.value)}>
            <option value="NONE">No access</option>
            {#each roles.filter((r) => r !== 'PUBLIC') as r}<option value={r}>{r[0] + r.slice(1).toLowerCase()}</option>{/each}
          </select>
        </div>
      {/each}
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
  .member { padding: 8px 0; border-top: 1px solid var(--line); }
  .member select { width: auto; }
</style>
