<script lang="ts">
  import * as Card from '$lib/components/ui/card'
  import { Badge } from '$lib/components/ui/badge'
  import { Skeleton } from '$lib/components/ui/skeleton'
  import { getRuntime, getScope } from '../context.svelte'
  import { RowSource } from '../rows.svelte'
  import { runActions } from '../actions'
  import { toText } from '../bindings'
  import { gridColsClass } from '../styles'
  import CellValue from './CellValue.svelte'
  import type { Row } from '../../api'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const scope = getScope()
  const src = new RowSource(rt, node.id, () => ({ props: p, scope: scope() }))
  const clickable = $derived(!!p.onRowClick?.length)
  const extra = $derived((Array.isArray(p.fields) ? p.fields : []).map((n: string) => src.table?.fields.find((f) => f.name === n)).filter(Boolean))

  async function open(row: Row) {
    if (!rt.editing && clickable) await runActions(p.onRowClick, rt, scope(), { row })
  }
</script>

{#if src.loading && !src.rows.length}
  <div class={['grid gap-4', gridColsClass[p.columns] ?? gridColsClass['3']]}>{#each Array(3) as _}<Skeleton class="h-36 rounded-xl" />{/each}</div>
{:else if src.error}
  <p class="text-destructive text-sm">{src.error}</p>
{:else if src.rows.length === 0}
  <p class="text-muted-foreground rounded-xl border border-dashed p-8 text-center text-sm">{toText(p.emptyText) || 'Nothing here yet'}</p>
{:else}
  <div class={['grid gap-4', gridColsClass[p.columns] ?? gridColsClass['3']]}>
    {#each src.rows as row (row._id)}
      <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
      <Card.Root class={['gap-3 overflow-hidden pt-0', clickable ? 'hover:border-primary/50 cursor-pointer transition-colors' : '', p.imageField ? '' : 'pt-6']} onclick={() => open(row)}>
        {#if p.imageField}
          {#if /^(https?:\/\/|\/)/.test(toText(row[p.imageField]))}
            <img src={toText(row[p.imageField])} alt="" class="h-40 w-full object-cover" />
          {:else}<div class="bg-muted h-40 w-full"></div>{/if}
        {/if}
        <Card.Header>
          <Card.Title class="flex items-start justify-between gap-2">
            <span class="truncate">{toText(row[p.titleField]) || `Row ${row._id}`}</span>
            {#if p.badgeField && toText(row[p.badgeField])}<Badge variant="secondary">{toText(row[p.badgeField])}</Badge>{/if}
          </Card.Title>
          {#if p.subtitleField}<Card.Description>{toText(row[p.subtitleField])}</Card.Description>{/if}
        </Card.Header>
        {#if p.bodyField || extra.length}
          <Card.Content class="flex flex-col gap-2 text-sm">
            {#if p.bodyField}<p class="text-muted-foreground line-clamp-3">{toText(row[p.bodyField])}</p>{/if}
            {#each extra as f (f!.id)}
              <div class="flex items-center justify-between gap-3"><span class="text-muted-foreground">{f!.name}</span><span class="truncate text-right"><CellValue field={f} value={row[f!.name]} /></span></div>
            {/each}
          </Card.Content>
        {/if}
      </Card.Root>
    {/each}
  </div>
{/if}
