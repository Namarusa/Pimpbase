<script lang="ts">
  import { Separator } from '$lib/components/ui/separator'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
</script>

{#if p.label}
  <div class="text-muted-foreground flex items-center gap-3 py-1 text-xs font-medium uppercase tracking-wide">
    <Separator class="flex-1" /><span>{p.label}</span><Separator class="flex-1" />
  </div>
{:else}
  <Separator class="my-1" />
{/if}
