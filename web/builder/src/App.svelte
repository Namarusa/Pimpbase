<script lang="ts">
  import { ModeWatcher } from 'mode-watcher'
  import { Toaster } from '$lib/components/ui/sonner'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as Avatar from '$lib/components/ui/avatar'
  import LogOutIcon from '@lucide/svelte/icons/log-out'
  import { api } from './lib/api'
  import { route, navigate, match } from './lib/router.svelte'
  import { session, isAdmin } from './lib/session.svelte'
  import Auth from './pages/Auth.svelte'
  import Apps from './pages/Apps.svelte'
  import Data from './pages/Data.svelte'
  import Design from './pages/Design.svelte'
  import Apis from './pages/Apis.svelte'
  import Flows from './pages/Flows.svelte'
  import Users from './pages/Users.svelte'
  import AppSettings from './pages/AppSettings.svelte'
  import Access from './pages/Access.svelte'
  import Account from './pages/Account.svelte'
  import UserCogIcon from '@lucide/svelte/icons/user-cog'
  import Runtime from './pages/Runtime.svelte'

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

  // Published apps live under /app/<slug>; everything else is the builder.
  const runtimeSlug = $derived(route.path.match(/^\/app\/([^/]+)/)?.[1])
  const dataRoute = $derived(match('/apps/:app/data/:table', route.path) ?? match('/apps/:app/data', route.path))
  const designRoute = $derived(match('/apps/:app/design/:screen', route.path) ?? match('/apps/:app/design', route.path))
  const apisRoute = $derived(match('/apps/:app/apis/:item', route.path) ?? match('/apps/:app/apis', route.path))
  const flowsRoute = $derived(match('/apps/:app/flows/:item', route.path) ?? match('/apps/:app/flows', route.path))
  const settingsRoute = $derived(match('/apps/:app/settings', route.path))
  const accessRoute = $derived(match('/apps/:app/access/:tab', route.path) ?? match('/apps/:app/access', route.path))
  const initials = $derived((session.user?.name || session.user?.email || '?').split(/\s+/).slice(0, 2).map((w) => w[0]?.toUpperCase()).join(''))
  const go = (e: MouseEvent, p: string) => { e.preventDefault(); navigate(p) }
</script>

<ModeWatcher defaultMode="system" disableTransitions />
<Toaster richColors position="top-right" />

{#if runtimeSlug}
  {#key runtimeSlug}<Runtime slug={decodeURIComponent(runtimeSlug)} />{/key}
{:else if !session.ready}
  <div class="text-muted-foreground grid h-screen place-items-center text-sm">Loading…</div>
{:else if !session.user}
  <Auth setup={needsSetup} onDone={() => (needsSetup = false)} />
{:else}
  <div class="flex h-screen flex-col">
    <header class="bg-card flex h-13 shrink-0 items-center gap-4 border-b px-4">
      <a class="flex items-center gap-2 font-semibold" href="/" onclick={(e) => go(e, '/')}>
        <span class="bg-primary text-primary-foreground grid size-7 place-items-center rounded-md text-sm">B</span> Bubibase
      </a>
      <nav class="flex gap-1">
        <a href="/" onclick={(e) => go(e, '/')} class={['rounded-md px-2.5 py-1.5 text-sm font-medium', route.path === '/' ? 'bg-muted text-foreground' : 'text-muted-foreground hover:text-foreground']}>Apps</a>
        {#if isAdmin()}
          <a href="/users" onclick={(e) => go(e, '/users')} class={['rounded-md px-2.5 py-1.5 text-sm font-medium', route.path.startsWith('/users') ? 'bg-muted text-foreground' : 'text-muted-foreground hover:text-foreground']}>Users</a>
        {/if}
      </nav>
      <div class="ml-auto">
        <DropdownMenu.Root>
          <DropdownMenu.Trigger>
            {#snippet child({ props })}
              <button {...props} class="hover:bg-muted flex items-center gap-2 rounded-md py-1 pr-2 pl-1">
                <Avatar.Root class="size-7"><Avatar.Fallback class="bg-primary/10 text-primary text-xs font-medium">{initials}</Avatar.Fallback></Avatar.Root>
                <span class="text-sm">{session.user?.name || session.user?.email}</span>
              </button>
            {/snippet}
          </DropdownMenu.Trigger>
          <DropdownMenu.Content align="end" class="w-56">
            <DropdownMenu.Label class="font-normal"><div class="text-sm font-medium">{session.user.name}</div><div class="text-muted-foreground text-xs">{session.user.email}</div></DropdownMenu.Label>
            <DropdownMenu.Separator />
            <DropdownMenu.Item onclick={() => navigate('/account')}><UserCogIcon />Account and API keys</DropdownMenu.Item>
            <DropdownMenu.Item onclick={logout}><LogOutIcon />Sign out</DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu.Root>
      </div>
    </header>
    <main class="min-h-0 flex-1 overflow-auto">
      {#if designRoute}
        {#key designRoute.app}<Design app={designRoute.app} screenId={designRoute.screen} />{/key}
      {:else if apisRoute}
        {#key apisRoute.app}<Apis app={apisRoute.app} item={apisRoute.item} />{/key}
      {:else if flowsRoute}
        {#key flowsRoute.app}<Flows app={flowsRoute.app} item={flowsRoute.item} />{/key}
      {:else if dataRoute}
        <div class="studio h-full">{#key dataRoute.app}<Data app={dataRoute.app} tableId={dataRoute.table} />{/key}</div>
      {:else if accessRoute}
        {#key accessRoute.app}<Access app={accessRoute.app} tab={accessRoute.tab} />{/key}
      {:else if route.path === '/account'}
        <Account />
      {:else if settingsRoute}
        <div class="studio h-full"><AppSettings app={settingsRoute.app} /></div>
      {:else if route.path === '/users' || route.path === '/users/sso'}
        <Users />
      {:else}
        <div class="studio h-full"><Apps /></div>
      {/if}
    </main>
  </div>
{/if}
