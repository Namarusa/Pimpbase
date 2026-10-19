<script lang="ts">
  import * as Avatar from '$lib/components/ui/avatar'
  import { toText } from '../bindings'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const name = $derived(toText(p.name))
  const initials = $derived(name.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]?.toUpperCase()).join('') || '?')
  const size: Record<string, string> = { sm: 'size-8 text-xs', md: 'size-10 text-sm', lg: 'size-14 text-base' }
</script>

<div class="flex items-center gap-3">
  <Avatar.Root class={size[p.size] ?? size.md}>
    {#if p.image}<Avatar.Image src={toText(p.image)} alt={name} />{/if}
    <Avatar.Fallback class="bg-primary/10 text-primary font-medium">{initials}</Avatar.Fallback>
  </Avatar.Root>
  {#if p.showName}
    <div class="flex flex-col">
      <span class="text-sm font-medium">{name}</span>
      {#if p.caption}<span class="text-muted-foreground text-xs">{toText(p.caption)}</span>{/if}
    </div>
  {/if}
</div>
