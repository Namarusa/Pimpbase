<script lang="ts">
  import type { App } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import { isBuilder } from '../lib/session.svelte'

  let { app, active }: { app: App; active: 'data' | 'settings' } = $props()
  const go = (e: MouseEvent, path: string) => { e.preventDefault(); navigate(path) }
</script>

<div class="bar">
  <strong>{app.name}</strong>
  <nav>
    <a href={`/apps/${app.slug}/data`} class:active={active === 'data'} onclick={(e) => go(e, `/apps/${app.slug}/data`)}>Data</a>
    {#if isBuilder()}
      <a href={`/apps/${app.slug}/settings`} class:active={active === 'settings'} onclick={(e) => go(e, `/apps/${app.slug}/settings`)}>Settings</a>
    {/if}
  </nav>
</div>

<style>
  .bar { display: flex; align-items: center; gap: 20px; padding: 0 16px; height: 44px; border-bottom: 1px solid var(--line); background: var(--panel); }
  nav { display: flex; gap: 4px; }
  a { color: var(--muted); padding: 4px 10px; border-radius: var(--radius); }
  a.active, a:hover { color: var(--text); background: var(--hover); }
</style>
