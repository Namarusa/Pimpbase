<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import * as Card from '$lib/components/ui/card'
  import * as Alert from '$lib/components/ui/alert'
  import * as ToggleGroup from '$lib/components/ui/toggle-group'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Badge } from '$lib/components/ui/badge'
  import { Switch } from '$lib/components/ui/switch'
  import { Spinner } from '$lib/components/ui/spinner'
  import { toast } from 'svelte-sonner'
  import TrashIcon from '@lucide/svelte/icons/trash'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import DatabaseIcon from '@lucide/svelte/icons/database'
  import PlugZapIcon from '@lucide/svelte/icons/plug-zap'
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check'
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert'
  import TableIcon from '@lucide/svelte/icons/table-2'
  import EyeIcon from '@lucide/svelte/icons/eye'
  import LayersIcon from '@lucide/svelte/icons/layers'
  import ArrowRightIcon from '@lucide/svelte/icons/arrow-right'
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw'
  import BindingInput from '../designer/BindingInput.svelte'
  import { api, type ConnectionTest, type Datasource, type DbKind, type DbSettings, type DbTableInfo, type Query, type Variable } from '../api'
  import { navigate } from '../router.svelte'
  import { apiHelp } from './bindings'
  import KeyValueEditor from './KeyValueEditor.svelte'
  import { dbLabels } from './kinds'

  // DbSourceEditor edits how a database is reached, tests the connection
  // and imports its tables into the app.
  let { appId, slug, datasource, variables, queries, onChange, onDelete, onNewQuery, onOpen }: {
    appId: string; slug: string; datasource: Datasource; variables: Variable[]; queries: Query[]
    onChange: (p: Partial<Datasource>) => void; onDelete: () => void; onNewQuery: () => void; onOpen: (q: Query) => void
  } = $props()
  const kind = $derived(datasource.kind as DbKind)
  const db = $derived<DbSettings>({ host: '', port: '', database: '', user: '', password: '', options: [], ...(datasource.config.db ?? {}) })
  const groups = $derived([{ label: 'Variables', items: variables.map((v) => ({ path: `env.${v.name}`, hint: v.secret ? 'secret' : '' })) }])
  const set = (p: Partial<DbSettings>) => onChange({ config: { ...datasource.config, db: { ...db, ...p } } })
  const own = $derived(queries.filter((q) => q.datasourceId === datasource.id))
  const ports: Record<DbKind, string> = { postgres: '5432', mysql: '3306', mssql: '1433', mongodb: '27017' }
  const schemaHint: Record<DbKind, string> = { postgres: 'public', mysql: 'the database', mssql: 'dbo', mongodb: '' }
  const sslModes = [
    { value: '', label: 'Use it when offered' }, { value: 'disable', label: 'Off' },
    { value: 'require', label: 'Required' }, { value: 'verify', label: 'Required, check the certificate' },
  ]
  const passwordSecret = $derived(variables.some((v) => v.secret && db.password.includes(`env.${v.name}`)))
  const plainPassword = $derived(!!db.password && !db.password.includes('{{'))

  let env = $state<'development' | 'production'>('development')
  let testing = $state(false)
  let test = $state<ConnectionTest | null>(null)
  let testError = $state('')
  async function runTest() {
    testing = true
    test = null
    testError = ''
    try { test = await api.testConnection(appId, kind, $state.snapshot(datasource.config) as Datasource['config'], env) }
    catch (e: any) { testError = e.message }
    finally { testing = false }
  }

  let tables = $state<DbTableInfo[] | null>(null)
  let loadingTables = $state(false)
  let tablesError = $state('')
  let importing = $state('')
  async function loadTables() {
    loadingTables = true
    tablesError = ''
    try { tables = await api.dbTables(appId, datasource.id) }
    catch (e: any) { tablesError = e.message; tables = null }
    finally { loadingTables = false }
  }
  async function importTable(t: DbTableInfo) {
    importing = t.schema + '.' + t.name
    try {
      const made = await api.importTable(appId, datasource.id, { schema: t.schema, source: t.name, name: t.name })
      tables = (tables ?? []).map((x) => (x === t ? { ...x, imported: made.id } : x))
      toast.success(`${made.name} is now a table of the app`, { action: { label: 'Open', onClick: () => navigate(`/apps/${slug}/data/${made.id}`) } })
    } catch (e: any) { toast.error(e.message) }
    finally { importing = '' }
  }
  const kindIcon = { table: TableIcon, view: EyeIcon, collection: LayersIcon }
</script>

<div class="mx-auto flex max-w-3xl flex-col gap-6 p-6">
  <div class="flex items-center gap-3">
    <span class="bg-primary/10 text-primary grid size-10 place-items-center rounded-lg"><DatabaseIcon class="size-5" /></span>
    <div class="min-w-0 flex-1">
      <Input class="h-9 border-transparent px-1 text-lg font-semibold md:text-lg shadow-none hover:border-input focus-visible:border-input" value={datasource.name} onchange={(e) => onChange({ name: e.currentTarget.value })} aria-label="Data source name" />
      <p class="text-muted-foreground px-1 text-xs">{dbLabels[kind]} · {own.length} {own.length === 1 ? 'query' : 'queries'}{db.readOnly ? ' · read only' : ''}</p>
    </div>
    <Button variant="outline" size="sm" onclick={onNewQuery}><PlusIcon />New query</Button>
    <Button variant="ghost" size="icon" class="hover:text-destructive" onclick={onDelete} aria-label="Delete data source"><TrashIcon /></Button>
  </div>

  <Card.Root>
    <Card.Header>
      <Card.Title class="text-sm">Connection</Card.Title>
      <Card.Description>Pimpbase connects to your server; nothing is copied. Point development and production at different databases with variables.</Card.Description>
    </Card.Header>
    <Card.Content class="flex flex-col gap-4">
      <div class="grid grid-cols-[minmax(0,3fr)_minmax(0,1fr)] gap-3">
        <div class="flex flex-col gap-1.5"><Label class="text-xs">Host</Label><BindingInput mono value={db.host} {groups} help={apiHelp} placeholder={'db.example.com  or  {{ env.DB_HOST }}'} onChange={(v) => set({ host: v })} /></div>
        <div class="flex flex-col gap-1.5"><Label class="text-xs">Port</Label><BindingInput mono value={db.port} {groups} help={apiHelp} placeholder={ports[kind]} onChange={(v) => set({ port: v })} /></div>
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div class="flex flex-col gap-1.5"><Label class="text-xs">Database</Label><BindingInput mono value={db.database} {groups} help={apiHelp} placeholder="sales" onChange={(v) => set({ database: v })} /></div>
        {#if kind !== 'mongodb'}
          <div class="flex flex-col gap-1.5"><Label class="text-xs">Schema</Label><BindingInput mono value={db.schema ?? ''} {groups} help={apiHelp} placeholder={schemaHint[kind]} onChange={(v) => set({ schema: v })} /></div>
        {/if}
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div class="flex flex-col gap-1.5"><Label class="text-xs">User</Label><BindingInput mono value={db.user} {groups} help={apiHelp} placeholder={'{{ env.DB_USER }}'} onChange={(v) => set({ user: v })} /></div>
        <div class="flex flex-col gap-1.5"><Label class="text-xs">Password</Label><BindingInput mono value={db.password} {groups} help={apiHelp} placeholder={'{{ env.DB_PASSWORD }}'} onChange={(v) => set({ password: v })} />
          {#if plainPassword}<p class="text-amber-600 dark:text-amber-400 text-xs">Builders can read a password typed here. Keep it in a secret variable instead.</p>
          {:else if passwordSecret}<p class="text-muted-foreground text-xs">Read from a secret variable; it never reaches the browser.</p>{/if}
        </div>
      </div>
      <div class="flex flex-col gap-1.5"><Label class="text-xs">Or a connection string</Label>
        <BindingInput mono value={db.uri ?? ''} {groups} help={apiHelp} placeholder={kind === 'mongodb' ? 'mongodb+srv://user:pass@cluster.example.net/sales  or  {{ env.MONGO_URL }}' : '{{ env.DATABASE_URL }}'} onChange={(v) => set({ uri: v })} />
        <p class="text-muted-foreground text-xs">When set, it is used instead of the fields above.</p>
      </div>
      <div class="grid grid-cols-2 gap-3">
        <div class="flex flex-col gap-1.5"><Label class="text-xs">Encryption (TLS)</Label>
          <Select.Root type="single" value={db.ssl ?? ''} onValueChange={(v) => set({ ssl: v as DbSettings['ssl'] })}>
            <Select.Trigger size="sm" class="w-full">{sslModes.find((m) => m.value === (db.ssl ?? ''))?.label}</Select.Trigger>
            <Select.Content>{#each sslModes as m (m.value)}<Select.Item value={m.value} label={m.label}>{m.label}</Select.Item>{/each}</Select.Content>
          </Select.Root>
        </div>
        <div class="flex items-end gap-3">
          <div class="flex flex-col gap-1.5"><Label for="db-conns" class="text-xs">Connections</Label>
            <Input id="db-conns" type="number" min="1" max="100" class="h-8 w-20" value={db.maxConnections || 5} oninput={(e) => set({ maxConnections: Number(e.currentTarget.value) || 5 })} /></div>
          <div class="flex flex-col gap-1.5"><Label for="db-timeout" class="text-xs">Timeout (s)</Label>
            <Input id="db-timeout" type="number" min="1" max="300" class="h-8 w-20" value={datasource.config.timeoutSeconds} oninput={(e) => onChange({ config: { ...datasource.config, db, timeoutSeconds: Number(e.currentTarget.value) || 30 } })} /></div>
        </div>
      </div>
      <label class="flex items-center gap-3 text-sm">
        <Switch checked={!!db.readOnly} onCheckedChange={(v) => set({ readOnly: v })} />
        <span>Read only <span class="text-muted-foreground text-xs">— tables and queries cannot change data</span></span>
      </label>
      <details class="text-sm">
        <summary class="text-muted-foreground cursor-pointer text-xs">Driver options</summary>
        <div class="mt-2"><KeyValueEditor value={db.options.map((o) => ({ key: o.key, value: o.value }))} onChange={(v) => set({ options: v.map((p) => ({ key: p.key, value: p.value })) })} {groups} addLabel="Add option" keyPlaceholder={kind === 'postgres' ? 'search_path' : kind === 'mysql' ? 'collation' : kind === 'mssql' ? 'ApplicationIntent' : 'replicaSet'} valuePlaceholder="value" /></div>
      </details>
      <div class="flex items-center gap-3 border-t pt-4">
        <Button variant="outline" size="sm" onclick={runTest} disabled={testing}>{#if testing}<Spinner />{:else}<PlugZapIcon />{/if}Test connection</Button>
        <ToggleGroup.Root type="single" variant="outline" size="sm" value={env} onValueChange={(v) => v && (env = v as typeof env)}>
          <ToggleGroup.Item value="development" class="px-2.5 text-xs">Development</ToggleGroup.Item>
          <ToggleGroup.Item value="production" class="px-2.5 text-xs">Production</ToggleGroup.Item>
        </ToggleGroup.Root>
      </div>
      {#if test}
        <Alert.Root variant={test.ok ? 'default' : 'destructive'} class={test.ok ? 'border-emerald-500/40 text-emerald-700 dark:text-emerald-400' : ''}>
          {#if test.ok}<CircleCheckIcon />{:else}<CircleAlertIcon />{/if}
          <Alert.Title>{test.ok ? `Connected in ${test.durationMs} ms` : 'Could not connect'}</Alert.Title>
          <Alert.Description>{test.ok ? `${test.tables >= 0 ? test.tables : 'Some'} ${kind === 'mongodb' ? 'collections' : 'tables and views'} found.` : test.message}</Alert.Description>
        </Alert.Root>
      {:else if testError}
        <Alert.Root variant="destructive"><CircleAlertIcon /><Alert.Title>Could not test</Alert.Title><Alert.Description>{testError}</Alert.Description></Alert.Root>
      {/if}
    </Card.Content>
  </Card.Root>

  <Card.Root>
    <Card.Header class="flex flex-row items-start justify-between gap-3">
      <div class="flex flex-col gap-1.5">
        <Card.Title class="text-sm">{kind === 'mongodb' ? 'Collections' : 'Tables'}</Card.Title>
        <Card.Description>Import one to use it on screens and in the data studio like a built-in table. Its rows stay in your database; roles and row rules still apply.</Card.Description>
      </div>
      <Button variant="outline" size="sm" onclick={loadTables} disabled={loadingTables}>{#if loadingTables}<Spinner />{:else}<RefreshCwIcon />{/if}{tables ? 'Reload' : 'Show tables'}</Button>
    </Card.Header>
    <Card.Content class="flex flex-col gap-0.5">
      {#if tablesError}<p class="text-destructive text-sm">{tablesError}</p>{/if}
      {#if tables}
        {#if !tables.length}<p class="text-muted-foreground text-sm">The database has no tables you may read.</p>{/if}
        {#each tables as t (t.schema + '.' + t.name)}
          {@const Icon = kindIcon[t.kind]}
          <div class="hover:bg-muted/60 flex items-center gap-3 rounded-md px-2 py-1.5">
            <Icon class="text-muted-foreground size-4 shrink-0" />
            <span class="min-w-0 flex-1 truncate font-mono text-xs">{#if t.schema && kind !== 'mongodb'}<span class="text-muted-foreground">{t.schema}.</span>{/if}{t.name}</span>
            {#if t.kind === 'view'}<Badge variant="secondary" class="text-[10px]">view</Badge>{/if}
            {#if t.imported}
              <Button variant="ghost" size="sm" class="h-7 text-xs" onclick={() => navigate(`/apps/${slug}/data/${t.imported}`)}>Imported<ArrowRightIcon /></Button>
            {:else}
              <Button variant="outline" size="sm" class="h-7 text-xs" disabled={importing === t.schema + '.' + t.name} onclick={() => importTable(t)}>{#if importing === t.schema + '.' + t.name}<Spinner />{/if}Import</Button>
            {/if}
          </div>
        {/each}
      {/if}
    </Card.Content>
  </Card.Root>

  {#if own.length}
    <Card.Root>
      <Card.Header><Card.Title class="text-sm">Queries</Card.Title></Card.Header>
      <Card.Content class="flex flex-col gap-1">
        {#each own as q (q.id)}
          <button type="button" class="hover:bg-muted flex items-center gap-3 rounded-md px-2 py-1.5 text-left" onclick={() => onOpen(q)}>
            <span class="w-20 font-mono text-[11px] font-semibold">{kind === 'mongodb' ? q.operation || 'find' : 'SQL'}</span>
            <span class="text-sm font-medium">{q.name}</span>
            <span class="text-muted-foreground min-w-0 flex-1 truncate font-mono text-xs">{kind === 'mongodb' ? q.collection : q.sql.replace(/\s+/g, ' ')}</span>
          </button>
        {/each}
      </Card.Content>
    </Card.Root>
  {/if}
</div>
