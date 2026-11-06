<script lang="ts">
  import * as Pagination from '$lib/components/ui/pagination'
  import Children from '../Children.svelte'
  import { extendScope, getRuntime, getScope, shareValues } from '../context.svelte'
  import { RowSource } from '../rows.svelte'
  import { gapClass } from '../styles'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const scope = getScope()
  let page = $state(1)
  const limit = $derived(Number(p.limit) || 50)
  const src = new RowSource(rt, node.id, () => ({ props: p, scope: scope(), offset: (page - 1) * limit, limit }))
  shareValues(node.id, () => ({ rows: src.rows, total: src.total, loading: src.loading }))
  extendScope(() => ({ data: { rows: src.rows, total: src.total, loading: src.loading, table: src.table } }))
</script>

<div class={['flex flex-col', gapClass[p.gap]]}>
  {#if src.error}<p class="text-destructive text-sm">{src.error}</p>{/if}
  <Children {node} empty="Add components that show this data: a table, list, chart or repeater" />
  {#if p.paginate && src.total > limit}
    <Pagination.Root count={src.total} perPage={limit} bind:page>
      {#snippet children({ pages, currentPage })}
        <Pagination.Content>
          <Pagination.Item><Pagination.PrevButton /></Pagination.Item>
          {#each pages as pg (pg.key)}
            {#if pg.type === 'ellipsis'}<Pagination.Item><Pagination.Ellipsis /></Pagination.Item>
            {:else}<Pagination.Item><Pagination.Link page={pg} isActive={currentPage === pg.value}>{pg.value}</Pagination.Link></Pagination.Item>{/if}
          {/each}
          <Pagination.Item><Pagination.NextButton /></Pagination.Item>
        </Pagination.Content>
      {/snippet}
    </Pagination.Root>
  {/if}
</div>
