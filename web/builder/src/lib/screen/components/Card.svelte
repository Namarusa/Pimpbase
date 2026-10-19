<script lang="ts">
  import * as Card from '$lib/components/ui/card'
  import Children from '../Children.svelte'
  import { gapClass } from '../styles'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
</script>

<Card.Root class="h-full gap-4">
  {#if p.title || p.description}
    <Card.Header>
      {#if p.title}<Card.Title>{p.title}</Card.Title>{/if}
      {#if p.description}<Card.Description>{p.description}</Card.Description>{/if}
    </Card.Header>
  {/if}
  <Card.Content class={['flex flex-col', gapClass[p.gap]]}>
    <Children {node} />
  </Card.Content>
</Card.Root>
