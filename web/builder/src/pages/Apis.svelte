<script lang="ts">
  import { onDestroy } from 'svelte'
  import { toast } from 'svelte-sonner'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as Empty from '$lib/components/ui/empty'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Separator } from '$lib/components/ui/separator'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import GlobeIcon from '@lucide/svelte/icons/globe'
  import KeyRoundIcon from '@lucide/svelte/icons/key-round'
  import EllipsisIcon from '@lucide/svelte/icons/ellipsis'
  import CheckIcon from '@lucide/svelte/icons/check'
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert'
  import PlugIcon from '@lucide/svelte/icons/plug'
  import DatabaseIcon from '@lucide/svelte/icons/database'
  import { api, type App, type Connector, type Datasource, type DbKind, type Query, type Variable } from '../lib/api'
  import DbSourceEditor from '../lib/apis/DbSourceEditor.svelte'
  import { sourceKinds } from '../lib/apis/kinds'
  import { navigate } from '../lib/router.svelte'
  import AppNav from '../components/AppNav.svelte'
  import ConfirmHost from '../lib/screen/ConfirmHost.svelte'
  import QueryEditor from '../lib/apis/QueryEditor.svelte'
  import DatasourceEditor from '../lib/apis/DatasourceEditor.svelte'
  import VariablesEditor from '../lib/apis/VariablesEditor.svelte'

  // Apis is the API studio: data sources, their queries and the app's
  // variables. Items open at /apps/<app>/apis/<id>.
  let { app: slug, item }: { app: string; item?: string } = $props()
  let app = $state<App | null>(null)
  let datasources = $state<Datasource[]>([])
  let queries = $state<Query[]>([])
  let variables = $state<Variable[]>([])
  // connectors are the database kinds this Pimpbase was built with.
  let connectors = $state<Connector[]>([])
  let error = $state('')
  let ready = $state(false)
  let confirmHost = $state<ConfirmHost>()
  let saving = $state<'saved' | 'saving' | 'error'>('saved')
  let saveError = $state('')

  async function load() {
    try {
      const res = await api.app(slug)
      if (!res.builder) { error = 'Only builders can manage APIs.'; return }
      app = res.app
      ;[datasources, queries, variables, connectors] = await Promise.all([api.datasources(app.id), api.queries(app.id), api.variables(app.id), api.connectors()])
      ready = true
    } catch (e: any) { error = e.message }
  }
  load()

  const go = (id?: string) => navigate(`/apps/${slug}/apis${id ? '/' + id : ''}`)
  const query = $derived(queries.find((q) => q.id === item))
  const ds = $derived(datasources.find((d) => d.id === item))

  // Open the first query when nothing is chosen.
  $effect(() => {
    if (ready && !item && (queries[0] || datasources[0])) go((queries[0] ?? datasources[0]).id)
  })

  // Edits apply at once and save shortly after the last one.
  let timer: ReturnType<typeof setTimeout> | undefined
  let pending: (() => Promise<void>) | null = null
  function schedule(fn: () => Promise<void>) {
    pending = fn
    saving = 'saving'
    clearTimeout(timer)
    timer = setTimeout(flush, 600)
  }
  async function flush() {
    clearTimeout(timer)
    const fn = pending
    pending = null
    if (!fn) return
    try {
      await fn()
      if (!pending) saving = 'saved'
    } catch (e: any) {
      saving = 'error'
      saveError = e.message
    }
  }
  onDestroy(flush)

  function changeQuery(id: string, p: Partial<Query>) {
    queries = queries.map((q) => (q.id === id ? { ...q, ...p } : q))
    schedule(async () => {
      const q = queries.find((x) => x.id === id)
      if (!q || !app) return
      const { id: _, appId, createdAt, updatedAt, ...body } = $state.snapshot(q) as Query
      await api.updateQuery(app.id, id, body)
    })
  }
  function changeDatasource(id: string, p: Partial<Datasource>) {
    datasources = datasources.map((d) => (d.id === id ? { ...d, ...p } : d))
    schedule(async () => {
      const d = datasources.find((x) => x.id === id)
      if (!d || !app) return
      await api.updateDatasource(app.id, id, { name: d.name, config: $state.snapshot(d.config) as Datasource['config'] })
    })
  }

  async function newQuery(datasourceId: string) {
    if (!app) return
    await flush()
    let name = 'New query'
    for (let i = 2; queries.some((q) => q.name.toLowerCase() === name.toLowerCase()); i++) name = `New query ${i}`
    const kind = datasources.find((d) => d.id === datasourceId)?.kind
    try {
      const q = await api.createQuery(app.id, kind === 'mongodb' ? { name, datasourceId, operation: 'find', filter: '{}' }
        : kind && kind !== 'rest' ? { name, datasourceId, sql: 'SELECT 1' } : { name, datasourceId, method: 'GET', path: '' })
      queries = [...queries, q].sort((a, b) => a.name.localeCompare(b.name))
      go(q.id)
    } catch (e: any) { toast.error(e.message) }
  }
  async function duplicateQuery(q: Query) {
    if (!app) return
    await flush()
    let name = `${q.name} copy`
    for (let i = 2; queries.some((x) => x.name.toLowerCase() === name.toLowerCase()); i++) name = `${q.name} copy ${i}`
    const { id, appId, createdAt, updatedAt, ...body } = $state.snapshot(q) as Query
    try {
      const copy = await api.createQuery(app.id, { ...body, name })
      queries = [...queries, copy].sort((a, b) => a.name.localeCompare(b.name))
      go(copy.id)
    } catch (e: any) { toast.error(e.message) }
  }
  async function deleteQuery(q: Query) {
    if (!app || !(await confirmHost?.ask(`Delete the query “${q.name}”?`, 'Screens and queries that use it will stop getting data.'))) return
    try {
      await api.deleteQuery(app.id, q.id)
      queries = queries.filter((x) => x.id !== q.id)
      if (item === q.id) go()
    } catch (e: any) { toast.error(e.message) }
  }
  async function deleteDatasource(d: Datasource) {
    const n = queries.filter((q) => q.datasourceId === d.id).length
    if (!app || !(await confirmHost?.ask(`Delete the data source “${d.name}”?`, n ? `Its ${n} ${n === 1 ? 'query is' : 'queries are'} deleted too.` : 'It has no queries.'))) return
    try {
      await api.deleteDatasource(app.id, d.id)
      datasources = datasources.filter((x) => x.id !== d.id)
      queries = queries.filter((q) => q.datasourceId !== d.id)
      go()
    } catch (e: any) { toast.error(e.message) }
  }

  let adding = $state(false)
  let newName = $state('')
  let newUrl = $state('')
  let newKind = $state<'rest' | DbKind>('rest')
  let addError = $state('')
  const offered = $derived(sourceKinds.filter((k) => k.kind === 'rest' || connectors.some((c) => c.kind === k.kind)))
  function startAdding(kind: 'rest' | DbKind = 'rest') { adding = true; newName = ''; newUrl = ''; newKind = kind; addError = '' }
  async function createDatasource(e: SubmitEvent) {
    e.preventDefault()
    if (!app) return
    addError = ''
    const db = newKind !== 'rest'
    try {
      const d = await api.createDatasource(app.id, db
        ? { name: newName, kind: newKind, config: { baseUrl: '', headers: [], auth: { type: 'none' }, timeoutSeconds: 30, db: { host: '', port: '', database: '', user: '', password: '', options: [], uri: newUrl.trim() } } }
        : { name: newName, config: { baseUrl: newUrl.trim(), headers: [], auth: { type: 'none' }, timeoutSeconds: 30 } })
      datasources = [...datasources, d].sort((a, b) => a.name.localeCompare(b.name))
      adding = false
      go(d.id)
    } catch (err: any) { addError = err.message }
  }
  const methodColor: Record<string, string> = { GET: 'text-emerald-600 dark:text-emerald-400', POST: 'text-amber-600 dark:text-amber-400', PUT: 'text-sky-600 dark:text-sky-400', PATCH: 'text-violet-600 dark:text-violet-400', DELETE: 'text-red-600 dark:text-red-400' }
</script>

<div class="flex h-full flex-col">
  {#if app}<AppNav {app} active="apis" />{/if}
  {#if error}
    <p class="text-destructive p-6 text-sm">{error}</p>
  {:else if ready && app}
    <div class="flex min-h-0 flex-1">
      <aside class="bg-card flex w-68 shrink-0 flex-col border-r">
        <div class="flex items-center justify-between px-3 pt-3 pb-1">
          <span class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">Data sources</span>
          <Button variant="ghost" size="icon" class="size-7" onclick={() => startAdding()} aria-label="New data source"><PlusIcon /></Button>
        </div>
        <div class="flex flex-1 flex-col gap-2 overflow-y-auto px-2 pb-3">
          {#each datasources as d (d.id)}
            <div>
              <div class={['group flex items-center rounded-md', item === d.id ? 'bg-primary/10 text-primary' : 'hover:bg-muted']}>
                <button type="button" class="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left" onclick={() => go(d.id)}>
                  {#if d.kind === 'rest'}<GlobeIcon class="size-3.5 shrink-0 opacity-70" />{:else}<DatabaseIcon class="size-3.5 shrink-0 opacity-70" />{/if}<span class="truncate text-sm font-medium">{d.name}</span>
                </button>
                <Button variant="ghost" size="icon" class="mr-1 size-6 opacity-0 group-hover:opacity-100" onclick={() => newQuery(d.id)} aria-label={`New query in ${d.name}`} title="New query"><PlusIcon class="size-3.5" /></Button>
              </div>
              <div class="ml-3 flex flex-col gap-0.5 border-l pl-1.5">
                {#each queries.filter((q) => q.datasourceId === d.id) as q (q.id)}
                  <div class={['group flex items-center rounded-md', item === q.id ? 'bg-primary/10 text-primary' : 'hover:bg-muted']}>
                    <button type="button" class="flex min-w-0 flex-1 items-center gap-2 px-2 py-1 text-left" onclick={() => go(q.id)}>
                      {#if d.kind === 'rest'}<span class={['w-11 shrink-0 font-mono text-[10px] font-semibold', methodColor[q.method]]}>{q.method}</span>
                      {:else}<span class="w-11 shrink-0 font-mono text-[10px] font-semibold text-sky-600 dark:text-sky-400">{d.kind === 'mongodb' ? 'MONGO' : 'SQL'}</span>{/if}
                      <span class="truncate text-sm">{q.name}</span>
                    </button>
                    <DropdownMenu.Root>
                      <DropdownMenu.Trigger>
                        {#snippet child({ props })}<Button {...props} variant="ghost" size="icon" class="mr-1 size-6 opacity-0 group-hover:opacity-100 data-[state=open]:opacity-100" aria-label="Query actions"><EllipsisIcon /></Button>{/snippet}
                      </DropdownMenu.Trigger>
                      <DropdownMenu.Content align="start">
                        <DropdownMenu.Item onclick={() => duplicateQuery(q)}>Duplicate</DropdownMenu.Item>
                        <DropdownMenu.Separator />
                        <DropdownMenu.Item variant="destructive" onclick={() => deleteQuery(q)}>Delete</DropdownMenu.Item>
                      </DropdownMenu.Content>
                    </DropdownMenu.Root>
                  </div>
                {/each}
              </div>
            </div>
          {/each}
        </div>
        <Separator />
        <div class="p-2">
          <button type="button" class={['flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left text-sm font-medium', item === 'variables' ? 'bg-primary/10 text-primary' : 'hover:bg-muted']} onclick={() => go('variables')}>
            <KeyRoundIcon class="size-3.5 opacity-70" />Variables<span class="text-muted-foreground ml-auto text-xs">{variables.length}</span>
          </button>
        </div>
        <div class="text-muted-foreground flex h-8 items-center gap-1 border-t px-3 text-xs">
          {#if saving === 'saving'}Saving…{:else if saving === 'error'}<CircleAlertIcon class="text-destructive size-3.5" /><span class="text-destructive truncate" title={saveError}>Not saved: {saveError}</span>{:else}<CheckIcon class="size-3.5" />Saved{/if}
        </div>
      </aside>

      <section class="min-w-0 flex-1 overflow-y-auto">
        {#if query}
          {#key query.id}
            <QueryEditor appId={app.id} {query} {datasources} {queries} {variables} onChange={(p) => changeQuery(query.id, p)} />
          {/key}
        {:else if ds && ds.kind !== 'rest'}
          {#key ds.id}
            <DbSourceEditor appId={app.id} {slug} datasource={ds} {variables} {queries} onChange={(p) => changeDatasource(ds.id, p)} onDelete={() => deleteDatasource(ds)} onNewQuery={() => newQuery(ds.id)} onOpen={(q) => go(q.id)} />
          {/key}
        {:else if ds}
          <DatasourceEditor datasource={ds} {variables} {queries} onChange={(p) => changeDatasource(ds.id, p)} onDelete={() => deleteDatasource(ds)} onNewQuery={() => newQuery(ds.id)} onOpen={(q) => go(q.id)} />
        {:else if item === 'variables'}
          <VariablesEditor appId={app.id} bind:variables />
        {:else}
          <Empty.Root class="h-full">
            <Empty.Header>
              <Empty.Media variant="icon"><PlugIcon /></Empty.Media>
              <Empty.Title>Connect an API or a database</Empty.Title>
              <Empty.Description>Add a REST API or one of your databases as a data source, then write queries against it or import its tables. Their data feeds tables, lists and charts on your screens.</Empty.Description>
            </Empty.Header>
            <Empty.Content><Button onclick={() => startAdding()}><PlusIcon />New data source</Button></Empty.Content>
          </Empty.Root>
        {/if}
      </section>
    </div>
  {:else}
    <p class="text-muted-foreground p-6 text-sm">Loading…</p>
  {/if}
</div>

<Dialog.Root bind:open={adding}>
  <Dialog.Content class="sm:max-w-md">
    <Dialog.Header><Dialog.Title>New data source</Dialog.Title><Dialog.Description>A REST API your queries call, or a database you already run. You can change everything later.</Dialog.Description></Dialog.Header>
    <form id="new-ds" class="flex flex-col gap-4" onsubmit={createDatasource}>
      <div class="grid grid-cols-2 gap-2" role="radiogroup" aria-label="Kind">
        {#each offered as k (k.kind)}
          <button type="button" role="radio" aria-checked={newKind === k.kind} onclick={() => (newKind = k.kind)}
            class={['flex items-start gap-2.5 rounded-lg border p-2.5 text-left transition-colors', newKind === k.kind ? 'border-primary bg-primary/5 ring-primary/30 ring-2' : 'hover:bg-muted']}>
            {#if k.kind === 'rest'}<GlobeIcon class="mt-0.5 size-4 shrink-0 opacity-70" />{:else}<DatabaseIcon class="mt-0.5 size-4 shrink-0 opacity-70" />{/if}
            <span class="min-w-0"><span class="block text-sm font-medium">{k.label}</span><span class="text-muted-foreground block text-[11px] leading-tight">{k.hint}</span></span>
          </button>
        {/each}
      </div>
      <div class="flex flex-col gap-1.5"><Label for="ds-name">Name</Label><Input id="ds-name" bind:value={newName} placeholder={newKind === 'rest' ? 'Shop API' : 'Sales database'} required /></div>
      {#if newKind === 'rest'}
        <div class="flex flex-col gap-1.5"><Label for="ds-url">Address</Label><Input id="ds-url" class="font-mono text-xs" bind:value={newUrl} placeholder={'http://192.168.1.10:8080  or  http://{{ env.HOST }}/v1'} /></div>
      {:else}
        <div class="flex flex-col gap-1.5"><Label for="ds-url">Connection string <span class="text-muted-foreground font-normal">(optional)</span></Label><Input id="ds-url" class="font-mono text-xs" bind:value={newUrl} placeholder={'{{ env.DATABASE_URL }}'} />
          <p class="text-muted-foreground text-xs">Or leave it empty and fill in the host, user and password next.</p></div>
      {/if}
      {#if addError}<p class="text-destructive text-sm">{addError}</p>{/if}
    </form>
    <Dialog.Footer><Button variant="outline" onclick={() => (adding = false)}>Cancel</Button><Button type="submit" form="new-ds">Create</Button></Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
<ConfirmHost bind:this={confirmHost} />
