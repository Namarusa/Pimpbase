<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import * as Tabs from '$lib/components/ui/tabs'
  import * as ToggleGroup from '$lib/components/ui/toggle-group'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Badge } from '$lib/components/ui/badge'
  import { Spinner } from '$lib/components/ui/spinner'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import SendIcon from '@lucide/svelte/icons/send'
  import SparklesIcon from '@lucide/svelte/icons/sparkles'
  import BindingInput from '../designer/BindingInput.svelte'
  import { api, type BodyType, type Datasource, type ParamType, type Query, type QueryResult, type RoleDef, type Variable } from '../api'
  import RoleSelect from '../access/RoleSelect.svelte'
  import { apiHelp, countOn, paramsUsed, queryBindings } from './bindings'
  import KeyValueEditor from './KeyValueEditor.svelte'
  import TemplateEditor from './TemplateEditor.svelte'
  import ResponseView from './ResponseView.svelte'

  let { appId, query, datasources, queries, variables, onChange }: {
    appId: string; query: Query; datasources: Datasource[]; queries: Query[]; variables: Variable[]
    onChange: (p: Partial<Query>) => void
  } = $props()

  const ds = $derived(datasources.find((d) => d.id === query.datasourceId))
  // Queries on a database are SQL, or a MongoDB operation.
  const db = $derived(!!ds && ds.kind !== 'rest')
  const mongo = $derived(ds?.kind === 'mongodb')
  const mongoOps = ['find', 'findOne', 'aggregate', 'count', 'insertOne', 'insertMany', 'updateOne', 'updateMany', 'deleteOne', 'deleteMany']
  const usesDocument = $derived(['insertOne', 'insertMany', 'updateOne', 'updateMany'].includes(query.operation || 'find'))
  const sqlStarter = 'SELECT *\nFROM customers\nWHERE country = {{ params.country }}\nORDER BY name\nLIMIT 100'
  const sqlHelp = 'Bindings are sent as parameters, never pasted into the SQL: write them without quotes, as in WHERE id = {{ params.id }}. A list fills IN ({{ params.ids }}).'
  const mongoHelp = 'Extended JSON. Bindings inside quotes are escaped text; outside quotes they are JSON values.'
  const groups = $derived(queryBindings(query, variables, queries))
  const missing = $derived(paramsUsed({ ...query, path: [query.path, query.sql, query.collection, query.filter, query.document, query.options].join('\n') }).filter((n) => !query.parameters.some((p) => p.name === n)))
  let tab = $state(query.parameters.length ? 'params' : datasources.find((d) => d.id === query.datasourceId && d.kind !== 'rest') ? 'statement' : 'query')
  let env = $state<'development' | 'production'>('development')
  let running = $state(false)
  let result = $state<QueryResult | null>(null)
  let error = $state('')

  const methods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'HEAD', 'OPTIONS']
  const methodColor: Record<string, string> = { GET: 'text-emerald-600 dark:text-emerald-400', POST: 'text-amber-600 dark:text-amber-400', PUT: 'text-sky-600 dark:text-sky-400', PATCH: 'text-violet-600 dark:text-violet-400', DELETE: 'text-red-600 dark:text-red-400' }
  const bodyTypes: { value: BodyType; label: string }[] = [
    { value: 'none', label: 'None' }, { value: 'json', label: 'JSON' }, { value: 'form', label: 'Form' }, { value: 'xml', label: 'XML' }, { value: 'text', label: 'Text' },
  ]
  const paramTypes: ParamType[] = ['text', 'number', 'boolean', 'json']
  let roles = $state<RoleDef[]>([])
  $effect(() => { api.roles(appId).then((r) => (roles = r)).catch(() => {}) })

  const starters: Record<string, string> = {
    json: '{\n  "name": "{{ params.name }}",\n  "limit": {{ params.limit }}\n}',
    xml: '<?xml version="1.0" encoding="UTF-8"?>\n<Request>\n  <Name>{{ params.name }}</Name>\n</Request>',
  }
  function setBodyType(t: BodyType) {
    const p: Partial<Query> = { bodyType: t }
    if (!query.body.trim() && starters[t]) p.body = starters[t]
    if (t !== 'none' && query.method === 'GET') p.method = 'POST'
    onChange(p)
  }

  function setParam(i: number, p: Partial<Query['parameters'][number]>) {
    onChange({ parameters: query.parameters.map((x, j) => (j === i ? { ...x, ...p } : x)) })
  }
  function declare(names: string[]) {
    onChange({ parameters: [...query.parameters, ...names.map((name) => ({ name, type: 'text' as ParamType, default: '', test: '' }))] })
    tab = 'params'
  }

  async function run() {
    running = true
    error = ''
    try {
      result = await api.testQuery(appId, $state.snapshot(query) as Query, env)
      // The designer offers the fields of the last good run.
      if (result.ok && JSON.stringify(result.fields) !== JSON.stringify(query.fields)) onChange({ fields: result.fields })
    } catch (e: any) {
      result = null
      error = e.message
    } finally {
      running = false
    }
  }
  function keydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key === 'Enter') { e.preventDefault(); run() }
  }
</script>

<svelte:window onkeydown={keydown} />

<div class="flex h-full min-h-0 flex-col">
  <!-- Request line -->
  <div class="flex flex-col gap-3 border-b p-4">
    <div class="flex items-center gap-3">
      <Input class="h-9 max-w-xs border-transparent px-1 text-lg font-semibold md:text-lg shadow-none hover:border-input focus-visible:border-input" value={query.name}
        onchange={(e) => onChange({ name: e.currentTarget.value })} aria-label="Query name" />
      <Select.Root type="single" value={query.datasourceId} onValueChange={(v) => onChange({ datasourceId: v })}>
        <Select.Trigger size="sm" class="w-48">{ds?.name ?? 'Choose a data source'}</Select.Trigger>
        <Select.Content>{#each datasources as d (d.id)}<Select.Item value={d.id} label={d.name}>{d.name}</Select.Item>{/each}</Select.Content>
      </Select.Root>
      <ToggleGroup.Root type="single" variant="outline" size="sm" class="ml-auto" value={env} onValueChange={(v) => v && (env = v as typeof env)}>
        <ToggleGroup.Item value="development" class="px-2.5 text-xs">Development</ToggleGroup.Item>
        <ToggleGroup.Item value="production" class="px-2.5 text-xs">Production</ToggleGroup.Item>
      </ToggleGroup.Root>
    </div>
    {#if db}
      <div class="flex items-center gap-2">
        <Badge variant="secondary" class="font-mono text-[11px] uppercase">{mongo ? 'MongoDB' : 'SQL'}</Badge>
        {#if mongo}
          <Select.Root type="single" value={query.operation || 'find'} onValueChange={(v) => onChange({ operation: v })}>
            <Select.Trigger size="sm" class="w-36 font-mono text-xs">{query.operation || 'find'}</Select.Trigger>
            <Select.Content>{#each mongoOps as o (o)}<Select.Item value={o} label={o} class="font-mono text-xs">{o}</Select.Item>{/each}</Select.Content>
          </Select.Root>
          <div class="min-w-0 flex-1"><BindingInput mono value={query.collection} {groups} help={apiHelp} placeholder="Collection, such as orders" onChange={(v) => onChange({ collection: v })} /></div>
        {:else}
          <span class="min-w-0 flex-1"></span>
        {/if}
        <Button onclick={run} disabled={running || !ds} title="Run (Ctrl+Enter)">{#if running}<Spinner />{:else}<SendIcon />{/if}Run</Button>
      </div>
    {:else}
    <div class="flex items-center gap-2">
      <Select.Root type="single" value={query.method} onValueChange={(v) => onChange({ method: v })}>
        <Select.Trigger size="sm" class={['w-28 font-mono text-xs font-semibold', methodColor[query.method]]}>{query.method}</Select.Trigger>
        <Select.Content>{#each methods as m (m)}<Select.Item value={m} label={m} class={['font-mono text-xs font-semibold', methodColor[m]]}>{m}</Select.Item>{/each}</Select.Content>
      </Select.Root>
      <div class="flex min-w-0 flex-1 items-center rounded-md border shadow-xs">
        {#if ds?.config.baseUrl}<span class="text-muted-foreground bg-muted/50 max-w-[40%] shrink-0 truncate border-r px-2.5 py-1.5 font-mono text-xs" title={ds.config.baseUrl}>{ds.config.baseUrl}</span>{/if}
        <div class="min-w-0 flex-1"><BindingInput mono value={query.path} {groups} help={apiHelp} placeholder={ds?.config.baseUrl ? '/items/{{ params.id }}' : 'https://api.example.com/items'} onChange={(v) => onChange({ path: v })} class="border-0 shadow-none focus-visible:ring-0" /></div>
      </div>
      <Button onclick={run} disabled={running || !ds} title="Send (Ctrl+Enter)">{#if running}<Spinner />{:else}<SendIcon />{/if}Send</Button>
    </div>
    {/if}
    {#if missing.length}
      <div class="flex items-center gap-2 text-xs">
        <span class="text-muted-foreground">Used but not declared:</span>
        {#each missing as m (m)}<code class="bg-muted rounded px-1.5 py-0.5">params.{m}</code>{/each}
        <Button variant="link" size="sm" class="h-auto p-0 text-xs" onclick={() => declare(missing)}><SparklesIcon class="size-3" />Add as parameters</Button>
      </div>
    {/if}
  </div>

  <div class="grid min-h-0 flex-1 grid-rows-[minmax(0,1fr)_minmax(0,1fr)]">
    <!-- Request settings -->
    <Tabs.Root bind:value={tab} class="flex min-h-0 flex-col gap-0 border-b">
      <div class="px-4 pt-2">
        <Tabs.List class="h-8">
          {#if db}<Tabs.Trigger value="statement" class="text-xs">{mongo ? 'Filter' : 'SQL'}</Tabs.Trigger>{/if}
          <Tabs.Trigger value="params" class="text-xs">Parameters {#if query.parameters.length}<Badge variant="secondary" class="h-4 px-1 text-[10px]">{query.parameters.length}</Badge>{/if}</Tabs.Trigger>
          {#if !db}
          <Tabs.Trigger value="query" class="text-xs">Query string {#if countOn(query.query)}<Badge variant="secondary" class="h-4 px-1 text-[10px]">{countOn(query.query)}</Badge>{/if}</Tabs.Trigger>
          <Tabs.Trigger value="headers" class="text-xs">Headers {#if countOn(query.headers)}<Badge variant="secondary" class="h-4 px-1 text-[10px]">{countOn(query.headers)}</Badge>{/if}</Tabs.Trigger>
          <Tabs.Trigger value="body" class="text-xs">Body {#if query.bodyType !== 'none'}<Badge variant="secondary" class="h-4 px-1 text-[10px] uppercase">{query.bodyType}</Badge>{/if}</Tabs.Trigger>
          {/if}
          <Tabs.Trigger value="settings" class="text-xs">Settings</Tabs.Trigger>
        </Tabs.List>
      </div>
      <div class="min-h-0 flex-1 overflow-y-auto p-4">
        {#if db}
          <Tabs.Content value="statement" class="flex flex-col gap-3">
            {#if mongo}
              <div class="flex flex-col gap-1.5"><Label class="text-xs">{query.operation === 'aggregate' ? 'Pipeline' : 'Filter'}</Label>
                <TemplateEditor value={query.filter} onChange={(v) => onChange({ filter: v })} {groups} minRows={5} placeholder={query.operation === 'aggregate' ? '[{ "$match": { "status": "{{ params.status }}" } }]' : '{ "status": "{{ params.status }}" }'} help={mongoHelp} /></div>
              {#if usesDocument}
                <div class="flex flex-col gap-1.5"><Label class="text-xs">{query.operation?.startsWith('update') ? 'Update' : 'Document'}</Label>
                  <TemplateEditor value={query.document} onChange={(v) => onChange({ document: v })} {groups} minRows={4} placeholder={query.operation?.startsWith('update') ? '{ "$set": { "status": "{{ params.status }}" } }' : '{ "name": "{{ params.name }}" }'} help={mongoHelp} /></div>
              {/if}
              <div class="flex flex-col gap-1.5"><Label class="text-xs">Options</Label>
                <TemplateEditor value={query.options} onChange={(v) => onChange({ options: v })} {groups} minRows={2} placeholder={'{ "sort": { "createdAt": -1 }, "limit": 50 }'} help="sort, limit, skip and projection for find; upsert for updates." /></div>
            {:else}
              <TemplateEditor value={query.sql} onChange={(v) => onChange({ sql: v })} {groups} minRows={9} placeholder={sqlStarter} help={sqlHelp} />
              <p class="text-muted-foreground text-xs">{sqlHelp} Statements that do not read rows report how many rows they changed.</p>
            {/if}
          </Tabs.Content>
        {/if}
        <Tabs.Content value="params">
          <p class="text-muted-foreground mb-3 text-xs">Parameters are what screens pass in, read as <code>{'{{ params.name }}'}</code>. Test values are used when you send from here.</p>
          {#if query.parameters.length}
            <div class="text-muted-foreground mb-1.5 grid grid-cols-[minmax(0,2fr)_7rem_minmax(0,2fr)_minmax(0,2fr)_1.75rem] gap-2 text-[11px] font-medium tracking-wide uppercase">
              <span>Name</span><span>Type</span><span>Default</span><span>Test value</span><span></span>
            </div>
          {/if}
          <div class="flex flex-col gap-1.5">
            {#each query.parameters as p, i (i)}
              <div class="grid grid-cols-[minmax(0,2fr)_7rem_minmax(0,2fr)_minmax(0,2fr)_1.75rem] items-center gap-2">
                <Input class="h-8 font-mono text-xs" value={p.name} placeholder="name" onchange={(e) => setParam(i, { name: e.currentTarget.value.trim() })} aria-label="Parameter name" />
                <Select.Root type="single" value={p.type} onValueChange={(v) => setParam(i, { type: v as ParamType })}>
                  <Select.Trigger size="sm" class="w-full text-xs">{p.type}</Select.Trigger>
                  <Select.Content>{#each paramTypes as t (t)}<Select.Item value={t} label={t}>{t}</Select.Item>{/each}</Select.Content>
                </Select.Root>
                <Input class="h-8 font-mono text-xs" value={p.default} placeholder={p.type === 'json' ? '[]' : ''} oninput={(e) => setParam(i, { default: e.currentTarget.value })} aria-label="Default value" />
                <Input class="h-8 font-mono text-xs" value={p.test} placeholder={p.default} oninput={(e) => setParam(i, { test: e.currentTarget.value })} aria-label="Test value" />
                <Button variant="ghost" size="icon" class="size-7" aria-label="Remove parameter" onclick={() => onChange({ parameters: query.parameters.filter((_, j) => j !== i) })}><XIcon class="size-3.5" /></Button>
              </div>
            {/each}
            <Button variant="outline" size="sm" class="h-7 self-start border-dashed text-xs" onclick={() => onChange({ parameters: [...query.parameters, { name: '', type: 'text', default: '', test: '' }] })}><PlusIcon />Add parameter</Button>
          </div>
        </Tabs.Content>
        <Tabs.Content value="query">
          <KeyValueEditor value={query.query} onChange={(v) => onChange({ query: v })} {groups} addLabel="Add query parameter" keyPlaceholder="limit" valuePlaceholder={'{{ params.limit }}'} />
          <p class="text-muted-foreground mt-3 text-xs">Values are URL-encoded for you. A list adds the parameter once per item.</p>
        </Tabs.Content>
        <Tabs.Content value="headers">
          <KeyValueEditor value={query.headers} onChange={(v) => onChange({ headers: v })} {groups} addLabel="Add header" keyPlaceholder="Authorization" valuePlaceholder={'Bearer {{ queries.Login.body.token }}'} />
          {#if ds && (countOn(ds.config.headers) || ds.config.auth.type !== 'none')}
            <p class="text-muted-foreground mt-3 text-xs">{ds.name} also sends {countOn(ds.config.headers)} header{countOn(ds.config.headers) === 1 ? '' : 's'}{ds.config.auth.type !== 'none' ? ` and ${ds.config.auth.type} sign-in` : ''}. Headers set here win.</p>
          {/if}
        </Tabs.Content>
        <Tabs.Content value="body" class="flex flex-col gap-3">
          <ToggleGroup.Root type="single" variant="outline" size="sm" class="self-start" value={query.bodyType} onValueChange={(v) => v && setBodyType(v as BodyType)}>
            {#each bodyTypes as b (b.value)}<ToggleGroup.Item value={b.value} class="px-3 text-xs">{b.label}</ToggleGroup.Item>{/each}
          </ToggleGroup.Root>
          {#if query.bodyType === 'form'}
            <KeyValueEditor value={query.form} onChange={(v) => onChange({ form: v })} {groups} addLabel="Add field" keyPlaceholder="email" valuePlaceholder={'{{ user.email }}'} />
          {:else if query.bodyType !== 'none'}
            <TemplateEditor value={query.body} onChange={(v) => onChange({ body: v })} {groups} />
            <p class="text-muted-foreground text-xs">
              {#if query.bodyType === 'json'}Inside quotes a binding is written as escaped text; outside quotes it is written as JSON, so numbers, lists and objects keep their type. Use <code>| json</code> to embed a value as JSON text.
              {:else if query.bodyType === 'xml'}Bindings are escaped as XML text. <code>{'{{ params.items | xml:"Item" }}'}</code> writes a list or object as elements; <code>| raw</code> inserts XML as it is. The body is checked to be well formed before it is sent.
              {:else}The text is sent as it is, with bindings filled.{/if}
            </p>
          {:else}
            <p class="text-muted-foreground text-xs">This query sends no body.</p>
          {/if}
        </Tabs.Content>
        <Tabs.Content value="settings" class="grid max-w-2xl grid-cols-2 gap-4">
          {#if !db}
          <div class="flex flex-col gap-1.5"><Label class="text-xs">Read the answer as</Label>
            <ToggleGroup.Root type="single" variant="outline" size="sm" class="w-full" value={query.responseType} onValueChange={(v) => v && onChange({ responseType: v as Query['responseType'] })}>
              {#each [['auto', 'Auto'], ['json', 'JSON'], ['xml', 'XML'], ['text', 'Text']] as [v, l] (v)}<ToggleGroup.Item value={v} class="flex-1 text-xs">{l}</ToggleGroup.Item>{/each}
            </ToggleGroup.Root>
            <p class="text-muted-foreground text-xs">Auto follows the Content-Type. XML becomes data: attributes are <code>@name</code>, repeated elements lists.</p></div>
          <div class="flex flex-col gap-1.5"><Label for="q-rows" class="text-xs">Rows are at</Label>
            <Input id="q-rows" class="h-8 font-mono text-xs" value={query.resultPath} placeholder="data.items" oninput={(e) => onChange({ resultPath: e.currentTarget.value })} />
            <p class="text-muted-foreground text-xs">A path into the body. Empty uses the first list found.</p></div>
          {/if}
          <div class="flex flex-col gap-1.5"><Label class="text-xs">Who can run it in the app</Label>
            <RoleSelect {roles} value={query.role} onChange={(v) => onChange({ role: v })} /></div>
          <div class="flex flex-col gap-1.5"><Label for="q-cache" class="text-xs">Reuse answers for (seconds)</Label>
            <Input id="q-cache" type="number" min="0" class="h-8" value={query.cacheSeconds} oninput={(e) => onChange({ cacheSeconds: Math.max(0, Number(e.currentTarget.value) || 0) })} />
            <p class="text-muted-foreground text-xs">Useful for sign-in queries other queries read. 0 sends every time.</p></div>
        </Tabs.Content>
      </div>
    </Tabs.Root>

    <ResponseView {result} {error} {running} queryName={query.name} resultPath={query.resultPath} onRows={(p) => { onChange({ resultPath: p }); queueMicrotask(run) }} />
  </div>
</div>
