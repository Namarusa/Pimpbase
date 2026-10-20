<script lang="ts">
  import { onDestroy, untrack } from 'svelte'
  import { toast } from 'svelte-sonner'
  import { setMode } from 'mode-watcher'
  import { Button } from '$lib/components/ui/button'
  import * as Empty from '$lib/components/ui/empty'
  import CompassIcon from '@lucide/svelte/icons/compass'
  import { api, type RuntimeInfo } from '../lib/api'
  import { navigate, route } from '../lib/router.svelte'
  import { session } from '../lib/session.svelte'
  import { Runtime } from '../lib/screen/context.svelte'
  import { matchRoute } from '../lib/screen/bindings'
  import { themeVars } from '../lib/screen/theme'
  import ScreenHost from '../lib/screen/ScreenHost.svelte'
  import ConfirmHost from '../lib/screen/ConfirmHost.svelte'
  import AppFrame from '../lib/screen/AppFrame.svelte'
  import Auth from './Auth.svelte'

  // Runtime is the published app at /app/<slug>/<screen route>.
  let { slug }: { slug: string } = $props()
  let info = $state<RuntimeInfo | null>(null)
  let error = $state('')
  let needsLogin = $state(false)
  let confirmHost = $state<ConfirmHost>()

  async function load() {
    try {
      info = await api.runtime(slug)
      needsLogin = false
      document.title = info.app.design.title || info.app.name
    } catch (e: any) {
      if (e.status === 401) needsLogin = true
      else error = e.message
    }
  }
  load()

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
    load()
  }
</script>

{#if needsLogin}
  <Auth setup={false} title={slug} onDone={load} />
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
{:else}
  <div class="text-muted-foreground grid min-h-screen place-items-center text-sm">Loading…</div>
{/if}

