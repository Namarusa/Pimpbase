<script lang="ts">
  import { toast } from 'svelte-sonner'
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import { Spinner } from '$lib/components/ui/spinner'
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw'
  import TableIcon from '@lucide/svelte/icons/table'
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check'
  import CircleXIcon from '@lucide/svelte/icons/circle-x'
  import { ApiError, api, type App, type Connection, type IntegrationSync, type Manifest, type SyncConfig } from '../api'
  import { navigate } from '../router.svelte'
  import { timeAgo } from '../format'
  import InputField from './InputField.svelte'

  // SyncCard turns one sync of a connection on or off, says where it
  // writes and how often, and runs it now.
  let { app, conn, manifest, sync, tables, onSaved, onRan }: {
    app: App; conn: Connection; manifest: Manifest; sync: IntegrationSync; tables: { id: string; name: string }[]; onSaved: (c: Connection) => void; onRan: () => void
  } = $props()
  const cfg = $derived<SyncConfig>(conn.syncs.find((s) => s.key === sync.key) ?? { key: sync.key, enabled: false, table: sync.table, every: 60, live: !!sync.events?.length, params: {} })
  const everies = [
    { value: 0, label: 'Only when I run it' }, { value: 15, label: 'Every 15 minutes' }, { value: 60, label: 'Every hour' },
    { value: 360, label: 'Every 6 hours' }, { value: 1440, label: 'Every day' },
  ]
  let running = $state(false)
  let table = $state('')
  // Once it ran, the sync keeps the table's id; show its name.
  const synced = $derived(tables.find((t) => t.id === cfg.table))
  $effect(() => { table = synced?.name ?? (cfg.table || sync.table) })
  const tableRef = () => (synced && table === synced.name ? synced.id : table)

  async function put(p: Partial<SyncConfig>) {
    const next = { ...$state.snapshot(cfg), ...p } as SyncConfig
    const list = [...conn.syncs.filter((s) => s.key !== sync.key), next]
    try { onSaved(await api.updateConnection(app.id, conn.id, { syncs: list })) } catch (e: any) { toast.error(e.message) }
  }
  async function run() {
    if (!cfg.enabled) await put({ enabled: true, table: tableRef() })
    running = true
    try {
      const r = await api.runSync(app.id, conn.id, sync.key)
      toast.success(`${r.lastCount ?? 0} ${sync.label.toLowerCase()} synced`, { description: `${r.created ?? 0} new, ${r.updated ?? 0} changed, ${r.deleted ?? 0} removed` })
    } catch (e: any) {
      toast.error(e instanceof ApiError && e.body?.lastError ? e.body.lastError : e.message)
    } finally {
      running = false
      try { onSaved(await api.connection(app.id, conn.id)) } catch {}
      onRan()
    }
  }
</script>

<div class="bg-card flex flex-col gap-3 rounded-xl border p-4">
  <div class="flex items-start gap-3">
    <Switch checked={cfg.enabled} onCheckedChange={(v) => put({ enabled: v, table: tableRef() })} aria-label={`Sync ${sync.label}`} />
    <div class="min-w-0 flex-1">
      <div class="text-sm font-semibold">{sync.label}</div>
      {#if sync.description}<div class="text-muted-foreground text-xs">{sync.description}</div>{/if}
    </div>
    <Button size="sm" variant="outline" disabled={running} onclick={run}>{#if running}<Spinner />{:else}<RefreshCwIcon />{/if}Sync now</Button>
  </div>
  <div class="grid grid-cols-2 gap-3">
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Into the table</Label>
      <Input class="h-8 text-xs" bind:value={table} onchange={() => put({ table: tableRef() })} />
    </div>
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">How often</Label>
      <Select.Root type="single" value={String(cfg.every)} onValueChange={(v) => put({ every: +v, table: tableRef() })}>
        <Select.Trigger size="sm" class="w-full">{everies.find((e) => e.value === cfg.every)?.label ?? `Every ${cfg.every} minutes`}</Select.Trigger>
        <Select.Content>{#each everies as e (e.value)}<Select.Item value={String(e.value)} label={e.label}>{e.label}</Select.Item>{/each}</Select.Content>
      </Select.Root>
    </div>
    {#each sync.inputs ?? [] as i (i.key)}
      <div class="col-span-2"><InputField input={i} value={cfg.params[i.key]} onChange={(v) => put({ params: { ...cfg.params, [i.key]: String(v ?? '') }, table: tableRef() })} /></div>
    {/each}
  </div>
  {#if sync.events?.length && manifest.webhook}
    <div class="flex items-start gap-2">
      <Switch id={`live-${sync.key}`} checked={cfg.live} onCheckedChange={(v) => put({ live: v, table: tableRef() })} />
      <Label for={`live-${sync.key}`} class="flex flex-col items-start gap-0.5 text-xs font-normal">Update at once from events
        <span class="text-muted-foreground">{sync.events.join(', ')}{sync.deleted?.length ? `; ${sync.deleted.join(', ')} removes the row` : ''}. Needs the webhook set up.</span></Label>
    </div>
  {/if}
  <div class="text-muted-foreground flex flex-wrap items-center gap-x-3 gap-y-1 border-t pt-2 text-xs">
    {#if sync.detect}
      <span>Fields come from what {manifest.name} has.</span>
    {:else}
      <span class="flex items-center gap-1"><TableIcon class="size-3.5" />{sync.fields.map((f) => f.name).join(', ')}</span>
    {/if}
    {#if cfg.lastRun}
      <span class="flex items-center gap-1">
        {#if cfg.lastOk}<CircleCheckIcon class="size-3.5 text-emerald-600" />{:else}<CircleXIcon class="text-destructive size-3.5" />{/if}
        Last run {timeAgo(cfg.lastRun)}{cfg.lastOk ? `: ${cfg.lastCount ?? 0} rows (${cfg.created ?? 0} new, ${cfg.updated ?? 0} changed, ${cfg.deleted ?? 0} removed)` : ''}
      </span>
      {#if cfg.lastOk}<button type="button" class="hover:text-foreground underline" onclick={() => navigate(`/apps/${app.slug}/data${synced ? '/' + synced.id : ''}`)}>Open the table</button>{/if}
    {/if}
  </div>
  {#if cfg.lastRun && !cfg.lastOk && cfg.lastError}<p class="text-destructive text-xs">{cfg.lastError}</p>{/if}
  <p class="text-muted-foreground text-[11px]">Syncs use the live keys.</p>
</div>
