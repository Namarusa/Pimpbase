<script lang="ts">
  import { Progress } from '$lib/components/ui/progress'
  import { toText } from '../bindings'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const max = $derived(Number(p.max) > 0 ? Number(p.max) : 100)
  const value = $derived(Math.max(0, Math.min(max, Number(p.value) || 0)))
</script>

<div class="flex flex-col gap-2">
  {#if p.label || p.showValue}
    <div class="flex items-center justify-between text-sm">
      <span class="font-medium">{toText(p.label)}</span>
      {#if p.showValue}<span class="text-muted-foreground tabular-nums">{Math.round((value / max) * 100)}%</span>{/if}
    </div>
  {/if}
  <Progress {value} {max} />
</div>
