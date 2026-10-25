<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import * as Card from '$lib/components/ui/card'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import TrashIcon from '@lucide/svelte/icons/trash'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import GlobeIcon from '@lucide/svelte/icons/globe'
  import BindingInput from '../designer/BindingInput.svelte'
  import type { Auth, Datasource, Query, Variable } from '../api'
  import { apiHelp, queryBindings } from './bindings'
  import KeyValueEditor from './KeyValueEditor.svelte'

  // DatasourceEditor edits an API's address, shared headers and sign-in.
  let { datasource, variables, queries, onChange, onDelete, onNewQuery, onOpen }: {
    datasource: Datasource; variables: Variable[]; queries: Query[]
    onChange: (p: Partial<Datasource>) => void; onDelete: () => void; onNewQuery: () => void; onOpen: (q: Query) => void
  } = $props()
  // The address and headers run with each query's own values, so they can
  // read any of its queries' parameters as well as variables and results.
  const groups = $derived.by(() => {
    const mine = queries.filter((q) => q.datasourceId === datasource.id)
    const seen = new Set<string>()
    const parameters = mine.flatMap((q) => q.parameters).filter((p) => p.name && !seen.has(p.name) && seen.add(p.name))
    return queryBindings({ name: '', parameters }, variables, queries)
  })
  const config = $derived(datasource.config)
  const setConfig = (p: Partial<Datasource['config']>) => onChange({ config: { ...config, ...p } })
  const setAuth = (p: Partial<Auth>) => setConfig({ auth: { ...config.auth, ...p } })
  const authTypes: { value: Auth['type']; label: string }[] = [
    { value: 'none', label: 'None' }, { value: 'bearer', label: 'Bearer token' }, { value: 'basic', label: 'User name and password' },
    { value: 'header', label: 'API key in a header' }, { value: 'query', label: 'API key in the query string' },
  ]
  const own = $derived(queries.filter((q) => q.datasourceId === datasource.id))
</script>

<div class="mx-auto flex max-w-3xl flex-col gap-6 p-6">
  <div class="flex items-center gap-3">
    <span class="bg-primary/10 text-primary grid size-10 place-items-center rounded-lg"><GlobeIcon class="size-5" /></span>
    <div class="min-w-0 flex-1">
      <Input class="h-9 border-transparent px-1 text-lg font-semibold md:text-lg shadow-none hover:border-input focus-visible:border-input" value={datasource.name} onchange={(e) => onChange({ name: e.currentTarget.value })} aria-label="Data source name" />
      <p class="text-muted-foreground px-1 text-xs">REST API · {own.length} {own.length === 1 ? 'query' : 'queries'}</p>
    </div>
    <Button variant="outline" size="sm" onclick={onNewQuery}><PlusIcon />New query</Button>
    <Button variant="ghost" size="icon" class="hover:text-destructive" onclick={onDelete} aria-label="Delete data source"><TrashIcon /></Button>
  </div>

  <Card.Root>
    <Card.Header><Card.Title class="text-sm">Address</Card.Title><Card.Description>Queries add their path to it. Plain http, IP addresses, localhost and ports all work, and a {'{{ binding }}'} can go anywhere: the scheme, host, port or path.</Card.Description></Card.Header>
    <Card.Content class="flex flex-col gap-4">
      <BindingInput mono value={config.baseUrl} {groups} help={apiHelp} placeholder={'http://192.168.1.10:8080/api  or  http://{{ env.HOST }}:{{ env.PORT }}/v1'} onChange={(v) => setConfig({ baseUrl: v })} />
      <div class="flex items-center gap-2">
        <Label for="ds-timeout" class="text-xs font-normal">Give up after</Label>
        <Input id="ds-timeout" type="number" min="1" max="120" class="h-8 w-20" value={config.timeoutSeconds} oninput={(e) => setConfig({ timeoutSeconds: Number(e.currentTarget.value) || 30 })} />
        <span class="text-muted-foreground text-xs">seconds</span>
      </div>
    </Card.Content>
  </Card.Root>

  <Card.Root>
    <Card.Header><Card.Title class="text-sm">Sign-in</Card.Title><Card.Description>Added to every query. Keep keys in secret variables so they never reach the browser.</Card.Description></Card.Header>
    <Card.Content class="flex flex-col gap-3">
      <Select.Root type="single" value={config.auth.type} onValueChange={(v) => setAuth({ type: v as Auth['type'] })}>
        <Select.Trigger size="sm" class="w-64">{authTypes.find((a) => a.value === config.auth.type)?.label}</Select.Trigger>
        <Select.Content>{#each authTypes as a (a.value)}<Select.Item value={a.value} label={a.label}>{a.label}</Select.Item>{/each}</Select.Content>
      </Select.Root>
      {#if config.auth.type === 'bearer'}
        <div class="flex flex-col gap-1.5"><Label class="text-xs">Token</Label><BindingInput mono value={config.auth.token ?? ''} {groups} help={apiHelp} placeholder={'{{ env.API_TOKEN }}'} onChange={(v) => setAuth({ token: v })} /></div>
      {:else if config.auth.type === 'basic'}
        <div class="grid grid-cols-2 gap-3">
          <div class="flex flex-col gap-1.5"><Label class="text-xs">User name</Label><BindingInput mono value={config.auth.username ?? ''} {groups} help={apiHelp} onChange={(v) => setAuth({ username: v })} /></div>
          <div class="flex flex-col gap-1.5"><Label class="text-xs">Password</Label><BindingInput mono value={config.auth.password ?? ''} {groups} help={apiHelp} placeholder={'{{ env.API_PASSWORD }}'} onChange={(v) => setAuth({ password: v })} /></div>
        </div>
      {:else if config.auth.type === 'header' || config.auth.type === 'query'}
        <div class="grid grid-cols-[minmax(0,1fr)_minmax(0,2fr)] gap-3">
          <div class="flex flex-col gap-1.5"><Label class="text-xs">{config.auth.type === 'header' ? 'Header' : 'Parameter'}</Label><Input class="h-8 font-mono text-xs" value={config.auth.name ?? ''} placeholder={config.auth.type === 'header' ? 'X-Api-Key' : 'api_key'} oninput={(e) => setAuth({ name: e.currentTarget.value })} /></div>
          <div class="flex flex-col gap-1.5"><Label class="text-xs">Key</Label><BindingInput mono value={config.auth.value ?? ''} {groups} help={apiHelp} placeholder={'{{ env.API_KEY }}'} onChange={(v) => setAuth({ value: v })} /></div>
        </div>
      {/if}
    </Card.Content>
  </Card.Root>

  <Card.Root>
    <Card.Header><Card.Title class="text-sm">Headers for every query</Card.Title></Card.Header>
    <Card.Content><KeyValueEditor value={config.headers} onChange={(v) => setConfig({ headers: v })} {groups} addLabel="Add header" keyPlaceholder="Accept-Language" valuePlaceholder="en" /></Card.Content>
  </Card.Root>

  {#if own.length}
    <Card.Root>
      <Card.Header><Card.Title class="text-sm">Queries</Card.Title></Card.Header>
      <Card.Content class="flex flex-col gap-1">
        {#each own as q (q.id)}
          <button type="button" class="hover:bg-muted flex items-center gap-3 rounded-md px-2 py-1.5 text-left" onclick={() => onOpen(q)}>
            <span class="w-14 font-mono text-[11px] font-semibold">{q.method}</span>
            <span class="text-sm font-medium">{q.name}</span>
            <span class="text-muted-foreground min-w-0 flex-1 truncate font-mono text-xs">{q.path}</span>
          </button>
        {/each}
      </Card.Content>
    </Card.Root>
  {/if}
</div>
