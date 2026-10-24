<script lang="ts">
  import { onDestroy } from 'svelte'
  import * as Empty from '$lib/components/ui/empty'
  import { Badge } from '$lib/components/ui/badge'
  import { Button } from '$lib/components/ui/button'
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check'
  import CircleXIcon from '@lucide/svelte/icons/circle-x'
  import CircleDashedIcon from '@lucide/svelte/icons/circle-dashed'
  import LoaderCircleIcon from '@lucide/svelte/icons/loader-circle'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw'
  import ActivityIcon from '@lucide/svelte/icons/activity'
  import JsonTree from '../apis/JsonTree.svelte'
  import RunEmail from './RunEmail.svelte'
  import { api, type FlowRun, type StepLog } from '../api'
  import { getEditor } from './editor.svelte'
  import { stepInfo, triggerInfo } from './steps'

  // RunsView is a flow's run history: the latest runs, and the log of the
  // chosen one, step by step.
  let { selected = $bindable(''), reloadKey = 0 }: { selected?: string; reloadKey?: number } = $props()
  const ed = getEditor()
  let runs = $state<FlowRun[]>([])
  let run = $state<FlowRun | null>(null)
  let open = $state<Record<number, boolean>>({})
  let loading = $state(true)

  async function load() {
    if (!ed.flow) return
    try {
      runs = await api.flowRuns(ed.flow.appId, ed.flow.id)
      if (!selected && runs[0]) selected = runs[0].id
      if (selected) {
        const r = await api.flowRun(ed.flow.appId, selected)
        if (r.id !== run?.id) open = Object.fromEntries(r.steps.map((s, i) => [i, s.status === 'failed']))
        run = r
      }
    } catch { /* the list shows what it has */ } finally { loading = false }
  }
  $effect(() => { void reloadKey; void selected; load() })
  // Running flows refresh by themselves.
  const poll = setInterval(() => { if (runs.some((r) => r.status === 'running') || run?.status === 'running') load() }, 1500)
  onDestroy(() => clearInterval(poll))

  // Show how the chosen run went on the canvas too.
  $effect(() => {
    const out: Record<string, 'ok' | 'failed' | 'skipped'> = {}
    for (const s of run?.steps ?? []) if (s.stepId && (out[s.stepId] !== 'failed')) out[s.stepId] = s.status
    ed.lastRun = out
  })
  onDestroy(() => (ed.lastRun = {}))

  const when = (iso: string) => {
    const d = new Date(iso)
    const today = new Date().toDateString() === d.toDateString()
    return today ? d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : d.toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' })
  }
  const took = (ms: number) => (ms < 1000 ? `${ms} ms` : ms < 60000 ? `${(ms / 1000).toFixed(1)} s` : `${Math.round(ms / 60000)} min`)
  const has = (v: any) => v !== undefined && v !== null && !(typeof v === 'object' && Object.keys(v).length === 0)
  const statusIcon = { ok: CircleCheckIcon, failed: CircleXIcon, skipped: CircleDashedIcon }
  const statusColor = { ok: 'text-emerald-600 dark:text-emerald-400', failed: 'text-destructive', skipped: 'text-muted-foreground' }
  const kind = (k: string) => (k === 'test' ? 'Test' : triggerInfo(k).label)
</script>

<div class="flex h-full min-h-0">
  <div class="bg-card/60 flex w-72 shrink-0 flex-col border-r">
    <div class="flex items-center justify-between border-b px-3 py-2">
      <span class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">Runs</span>
      <Button variant="ghost" size="icon" class="size-7" aria-label="Refresh" onclick={load}><RefreshCwIcon class="size-3.5" /></Button>
    </div>
    <div class="flex-1 overflow-y-auto p-1.5">
      {#each runs as r (r.id)}
        <button type="button" onclick={() => (selected = r.id)}
          class={['flex w-full items-center gap-2.5 rounded-md px-2 py-2 text-left', selected === r.id ? 'bg-primary/10' : 'hover:bg-muted']}>
          {#if r.status === 'running'}<LoaderCircleIcon class="text-primary size-4 shrink-0 animate-spin" />
          {:else if r.status === 'succeeded'}<CircleCheckIcon class="size-4 shrink-0 text-emerald-600 dark:text-emerald-400" />
          {:else}<CircleXIcon class="text-destructive size-4 shrink-0" />{/if}
          <span class="flex min-w-0 flex-1 flex-col">
            <span class="text-sm font-medium">{when(r.startedAt)}</span>
            <span class="text-muted-foreground truncate text-xs">{kind(r.triggerKind)}{r.status !== 'running' ? ` · ${took(r.durationMs)}` : ' · running'}</span>
          </span>
          {#if r.test}<Badge variant="outline" class="h-5 px-1.5 text-[10px]">test</Badge>{/if}
        </button>
      {:else}
        {#if !loading}<p class="text-muted-foreground p-3 text-xs">No runs yet. Switch the flow on, or press Test.</p>{/if}
      {/each}
    </div>
  </div>

  <div class="min-w-0 flex-1 overflow-y-auto">
    {#if run}
      <div class="mx-auto flex max-w-3xl flex-col gap-4 p-6">
        <div class="flex flex-wrap items-center gap-3">
          {#if run.status === 'running'}<Badge class="gap-1"><LoaderCircleIcon class="size-3 animate-spin" />Running</Badge>
          {:else if run.status === 'succeeded'}<Badge class="bg-emerald-600 text-white hover:bg-emerald-600">Succeeded</Badge>
          {:else}<Badge variant="destructive">Failed</Badge>{/if}
          <span class="text-sm">{new Date(run.startedAt).toLocaleString()}</span>
          <span class="text-muted-foreground text-sm">· {kind(run.triggerKind)}{run.test ? ' (test, development values)' : ''}{run.status !== 'running' ? ` · ${took(run.durationMs)}` : ''}</span>
        </div>
        {#if run.error}<div class="border-destructive/40 bg-destructive/5 text-destructive rounded-lg border px-3 py-2 text-sm">{run.error}</div>{/if}

        <details class="bg-card rounded-lg border" open={run.steps.length === 0}>
          <summary class="cursor-pointer px-3 py-2 text-sm font-medium">Trigger data</summary>
          <div class="border-t px-2 py-1.5"><JsonTree value={run.trigger} /></div>
        </details>

        <ol class="flex flex-col gap-1.5">
          {#each run.steps as s, i (i)}
            {@const info = stepInfo(s.type)}
            {@const Icon = statusIcon[s.status] ?? CircleDashedIcon}
            <li class="bg-card rounded-lg border" style={`margin-left:${s.depth * 20}px`}>
              <button type="button" class="flex w-full items-center gap-2.5 px-3 py-2 text-left" onclick={() => (open[i] = !open[i])} disabled={s.type === 'note'}>
                <Icon class={['size-4 shrink-0', statusColor[s.status]]} />
                {#if info}<span class={['grid size-6 shrink-0 place-items-center rounded-md', info.tone]}><info.icon class="size-3.5" /></span>{/if}
                <span class="min-w-0 flex-1 truncate text-sm"><span class="font-medium">{s.name}</span>{#if info}<span class="text-muted-foreground"> · {info.label}</span>{/if}</span>
                {#if s.item}<Badge variant="secondary" class="h-5 px-1.5 font-mono text-[10px]">item {s.item}</Badge>{/if}
                {#if s.type !== 'note'}<span class="text-muted-foreground w-14 text-right text-xs tabular-nums">{took(s.durationMs)}</span>
                <ChevronRightIcon class={['text-muted-foreground size-3.5 transition-transform', open[i] ? 'rotate-90' : '']} />{/if}
              </button>
              {#if open[i]}
                <div class="flex flex-col gap-2 border-t px-3 py-2">
                  {#if s.error}<p class="text-destructive text-xs">{s.error}</p>{/if}
                  {#if s.email}<RunEmail email={s.email} failed={s.status === 'failed'} />{/if}
                  {#if has(s.inputs)}<div><div class="text-muted-foreground mb-0.5 text-[11px] font-semibold tracking-wide uppercase">Inputs</div><JsonTree value={s.inputs} /></div>{/if}
                  {#if has(s.output)}<div><div class="text-muted-foreground mb-0.5 text-[11px] font-semibold tracking-wide uppercase">Result</div><JsonTree value={s.output} /></div>{/if}
                  {#if !s.error && !has(s.inputs) && !has(s.output)}<p class="text-muted-foreground text-xs">Nothing to show.</p>{/if}
                </div>
              {/if}
            </li>
          {/each}
        </ol>
        {#if run.output !== null && run.output !== undefined}
          <div class="bg-card rounded-lg border px-3 py-2">
            <div class="text-muted-foreground mb-0.5 text-[11px] font-semibold tracking-wide uppercase">Returned</div>
            <JsonTree value={run.output} />
          </div>
        {/if}
      </div>
    {:else if !loading}
      <Empty.Root class="h-full">
        <Empty.Header>
          <Empty.Media variant="icon"><ActivityIcon /></Empty.Media>
          <Empty.Title>No runs yet</Empty.Title>
          <Empty.Description>Each time the flow runs, its steps, what they read and what they gave back show here.</Empty.Description>
        </Empty.Header>
      </Empty.Root>
    {/if}
  </div>
</div>
