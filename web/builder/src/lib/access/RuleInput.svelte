<script lang="ts">
  // RuleInput edits a row rule: a formula that is true for the rows a
  // role may use. It checks the formula as it is typed.
  import { Textarea } from '$lib/components/ui/textarea'
  import { api, type FormulaError, type Table } from '../api'
  import FormulaHelp from '../ai/FormulaHelp.svelte'
  import { isBuilder } from '../session.svelte'

  let { value, app, table, onChange, id }: { value: string; app: string; table: Table; onChange: (v: string) => void; id?: string } = $props()
  let errors = $state<FormulaError[]>([])
  let type = $state('')
  let el = $state<HTMLTextAreaElement | null>(null)
  let timer: ReturnType<typeof setTimeout>

  $effect(() => {
    const src = value
    clearTimeout(timer)
    if (!src.trim()) { errors = []; type = ''; return }
    timer = setTimeout(async () => {
      try {
        const res = await api.checkFormula(app, table.id, src)
        if (src !== value) return
        errors = res.errors
        type = res.errors.length ? '' : res.type
      } catch { /* saving reports it */ }
    }, 250)
  })

  function insert(text: string) {
    const start = el?.selectionStart ?? value.length
    const end = el?.selectionEnd ?? value.length
    onChange(value.slice(0, start) + text + value.slice(end))
    queueMicrotask(() => { el?.focus(); el?.setSelectionRange(start + text.length, start + text.length) })
  }

  const err = $derived(errors[0])
  const hints = [
    { label: 'Current.User.Email', text: 'Current.User.Email' },
    { label: 'Current.User.Groups', text: 'Current.User.Groups' },
    { label: 'Current.User.Role', text: 'Current.User.Role' },
  ]
</script>

<div class="flex flex-col gap-1.5">
  <Textarea {id} bind:ref={el} rows={2} spellcheck={false} class={['min-h-14 font-mono text-xs', err && 'border-destructive']}
    placeholder="[Owner] = Current.User.Email" {value} oninput={(e) => onChange(e.currentTarget.value)} />
  <div class="flex flex-wrap items-center gap-1">
    {#each hints as h (h.label)}
      <button type="button" class="bg-muted hover:bg-muted/70 rounded px-1.5 py-0.5 font-mono text-[10px]" onclick={() => insert(h.text)}>{h.label}</button>
    {/each}
    {#each table.fields.slice(0, 6) as f (f.id)}
      <button type="button" class="hover:bg-muted rounded border px-1.5 py-0.5 font-mono text-[10px]" onclick={() => insert(`[${f.name}]`)}>[{f.name}]</button>
    {/each}
    {#if isBuilder()}<span class="ml-auto"><FormulaHelp {app} table={table.id} purpose="rule" current={value} onUse={onChange} label="AI" /></span>{/if}
  </div>
  {#if err}
    <p class="text-destructive text-xs">{err.message}</p>
  {:else if type && type !== 'boolean' && type !== 'any'}
    <p class="text-destructive text-xs">A row rule must be true or false; this gives {type}.</p>
  {/if}
</div>
