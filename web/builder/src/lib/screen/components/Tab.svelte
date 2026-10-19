<script lang="ts">
  import Children from '../Children.svelte'
  import { gapClass } from '../styles'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
</script>

<div class={['flex flex-col', gapClass[p.gap]]}>
  <Children {node} empty="Add components to this tab" />
</div>
