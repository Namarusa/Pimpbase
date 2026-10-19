<script lang="ts">
  import * as Accordion from '$lib/components/ui/accordion'
  import Children from '../Children.svelte'
  import NodeView from '../Node.svelte'
  import { getRuntime, pathTo } from '../context.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  let open = $state<string[]>(node.children[0] ? [node.children[0].id] : [])

  $effect(() => {
    if (!rt.editing || !rt.selected) return
    const item = node.children.find((c) => pathTo(c, rt.selected!))
    if (item && !open.includes(item.id)) open = [...open, item.id]
  })
</script>

{#if node.children.length === 0}
  <Children {node} empty="Add a Section to this accordion" />
{:else}
  <Accordion.Root type="multiple" bind:value={open} class={['w-full', p.bordered ? 'rounded-lg border px-4' : '']}>
    {#each node.children as item (item.id)}
      <Accordion.Item value={item.id}>
        <Accordion.Trigger>{item.props.title || 'Section'}</Accordion.Trigger>
        <Accordion.Content><NodeView node={item} /></Accordion.Content>
      </Accordion.Item>
    {/each}
  </Accordion.Root>
{/if}
