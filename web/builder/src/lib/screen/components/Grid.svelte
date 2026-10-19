<script lang="ts">
  import Children from '../Children.svelte'
  import { gapClass, gridColsClass } from '../styles'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
</script>

<div class={['grid', gridColsClass[p.columns] ?? 'grid-cols-1', gapClass[p.gap]]}>
  <Children {node} />
</div>
