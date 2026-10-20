<script lang="ts">
  // FormulaHelp writes a formula from a description. The formula is
  // compiled against the table before it is offered.
  import * as Popover from '$lib/components/ui/popover'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Spinner } from '$lib/components/ui/spinner'
  import SparklesIcon from '@lucide/svelte/icons/sparkles'
  import { ai, ApiError, type AIFormula } from '../api'
  import NoKey from './NoKey.svelte'

  let { app, table, purpose, current = '', onUse, label = 'Write with AI' }: {
    app: string; table: string; purpose: 'field' | 'filter' | 'rule'; current?: string; onUse: (formula: string) => void; label?: string
  } = $props()

  let open = $state(false)
  let prompt = $state('')
  let busy = $state(false)
  let error = $state('')
  let noKey = $state(false)
  let result = $state<AIFormula | null>(null)
  const examples = { field: 'Days until the due date, or "overdue"', filter: 'Unpaid invoices due this month', rule: 'Rows owned by the person or shared with their team' }

  async function write() {
    busy = true; error = ''; result = null
    try { result = await ai.formula(app, table, prompt, purpose, current) } catch (e: any) {
      if (e instanceof ApiError && e.body?.code === 'ai_not_configured') noKey = true
      else error = e.message
    } finally { busy = false }
  }
</script>

<Popover.Root bind:open>
  <Popover.Trigger>
    {#snippet child({ props })}
      <Button {...props} type="button" variant="outline" size="sm" class="h-7 gap-1 px-2 text-xs"><SparklesIcon class="size-3.5 text-violet-500" />{label}</Button>
    {/snippet}
  </Popover.Trigger>
  <Popover.Content class="flex w-96 flex-col gap-2.5" align="end">
    {#if noKey}
      <NoKey onNavigate={() => (open = false)} />
    {:else}
      <div class="text-sm font-medium">Describe the {purpose === 'rule' ? 'rows the role may use' : purpose === 'filter' ? 'rows you want' : 'value'}</div>
      <div class="flex gap-2">
        <Input bind:value={prompt} placeholder={examples[purpose]} onkeydown={(e) => e.key === 'Enter' && prompt.trim() && !busy && write()} />
        <Button type="button" size="sm" onclick={write} disabled={busy || !prompt.trim()}>{#if busy}<Spinner />{:else}Write{/if}</Button>
      </div>
      {#if result}
        <div class="bg-muted/50 flex flex-col gap-1.5 rounded-md border p-2.5">
          <code class="font-mono text-xs break-all">{result.formula}</code>
          <p class="text-muted-foreground text-xs">{result.explanation} <span class="whitespace-nowrap">Gives {result.type}.</span></p>
          <div class="flex gap-2">
            <Button type="button" size="sm" class="h-7" onclick={() => { onUse(result!.formula); open = false; result = null; prompt = '' }}>Use it</Button>
            <Button type="button" size="sm" variant="ghost" class="h-7" onclick={write} disabled={busy}>Try again</Button>
          </div>
        </div>
      {/if}
      {#if error}<p class="text-destructive text-xs">{error}</p>{/if}
    {/if}
  </Popover.Content>
</Popover.Root>
