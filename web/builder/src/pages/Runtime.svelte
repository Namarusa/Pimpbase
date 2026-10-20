<script lang="ts">
  import { onDestroy, untrack } from 'svelte'
  import { toast } from 'svelte-sonner'
  import { setMode } from 'mode-watcher'
  import { Button } from '$lib/components/ui/button'
  import * as Empty from '$lib/components/ui/empty'
  import * as Select from '$lib/components/ui/select'
  import CompassIcon from '@lucide/svelte/icons/compass'
  import EyeIcon from '@lucide/svelte/icons/eye'
  import { api, preview, type RuntimeInfo } from '../lib/api'
  import { navigate, route } from '../lib/router.svelte'
  import { session } from '../lib/session.svelte'
  import { Runtime } from '../lib/screen/context.svelte'
  import { matchRoute } from '../lib/screen/bindings'
  import { themeVars } from '../lib/screen/theme'
  import ScreenHost from '../lib/screen/ScreenHost.svelte'
  import ConfirmHost from '../lib/screen/ConfirmHost.svelte'
  import AppFrame from '../lib/screen/AppFrame.svelte'
  import AppGate from '../lib/screen/AppGate.svelte'
  import RoleDot from '../lib/access/RoleDot.svelte'
  import { roleColor, roleName } from '../lib/access/roles'

  // Runtime is the published app at /app/<slug>/<screen route>.
  let { slug }: { slug: string } = $props()
  let info = $state<RuntimeInfo | null>(null)
  let error = $state('')
  let needsLogin = $state(false)
  let denied = $state('')
  let confirmHost = $state<ConfirmHost>()
  // Builders may preview the app as one of its roles: ?as=ROLE.
  preview.role = new URLSearchParams(location.search).get('as') ?? ''

  async function load() {
    try {
      info = await api.runtime(slug)
      needsLogin = false
      denied = ''
      document.title = info.app.design.title || info.app.name
    } catch (e: any) {
      if (e.status === 401) needsLogin = true
      else if (e.status === 403) denied = e.message
      else error = e.message
    }
  }
  load()
  onDestroy(() => (preview.role = ''))

  function previewAs(role: string) {
    preview.role = role === 'BUILDER' ? '' : role
    const u = new URL(location.href)
    if (preview.role) u.searchParams.set('as', preview.role)
    else u.searchParams.delete('as')
    history.replaceState(null, '', u)
    info = null
    load()
  }

  const base = $derived(`/app/${slug}`)
  const path = $derived(route.path.slice(base.length) || '/')
  const matched = $derived.by(() => {
    if (!info) return null
    for (const s of info.screens) {
      const params = matchRoute(s.route, path)
      if (params) return { screen: s, params }
    }
    return null
  })

  // An app opens on its "/" screen, or its first screen without parameters.
  $effect(() => {
    if (!info || matched || path !== '/') return
    const home = info.screens.find((s) => !s.route.includes(':'))
    if (home) navigate(base + home.route)
  })

  // The app's theme and accent colour apply to the whole page, so dialogs
  // and menus follow them too.
  $effect(() => {
    if (!info) return
    const theme = info.app.design.theme
    untrack(() => setMode(theme === 'dark' ? 'dark' : theme === 'light' ? 'light' : 'system'))
    const vars = themeVars(info.app.design)
    for (const [k, v] of Object.entries(vars)) document.documentElement.style.setProperty(k, v)
    return () => { for (const k of Object.keys(vars)) document.documentElement.style.removeProperty(k) }
  })
  onDestroy(() => setMode('system'))

  const rt = $derived(info ? new Runtime({
    appId: info.app.id, appSlug: info.app.slug, tables: info.tables, queries: info.queries, flows: info.flows ?? [], editing: false,
    navigate: (to) => navigate(base + (to === '/' ? '' : to)),
    toast: (kind, message) => (kind === 'error' ? toast.error(message) : kind === 'info' ? toast.info(message) : toast.success(message)),
    confirm: (t, m) => confirmHost?.ask(t, m) ?? Promise.resolve(window.confirm(t)),
  }) : null)

  async function signOut() {
    await api.logout()
    session.user = null
    info = null
    denied = ''
    load()
  }
</script>

{#if needsLogin}
  <AppGate {slug} mode="signin" onSignedIn={(u) => { session.user = u; load() }} onSignOut={signOut} />
{:else if denied}
  <AppGate {slug} mode="denied" user={session.user} onSignedIn={(u) => { session.user = u; load() }} onSignOut={signOut} />
{:else if error}
  <div class="grid min-h-screen place-items-center p-6">
    <Empty.Root><Empty.Header><Empty.Title>This app is not available</Empty.Title><Empty.Description>{error}</Empty.Description></Empty.Header></Empty.Root>
  </div>
{:else if info && rt}
  <div class="h-screen print:h-auto">
    <AppFrame design={info.app.design} appName={info.app.name} screens={info.screens} current={matched?.screen.id} user={info.user}
      onNavigate={(r) => rt.navigate(r)} onSignOut={signOut} onSignIn={() => (needsLogin = true)}>
      {#if matched}
        {#key matched.screen.id + JSON.stringify(matched.params)}
          <ScreenHost {rt} root={matched.screen.definition} scope={() => ({
            user: info!.user, app: { id: info!.app.id, name: info!.app.name, slug: info!.app.slug },
            url: matched.params, query: Object.fromEntries(new URLSearchParams(location.search)), state: rt.state,
          })} />
        {/key}
      {:else if info.screens.length === 0}
        <div class="grid h-full place-items-center p-6">
          <Empty.Root>
            <Empty.Header><Empty.Media variant="icon"><CompassIcon /></Empty.Media><Empty.Title>No screens yet</Empty.Title>
              <Empty.Description>{info.builder ? 'Design the first screen of this app in the builder.' : 'This app has nothing to show you yet.'}</Empty.Description></Empty.Header>
            {#if info.builder}<Empty.Content><Button href={`/apps/${slug}/design`}>Open the designer</Button></Empty.Content>{/if}
          </Empty.Root>
        </div>
      {:else if path !== '/'}
        <div class="grid h-full place-items-center p-6">
          <Empty.Root>
            <Empty.Header><Empty.Media variant="icon"><CompassIcon /></Empty.Media><Empty.Title>Page not found</Empty.Title>
              <Empty.Description>No screen lives at {path}.</Empty.Description></Empty.Header>
            <Empty.Content><Button variant="outline" onclick={() => navigate(base)}>Go to the start</Button></Empty.Content>
          </Empty.Root>
        </div>
      {/if}
    </AppFrame>
  </div>
  <ConfirmHost bind:this={confirmHost} />
  {#if info.roles}
    <div class="bg-background/95 fixed bottom-4 left-1/2 z-50 flex -translate-x-1/2 items-center gap-2 rounded-full border py-1 pr-1 pl-3 text-sm shadow-lg backdrop-blur print:hidden">
      <EyeIcon class="text-muted-foreground size-4" />
      <span class="text-muted-foreground hidden sm:inline">Viewing as</span>
      <Select.Root type="single" value={preview.role || 'BUILDER'} onValueChange={previewAs}>
        <Select.Trigger size="sm" class="h-7 rounded-full border-0 bg-muted shadow-none">
          {#if preview.role}<RoleDot color={roleColor(info.roles, preview.role)} />{roleName(info.roles, preview.role)}{:else}Builder (everything){/if}
        </Select.Trigger>
        <Select.Content align="center">
          <Select.Item value="BUILDER" label="Builder">Builder (everything)</Select.Item>
          <Select.Separator />
          {#each info.roles as r (r.id)}<Select.Item value={r.id} label={r.name}><RoleDot color={r.color || '#64748b'} />{r.name}</Select.Item>{/each}
        </Select.Content>
      </Select.Root>
    </div>
  {/if}
{:else}
  <div class="text-muted-foreground grid min-h-screen place-items-center text-sm">Loading…</div>
{/if}

