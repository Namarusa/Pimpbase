<script lang="ts">
  import { toast } from 'svelte-sonner'
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import DatabaseZapIcon from '@lucide/svelte/icons/database-zap'
  import JsonTree from '../apis/JsonTree.svelte'
  import type { FlowStep } from '../api'
  import { formatPath } from '../paths'
  import { describe, preview } from '../preview'
  import { getEditor } from './editor.svelte'
  import { loopItems, outputs } from './sample'
  import { walk } from './steps'

  // LoopItemPanel shows what one item of a loop holds, from the sample
  // data, and how steps inside the loop read it.
  let { step }: { step: FlowStep } = $props()
  const ed = getEditor()
  const scope = $derived(ed.scope(step.id))
  const info = $derived(scope ? loopItems(step, scope, outputs(ed.samples.run).firstItems) : { first: undefined, count: undefined, found: false })
  // source is the step that makes the list, when it can get sample data.
  const source = $derived.by(() => {
    const m = /\{\{\s*steps\.([^.\s}|[]+)/.exec(step.inputs.items ?? '')
    let s: FlowStep | undefined
    if (m && ed.flow) walk(ed.flow.steps, (x) => { if (x.name === m[1]) s = x })
    return s && (s.type === 'run_query' || s.type === 'find_rows') ? s : undefined
  })
  let busy = $state(false)
  let error = $state('')
  async function get() {
    if (!source) return
    busy = true
    error = ''
    try { await ed.fetchSample(source) } catch (e: any) { error = e.message } finally { busy = false }
  }
  async function pick(path: string[]) {
    const b = `{{ ${formatPath(path)} }}`
    try { await navigator.clipboard.writeText(b); toast.success(`Copied ${b}`) } catch { toast(b) }
  }
  const isBox = (v: any) => v !== null && typeof v === 'object'
  // notList is a value the sample has that is not a list.
  const items = $derived(preview(step.inputs.items ?? '', scope))
  const notList = $derived(!info.found && items.kind === 'value' && items.found && items.value !== undefined)
</script>

<div class="bg-muted/30 mt-1 flex flex-col gap-2 rounded-lg border p-2.5" data-loop-item>
  <div class="flex items-center gap-2">
    <span class="text-xs font-medium">Each item</span>
    {#if info.count !== undefined}<span class="text-muted-foreground text-[11px]">{info.count} in the sample</span>{/if}
    {#if source}
      <Button variant="outline" size="sm" class="ml-auto h-7 text-xs" disabled={busy} onclick={get}>{#if busy}<Spinner />{:else}<DatabaseZapIcon />{/if}Get sample data</Button>
    {/if}
  </div>
  {#if error}<p class="text-destructive text-xs">{error}</p>{/if}
  <p class="text-muted-foreground text-[11px] leading-snug">
    Steps inside the loop read the current item as <code class="text-foreground font-mono whitespace-nowrap">{'{{ loop.item }}'}</code> and its keys as <code class="text-foreground font-mono whitespace-nowrap">{'{{ loop.item.name }}'}</code>.
    {#if info.found && isBox(info.first)}Click a key to copy its binding.{/if}
  </p>
  {#if info.found}
    <div class="bg-background max-h-64 overflow-auto rounded-md border p-1.5">
      <JsonTree value={info.first} name="loop.item" path={['loop', 'item']} onPick={pick} />
    </div>
  {:else if notList}
    <p class="text-xs text-amber-700 dark:text-amber-400">In the sample, {step.inputs.items} is {describe(items.value)}, not a list. Pick a list, such as the rows of an API step.</p>
  {:else}
    <p class="text-muted-foreground text-xs">No sample yet. {source ? `Get sample data from ${source.name}, or run a test,` : 'Run a test'} to see what each item holds.</p>
  {/if}
</div>
