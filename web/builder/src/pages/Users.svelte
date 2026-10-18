<script lang="ts">
  import { api, type User } from '../lib/api'
  import { session } from '../lib/session.svelte'
  import Modal from '../components/Modal.svelte'

  let users = $state<User[]>([])
  let error = $state('')
  let adding = $state(false)
  let form = $state({ email: '', name: '', password: '', role: 'member' })
  const roles = ['owner', 'admin', 'builder', 'member']

  async function load() {
    try { users = await api.users() } catch (e: any) { error = e.message }
  }
  load()

  async function add(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    try {
      await api.createUser(form)
      adding = false
      form = { email: '', name: '', password: '', role: 'member' }
      load()
    } catch (err: any) { error = err.message }
  }

  async function setRole(u: User, role: string) {
    error = ''
    try { await api.updateUser(u.id, { role }) } catch (err: any) { error = err.message }
    load()
  }

  async function remove(u: User) {
    if (!confirm(`Remove ${u.email}? They lose access to every app.`)) return
    try { await api.deleteUser(u.id); load() } catch (err: any) { error = err.message }
  }
</script>

<div class="page">
  <div class="row top">
    <h1 class="grow">Users</h1>
    <button class="primary" onclick={() => (adding = true)}>+ Add user</button>
  </div>
  <p class="muted intro">Owners and admins manage users. Builders design apps. Members use the apps they are given a role in.</p>
  {#if error}<p class="error">{error}</p>{/if}
  <div class="card">
    <table>
      <thead><tr><th>Name</th><th>Email</th><th>Workspace role</th><th></th></tr></thead>
      <tbody>
        {#each users as u (u.id)}
          <tr>
            <td>{u.name}</td>
            <td class="muted">{u.email}</td>
            <td>
              <select value={u.role} onchange={(e) => setRole(u, e.currentTarget.value)} disabled={u.id === session.user?.id}>
                {#each roles as r}<option value={r}>{r}</option>{/each}
              </select>
            </td>
            <td class="right">{#if u.id !== session.user?.id}<button class="ghost small danger" onclick={() => remove(u)}>Remove</button>{/if}</td>
          </tr>
        {/each}
      </tbody>
    </table>
  </div>
</div>

{#if adding}
  <Modal title="Add user" onClose={() => (adding = false)}>
    <form id="add-user" onsubmit={add}>
      <div class="field"><label for="u-email">Email</label><input id="u-email" type="email" bind:value={form.email} required /></div>
      <div class="field"><label for="u-name">Name</label><input id="u-name" bind:value={form.name} /></div>
      <div class="field"><label for="u-pw">Temporary password</label><input id="u-pw" type="text" bind:value={form.password} minlength="10" required /></div>
      <div class="field"><label for="u-role">Workspace role</label>
        <select id="u-role" bind:value={form.role}>{#each roles as r}<option value={r}>{r}</option>{/each}</select></div>
    </form>
    {#snippet footer()}
      <button onclick={() => (adding = false)}>Cancel</button>
      <button class="primary" form="add-user">Add user</button>
    {/snippet}
  </Modal>
{/if}

<style>
  .page { max-width: 900px; margin: 0 auto; padding: 32px 16px; }
  .top { margin-bottom: 8px; }
  .intro { margin: 0 0 20px; }
  table { width: 100%; border-collapse: collapse; }
  th { text-align: left; color: var(--muted); font-weight: 500; font-size: 13px; }
  th, td { padding: 10px 14px; border-bottom: 1px solid var(--line); }
  tr:last-child td { border-bottom: none; }
  td select { width: auto; }
  .right { text-align: right; }
</style>
