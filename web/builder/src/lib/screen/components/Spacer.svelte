<script lang="ts">
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const h: Record<string, string> = { sm: 'h-2', md: 'h-4', lg: 'h-8', xl: 'h-16' }
</script>

<div class={h[p.size] ?? 'h-4'}></div>
