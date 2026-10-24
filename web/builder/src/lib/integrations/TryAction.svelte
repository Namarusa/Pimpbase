<script lang="ts">
  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Spinner } from '$lib/components/ui/spinner'
  import PlayIcon from '@lucide/svelte/icons/play'
  import FlaskConicalIcon from '@lucide/svelte/icons/flask-conical'
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert'
  import { api, type App, type Connection, type Manifest } from '../api'
  import InputField from './InputField.svelte'

  // TryAction runs one action of a connection by hand, with test keys by
  // default, to see what it needs and what it gives back before using it
  // in a flow.
  let { app, conn, manifest }: { app: App; conn: Connection; manifest: Manifest } = $props()
  let key = $state(manifest.actions[0]?.key ?? '')
  const action = $derived(manifest.actions.find((a) => a.key === key))
  let values = $state<Record<string, Record<string, any>>>({})
  const params = $derived(values[key] ?? {})
  let env = $state<'test' | 'live'>('test')
  let running = $state(false)
  let result = $state<{ ok: boolean; output?: any; request?: string; error?: string } | null>(null)
  const hasTest = $derived(Object.values(conn.secrets).some((s) => s.test) || Object.values(conn.testSettings).some(Boolean))

  function set(k: string, v: any) { values[key] = { ...params, [k]: v } }
  function pick(k: string) { key = k; result = null }

  async function run() {
    if (!action) return
    running = true
    try {
      // Plain JSON fields go as values, not text.
      const inputs: Record<string, any> = {}
      for (const [k, v] of Object.entries(params)) {
        const i = action.inputs.find((x) => x.key === k)
        if (v === '' || v === undefined) continue
        if ((i?.kind === 'json' || i?.kind === 'pairs') && typeof v === 'string') {
          try { inputs[k] = JSON.parse(v) } catch { inputs[k] = v }
        } else inputs[k] = v
      }
      const r = await api.tryAction(app.id, conn.id, action.key, inputs, env)
      result = { ok: true, output: r.output, request: r.request }
    } catch (e: any) { result = { ok: false, error: e.message } } finally { running = false }
  }
</script>

<div class="flex min-h-full">
  <nav class="flex w-64 shrink-0 flex-col gap-0.5 border-r p-2">
    {#each manifest.actions as a (a.key)}
      <button type="button" onclick={() => pick(a.key)}
        class={['flex flex-col items-start rounded-md px-2.5 py-1.5 text-left', key === a.key ? 'bg-primary/10 text-primary' : 'hover:bg-muted']}>
        <span class="flex items-center gap-1.5 text-sm font-medium">{a.label}</span>
        <span class="text-muted-foreground line-clamp-2 text-[11px]">{a.description}</span>
      </button>
    {/each}
  </nav>
  {#if action}
    <div class="flex min-w-0 flex-1 flex-col gap-4 p-6">
      <div>
        <h3 class="text-base font-semibold">{action.label}</h3>
        <p class="text-muted-foreground text-sm">{action.description}</p>
      </div>
      <div class="grid max-w-2xl grid-cols-1 gap-3">
        {#each action.inputs as i (i.key)}
          <InputField input={i} value={params[i.key]} onChange={(v) => set(i.key, v)} />
        {/each}
        {#if !action.inputs.length}<p class="text-muted-foreground text-sm">This action needs no settings.</p>{/if}
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <div class="bg-muted inline-flex rounded-md p-0.5 text-xs">
          <button type="button" class={['flex items-center gap-1 rounded px-2.5 py-1', env === 'test' ? 'bg-background shadow-xs' : 'text-muted-foreground']} onclick={() => (env = 'test')}><FlaskConicalIcon class="size-3" />Test keys</button>
          <button type="button" class={['rounded px-2.5 py-1', env === 'live' ? 'bg-background shadow-xs' : 'text-muted-foreground']} onclick={() => (env = 'live')}>Live keys</button>
        </div>
        <Button size="sm" disabled={running} onclick={run}>{#if running}<Spinner />{:else}<PlayIcon />{/if}Try it</Button>
        {#if action.writes && (env === 'live' || !hasTest)}
          <span class="flex items-center gap-1 text-xs text-amber-700 dark:text-amber-400"><TriangleAlertIcon class="size-3.5" />{env === 'live' ? 'This changes real data.' : 'No test keys saved, so this uses the live ones and changes real data.'}</span>
        {/if}
      </div>
      {#if result}
        <div class={['flex flex-col gap-2 rounded-xl border p-3', result.ok ? '' : 'border-destructive/40 bg-destructive/5']}>
          <div class="flex items-center gap-2">
            <Badge variant={result.ok ? 'secondary' : 'destructive'} class="text-[10px]">{result.ok ? 'Done' : 'Failed'}</Badge>
            {#if result.request}<code class="text-muted-foreground truncate font-mono text-[11px]">{result.request}</code>{/if}
          </div>
          {#if result.ok}
            <pre class="bg-muted/50 max-h-96 overflow-auto rounded-md p-2.5 font-mono text-[11px] leading-5">{JSON.stringify(result.output, null, 2)}</pre>
            {#if action.outputs.length}<p class="text-muted-foreground text-xs">Later flow steps read {action.outputs.slice(0, 4).map((o) => `steps.Name.${o.path}`).join(', ')}.</p>{/if}
          {:else}
            <p class="text-destructive text-sm">{result.error}</p>
          {/if}
        </div>
      {/if}
    </div>
  {/if}
</div>
