<script lang="ts">
  import { Badge } from '$lib/components/ui/badge'
  import { toText } from '../bindings'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const variant = $derived((['default', 'secondary', 'outline', 'destructive'].includes(p.variant) ? p.variant : 'secondary') as 'default')
</script>

{#if toText(p.text)}<Badge {variant}>{toText(p.text)}</Badge>{/if}
