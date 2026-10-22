<script lang="ts">
  import { toast } from 'svelte-sonner'
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import DatabaseZapIcon from '@lucide/svelte/icons/database-zap'
  import JsonTree from '../apis/JsonTree.svelte'
  import type { FlowStep } from '../api'
  import { formatPath } from '../paths'
  import { getEditor } from './editor.svelte'
  import { outputs } from './sample'

  // SampleBox shows what a data step gave in the latest run, or gets
  // sample data now, so later steps and loops can be written against it.
  let { step }: { step: FlowStep } = $props()
  const ed = getEditor()
  let busy = $state(false)
  let error = $state('')
  const fetched = $derived(ed.samples.fetched[step.id])
  const fromRun = $derived(outputs(ed.samples.run).steps[step.id])
  const sample = $derived(fetched ?? fromRun)
  const truncated = $derived(sample && typeof sample === 'object' && sample.truncated === true && 'preview' in sample)
  const runAt = $derived(ed.samples.run ? new Date(ed.samples.run.startedAt).toLocaleString(undefined, { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }) : '')
  const ready = $derived(step.type === 'run_query' ? !!step.inputs.query : !!step.inputs.table)

  async function get() {
    busy = true
    error = ''
    try { await ed.fetchSample(step) } catch (e: any) { error = e.message } finally { busy = false }
  }
  async function pick(path: string[]) {
    const b = `{{ ${formatPath(path)} }}`
    try { await navigator.clipboard.writeText(b); toast.success(`Copied ${b}`) } catch { toast(b) }
  }
</script>

<div class="flex flex-col gap-2 rounded-lg border p-2.5" data-sample-box>
  <div class="flex items-center gap-2">
    <span class="text-xs font-medium">Sample data</span>
    <span class="text-muted-foreground min-w-0 flex-1 truncate text-[11px]">
      {#if fetched}got just now{:else if fromRun !== undefined}from the run of {runAt}{:else}none yet{/if}
    </span>
    <Button variant="outline" size="sm" class="h-7 text-xs" disabled={!ready || busy} onclick={get}>
      {#if busy}<Spinner />{:else}<DatabaseZapIcon />{/if}Get sample data
    </Button>
  </div>
  {#if error}<p class="text-destructive text-xs">{error}</p>{/if}
  {#if sample !== undefined && !truncated}
    <div class="bg-muted/40 max-h-64 overflow-auto rounded-md p-1.5">
      <JsonTree value={sample} name={`steps.${step.name}`} path={['steps', step.name]} onPick={pick} />
    </div>
    <p class="text-muted-foreground text-[11px]">Click a key to copy its binding. Later steps offer these keys in their binding picker{step.type === 'find_rows' && fetched ? '; the sample is the newest rows, without the filter' : ''}.</p>
  {:else if truncated}
    <p class="text-muted-foreground text-xs">The last run's answer was too large to keep. Get sample data to see it.</p>
  {:else}
    <p class="text-muted-foreground text-xs">{step.type === 'run_query' ? 'Calls the query now with the development values' : 'Reads a few rows of the table'} so the steps after it can show its keys and values. Nothing is changed{step.type === 'run_query' ? ', unless the query itself changes data' : ''}.</p>
  {/if}
</div>
