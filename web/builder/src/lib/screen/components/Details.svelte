<script lang="ts">
  import { getScope } from '../context.svelte'
  import CellValue from './CellValue.svelte'
  import { toText } from '../bindings'
  import type { Field } from '../../api'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const scope = getScope()
  const s = $derived(scope())
  const row = $derived(s.row ?? {})
  const table = $derived(s.rowTable ?? s.data?.table)
  const fields = $derived.by((): Field[] => {
    const all: Field[] = table?.fields ?? []
    const chosen: string[] = Array.isArray(p.fields) ? p.fields : []
    if (!chosen.length) return all
    return chosen.map((n) => all.find((f) => f.name === n)).filter(Boolean) as Field[]
  })
  const cols: Record<string, string> = { '1': 'grid-cols-1', '2': 'grid-cols-1 @md:grid-cols-2', '3': 'grid-cols-1 @md:grid-cols-2 @xl:grid-cols-3' }
</script>

{#if !table}
  <p class="text-muted-foreground rounded-md border border-dashed p-4 text-sm">Put Details inside a row provider, repeater or table row dialog to show a row’s fields.</p>
{:else}
  <div class={p.card !== false ? 'bg-card rounded-xl border p-5' : ''}>
    {#if p.title}<h3 class="mb-4 text-base font-semibold">{toText(p.title)}</h3>{/if}
    <dl class={['grid gap-x-8 gap-y-4', cols[p.columns] ?? cols['2']]}>
      {#each fields as f (f.id)}
        <div class="flex min-w-0 flex-col gap-1">
          <dt class="text-muted-foreground text-xs font-medium tracking-wide uppercase">{f.name}</dt>
          <dd class="text-sm break-words"><CellValue field={f} value={row[f.name]} /></dd>
        </div>
      {/each}
    </dl>
  </div>
{/if}
