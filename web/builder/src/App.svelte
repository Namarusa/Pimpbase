<script lang="ts">
  import { api } from './lib/api'
  import { route, navigate, match } from './lib/router.svelte'
  import { session, isAdmin } from './lib/session.svelte'
  import Auth from './pages/Auth.svelte'
  import Apps from './pages/Apps.svelte'
  import Data from './pages/Data.svelte'
  import Users from './pages/Users.svelte'
  import AppSettings from './pages/AppSettings.svelte'

  let needsSetup = $state(false)

  async function load() {
    try {
      needsSetup = (await api.setup()).needsSetup
      if (!needsSetup) session.user = await api.me().catch(() => null)
    } finally {
      session.ready = true
    }
  }
  load()

  async function logout() {
    await api.logout()
    session.user = null
    navigate('/')
  }

  const dataRoute = $derived(match('/apps/:app/data/:table', route.path) ?? match('/apps/:app/data', route.path))
  const settingsRoute = $derived(match('/apps/:app/settings', route.path))
</script>

{#if !session.ready}
  <div class="center muted">Loading…</div>
{:else if !session.user}
  <Auth setup={needsSetup} onDone={() => (needsSetup = false)} />
{:else}
  <div class="shell">
    <header>
      <a class="brand" href="/" onclick={(e) => { e.preventDefault(); navigate('/') }}>
        <span class="logo">B</span> Bubibase
      </a>
      <nav>
        <a href="/" class:active={route.path === '/'} onclick={(e) => { e.preventDefault(); navigate('/') }}>Apps</a>
        {#if isAdmin()}
          <a href="/users" class:active={route.path === '/users'} onclick={(e) => { e.preventDefault(); navigate('/users') }}>Users</a>
        {/if}
      </nav>
      <div class="grow"></div>
      <span class="muted">{session.user.name}</span>
      <button class="ghost small" onclick={logout}>Sign out</button>
    </header>
    <main>
      {#if dataRoute}
        {#key dataRoute.app}
          <Data app={dataRoute.app} tableId={dataRoute.table} />
        {/key}
      {:else if settingsRoute}
        <AppSettings app={settingsRoute.app} />
      {:else if route.path === '/users'}
        <Users />
      {:else}
        <Apps />
      {/if}
    </main>
  </div>
{/if}

<style>
  .center { display: grid; place-items: center; height: 100vh; }
  .shell { display: flex; flex-direction: column; height: 100vh; }
  header {
    display: flex; align-items: center; gap: 16px; padding: 0 16px; height: 52px; flex: none;
    background: var(--panel); border-bottom: 1px solid var(--line);
  }
  .brand { display: flex; align-items: center; gap: 8px; color: var(--text); font-weight: 650; }
  .logo {
    width: 26px; height: 26px; border-radius: 7px; background: var(--accent); color: white;
    display: grid; place-items: center; font-size: 14px;
  }
  nav { display: flex; gap: 4px; }
  nav a { color: var(--muted); padding: 6px 10px; border-radius: var(--radius); }
  nav a.active, nav a:hover { color: var(--text); background: var(--hover); }
  main { flex: 1; min-height: 0; overflow: auto; }
</style>
