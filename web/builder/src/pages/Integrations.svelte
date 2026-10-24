<script lang="ts">
  import { toast } from 'svelte-sonner'
  import * as Empty from '$lib/components/ui/empty'
  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Input } from '$lib/components/ui/input'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import SearchIcon from '@lucide/svelte/icons/search'
  import CableIcon from '@lucide/svelte/icons/cable'
  import BadgeCheckIcon from '@lucide/svelte/icons/badge-check'
  import { api, type App, type Manifest } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import AppNav from '../components/AppNav.svelte'
  import ConfirmHost from '../lib/screen/ConfirmHost.svelte'
  import IntegrationIcon from '../lib/integrations/IntegrationIcon.svelte'
  import ConnectionPage from '../lib/integrations/ConnectionPage.svelte'
  import { integrations, loadIntegrations, manifestOf } from '../lib/integrations/state.svelte'

  // Integrations lists the services an app is connected to, and the
  // catalog of services it can connect. A connection opens at
  // /apps/<app>/integrations/<id>; the catalog is the page without one.
  let { app: slug, item }: { app: string; item?: string } = $props()
  let app = $state<App | null>(null)
  let error = $state('')
  let ready = $state(false)
  let confirmHost = $state<ConfirmHost>()

  async function load() {
    try {
      const res = await api.app(slug)
      if (!res.builder) { error = 'Only builders can manage integrations.'; return }
      app = res.app
      await loadIntegrations(app.id)
      ready = true
    } catch (e: any) { error = e.message }
  }
  load()

  const go = (id?: string) => navigate(`/apps/${slug}/integrations${id ? '/' + id : ''}`)
  const current = $derived(integrations.connections.find((c) => c.id === item) ?? null)

  let query = $state('')
  const shown = $derived(integrations.catalog.filter((m) => !query.trim() || `${m.name} ${m.category} ${m.description}`.toLowerCase().includes(query.trim().toLowerCase())))
  const order = ['Payments', 'Messaging', 'Data', 'Developer tools', 'Storage']
  const rank = (c: string) => (order.includes(c) ? order.indexOf(c) : order.length)
  const categories = $derived([...new Set(shown.map((m) => m.category))].sort((a, b) => rank(a) - rank(b) || a.localeCompare(b)))
  const count = (m: Manifest) => integrations.connections.filter((c) => c.kind === m.id).length

  let connecting = $state('')
  async function connect(m: Manifest) {
    if (!app || connecting) return
    connecting = m.id
    try {
      const c = await api.createConnection(app.id, { kind: m.id })
      integrations.connections = [...integrations.connections, c]
      go(c.id)
    } catch (e: any) { toast.error(e.message) } finally { connecting = '' }
  }
</script>

<div class="flex h-full flex-col">
  {#if app}<AppNav {app} active="integrations" />{/if}
  {#if error}
    <p class="text-destructive p-6 text-sm">{error}</p>
  {:else if ready && app}
    <div class="flex min-h-0 flex-1">
      <aside class="bg-card flex w-64 shrink-0 flex-col border-r">
        <div class="flex items-center justify-between px-3 pt-3 pb-1">
          <span class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">Connections</span>
          <Button variant="ghost" size="icon" class="size-7" onclick={() => go()} aria-label="Connect a service"><PlusIcon /></Button>
        </div>
        <div class="flex flex-1 flex-col gap-0.5 overflow-y-auto px-2 pb-3">
          {#each integrations.connections as c (c.id)}
            {@const m = manifestOf(c.kind)}
            {@const ready = m ? !m.credentials.some((cr) => cr.required && !(cr.secret ? c.secrets[cr.key]?.live || c.secrets[cr.key]?.test : c.settings[cr.key] || c.testSettings[cr.key])) : false}
            <button type="button" onclick={() => go(c.id)}
              class={['flex items-center gap-2.5 rounded-md px-2 py-1.5 text-left', item === c.id ? 'bg-primary/10 text-primary' : 'hover:bg-muted']}>
              <IntegrationIcon manifest={m} size="sm" />
              <span class="flex min-w-0 flex-col">
                <span class="truncate text-sm font-medium">{c.name}</span>
                <span class={['truncate text-[11px]', ready ? 'text-muted-foreground' : 'text-amber-600 dark:text-amber-400']}>{ready ? (m?.category ?? c.kind) : 'Needs its keys'}</span>
              </span>
            </button>
          {/each}
          <button type="button" onclick={() => go()}
            class={['text-muted-foreground flex items-center gap-2.5 rounded-md px-2 py-1.5 text-left text-sm', !item ? 'bg-primary/10 text-primary' : 'hover:bg-muted']}>
            <span class="grid size-6 place-items-center rounded-md border border-dashed"><PlusIcon class="size-3.5" /></span>Connect a service
          </button>
        </div>
      </aside>

      {#if current}
        {#key current.id}<ConnectionPage {app} conn={current} confirm={confirmHost} onDeleted={() => go()} />{/key}
      {:else if item}
        <Empty.Root class="flex-1">
          <Empty.Header><Empty.Title>No such connection</Empty.Title><Empty.Description>It may have been deleted.</Empty.Description></Empty.Header>
          <Empty.Content><Button variant="outline" onclick={() => go()}>See the services</Button></Empty.Content>
        </Empty.Root>
      {:else}
        <section class="min-w-0 flex-1 overflow-y-auto">
          <div class="mx-auto flex max-w-5xl flex-col gap-6 p-6">
            <div class="flex items-end gap-4">
              <div class="flex-1">
                <h1 class="flex items-center gap-2 text-lg font-semibold"><CableIcon class="text-muted-foreground size-5" />Integrations</h1>
                <p class="text-muted-foreground text-sm">Connect a service once, then use its actions as flow steps, start flows from its events, and copy its data into tables.</p>
              </div>
              <div class="relative w-64">
                <SearchIcon class="text-muted-foreground absolute top-2.5 left-2.5 size-4" />
                <Input class="h-9 pl-8" placeholder="Find a service" bind:value={query} />
              </div>
            </div>
            {#each categories as cat (cat)}
              <div class="flex flex-col gap-2">
                <h2 class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">{cat}</h2>
                <div class="grid grid-cols-1 gap-3 md:grid-cols-2 xl:grid-cols-3">
                  {#each shown.filter((m) => m.category === cat) as m (m.id)}
                    <div class="bg-card flex flex-col gap-3 rounded-xl border p-4 shadow-xs">
                      <div class="flex items-start gap-3">
                        <IntegrationIcon manifest={m} size="lg" />
                        <div class="min-w-0 flex-1">
                          <div class="flex items-center gap-1.5 font-semibold">{m.name}{#if m.official}<BadgeCheckIcon class="text-primary size-4" aria-label="Official" />{/if}</div>
                          <div class="text-muted-foreground flex flex-wrap gap-1 pt-1">
                            <Badge variant="secondary" class="h-5 px-1.5 text-[10px]">{m.actions.length} actions</Badge>
                            {#if m.webhook}<Badge variant="secondary" class="h-5 px-1.5 text-[10px]">events</Badge>{/if}
                            {#if m.syncs?.length}<Badge variant="secondary" class="h-5 px-1.5 text-[10px]">sync to tables</Badge>{/if}
                          </div>
                        </div>
                      </div>
                      <p class="text-muted-foreground flex-1 text-sm">{m.description}</p>
                      <div class="flex items-center gap-2">
                        <Button size="sm" variant={count(m) ? 'outline' : 'default'} disabled={connecting === m.id} onclick={() => connect(m)}><PlusIcon />{count(m) ? 'Connect another' : 'Connect'}</Button>
                        {#if count(m)}<span class="text-muted-foreground text-xs">{count(m)} connected</span>{/if}
                      </div>
                    </div>
                  {/each}
                </div>
              </div>
            {/each}
            {#if !shown.length}<p class="text-muted-foreground text-sm">No service matches “{query}”.</p>{/if}
          </div>
        </section>
      {/if}
    </div>
  {:else}
    <p class="text-muted-foreground p-6 text-sm">Loading…</p>
  {/if}
</div>
<ConfirmHost bind:this={confirmHost} />
