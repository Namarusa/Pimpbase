<script lang="ts">
  import Children from '../Children.svelte'
  import RowScope from './RowScope.svelte'
  import { getRuntime, getScope, shareValues } from '../context.svelte'
  import { RowSource } from '../rows.svelte'
  import { gapClass, gridColsClass } from '../styles'
  import { Skeleton } from '$lib/components/ui/skeleton'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const scope = getScope()
  const src = new RowSource(rt, node.id, () => ({ props: p, scope: scope() }))
  shareValues(node.id, () => ({ rows: src.rows, total: src.total, loading: src.loading }))
  const layout = $derived(p.layout === 'grid' ? ['grid', gridColsClass[p.columns] ?? gridColsClass['3']] : p.layout === 'row' ? ['flex flex-row flex-wrap'] : ['flex flex-col'])
  // The builder shows a few copies; that is enough to design one.
  const rows = $derived(rt.editing ? src.rows.slice(0, 6) : src.rows)
</script>

{#if src.loading && !src.rows.length}
  <div class="flex flex-col gap-2"><Skeleton class="h-10 w-full" /><Skeleton class="h-10 w-full" /></div>
{:else if src.error}
  <p class="text-destructive text-sm">{src.error}</p>
{:else if rows.length === 0}
  {#if rt.editing}
    <Children {node} empty="Add components to repeat for every row" />
  {:else}
    <p class="text-muted-foreground py-6 text-center text-sm">{p.emptyText}</p>
  {/if}
{:else}
  <div class={[...layout, gapClass[p.gap]]}>
    {#each rows as row, i (row._id ?? i)}
      <RowScope {row} index={i} table={src.table}>
        <div class={['flex min-w-0 flex-col', gapClass[p.itemGap ?? 'sm']]}><Children {node} empty="Add components to repeat for every row" /></div>
      </RowScope>
    {/each}
  </div>
{/if}
