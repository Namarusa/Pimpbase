<script lang="ts">
  import NodeView from './Node.svelte'
  import { getRuntime } from './context.svelte'
  import type { Node } from './types'

  let { node, empty = 'Add components here' }: { node: Node; empty?: string } = $props()
  const rt = getRuntime()
</script>

{#each node.children as c (c.id)}
  <NodeView node={c} />
{/each}
{#if rt.editing && node.children.length === 0}
  <div class="text-muted-foreground border-border/80 flex min-h-14 w-full items-center justify-center rounded-md border border-dashed px-3 py-4 text-xs">{empty}</div>
{/if}
