<script lang="ts">
  import { api, rowKey, type Row } from '../../api'
  import Children from '../Children.svelte'
  import { extendScope, getRuntime } from '../context.svelte'
  import { Skeleton } from '$lib/components/ui/skeleton'
  import { gapClass } from '../styles'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const table = $derived(rt.table(p.table))
  let row = $state<Row | null>(null)
  let loading = $state(true)
  let error = $state('')

  $effect(() => {
    const t = table
    const id = rowKey(p.rowId)
    void rt.refresh.all
    void rt.refresh[node.id]
    if (!t || !id) {
      row = null
      loading = false
      error = rt.editing || !t ? '' : 'No row was chosen.'
      return
    }
    loading = true
    api.row(rt.appId, t.id, id).then((r) => { row = r; error = '' }).catch((e) => { row = null; error = e.status === 404 ? 'This row no longer exists.' : e.message }).finally(() => (loading = false))
  })
  extendScope(() => ({ row: row ?? {}, rowTable: table }))
</script>

<div class={['flex flex-col', gapClass[p.gap]]}>
  {#if loading && !row}
    <Skeleton class="h-6 w-1/3" /><Skeleton class="h-20 w-full" />
  {:else if error}
    <p class="text-muted-foreground text-sm">{error}</p>
  {:else}
    {#if rt.editing && !row}
      <p class="text-muted-foreground text-xs">Preview: set “Row id”, for example {'{{ url.id }}'}, to load a row. Open the screen with a row id to see it filled.</p>
    {/if}
    <Children {node} empty="Add components that show this row, such as Details or a Form" />
  {/if}
</div>
