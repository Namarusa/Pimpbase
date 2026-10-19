<script lang="ts">
  import * as Tabs from '$lib/components/ui/tabs'
  import Children from '../Children.svelte'
  import NodeView from '../Node.svelte'
  import { getRuntime, pathTo } from '../context.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  let value = $state(node.children[0]?.id ?? '')

  // In the builder, show the tab holding the selected component.
  $effect(() => {
    if (!rt.editing || !rt.selected) return
    const tab = node.children.find((c) => pathTo(c, rt.selected!))
    if (tab) value = tab.id
  })
  $effect(() => {
    if (!node.children.some((c) => c.id === value)) value = node.children[0]?.id ?? ''
  })
</script>

{#if node.children.length === 0}
  <Children {node} empty="Add a Tab to these tabs" />
{:else}
  <Tabs.Root bind:value class="w-full">
    <Tabs.List class={p.fullWidth ? 'w-full' : ''}>
      {#each node.children as tab (tab.id)}
        <Tabs.Trigger value={tab.id}>{tab.props.title || 'Tab'}</Tabs.Trigger>
      {/each}
    </Tabs.List>
    {#each node.children as tab (tab.id)}
      <Tabs.Content value={tab.id} class="pt-2">
        <NodeView node={tab} />
      </Tabs.Content>
    {/each}
  </Tabs.Root>
{/if}
