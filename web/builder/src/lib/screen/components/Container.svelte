<script lang="ts">
  import Children from '../Children.svelte'
  import { alignClass, bgClass, gapClass, justifyClass, maxWidthClass, padClass } from '../styles'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
</script>

<div class={[
  'flex',
  p.direction === 'row' ? 'flex-row' : 'flex-col',
  p.wrap && p.direction === 'row' ? 'flex-wrap' : '',
  gapClass[p.gap], padClass[p.padding], alignClass[p.align], justifyClass[p.justify],
  bgClass[p.background], maxWidthClass[p.maxWidth], p.fill ? 'min-h-full' : '',
]}>
  <Children {node} />
</div>
