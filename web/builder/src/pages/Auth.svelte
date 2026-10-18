<script lang="ts">
  import { api } from '../lib/api'
  import { session } from '../lib/session.svelte'

  let { setup, onDone }: { setup: boolean; onDone: () => void } = $props()
  let email = $state('')
  let name = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    busy = true
    try {
      session.user = setup ? await api.createOwner(email, name, password) : await api.login(email, password)
      onDone()
    } catch (err: any) {
      error = err.message
    } finally {
      busy = false
    }
  }
</script>

<div class="wrap">
  <form class="card" onsubmit={submit}>
    <div class="logo">B</div>
    <h1>{setup ? 'Set up Bubibase' : 'Sign in'}</h1>
    <p class="muted">{setup ? 'Create the owner account for this workspace.' : 'Welcome back.'}</p>
    {#if error}<p class="error">{error}</p>{/if}
    {#if setup}
      <div class="field"><label for="name">Your name</label><input id="name" bind:value={name} autocomplete="name" /></div>
    {/if}
    <div class="field"><label for="email">Email</label><input id="email" type="email" bind:value={email} required autocomplete="email" /></div>
    <div class="field">
      <label for="pw">Password</label>
      <input id="pw" type="password" bind:value={password} required minlength={setup ? 10 : undefined}
        autocomplete={setup ? 'new-password' : 'current-password'} />
      {#if setup}<small class="muted">At least 10 characters.</small>{/if}
    </div>
    <button class="primary" disabled={busy}>{setup ? 'Create account' : 'Sign in'}</button>
  </form>
</div>

<style>
  .wrap { min-height: 100vh; display: grid; place-items: center; padding: 16px; }
  form { width: 100%; max-width: 380px; padding: 32px; }
  .logo {
    width: 40px; height: 40px; border-radius: 10px; background: var(--accent); color: white;
    display: grid; place-items: center; font-weight: 700; font-size: 20px; margin-bottom: 16px;
  }
  p { margin: 6px 0 20px; }
  button { width: 100%; justify-content: center; margin-top: 6px; }
</style>
