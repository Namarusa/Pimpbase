<script lang="ts">
  import ImageIcon from '@lucide/svelte/icons/image'
  import { toText } from '../bindings'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const src = $derived(toText(p.src))
  const safe = $derived(/^(https?:\/\/|\/|data:image\/)/.test(src))
  const height: Record<string, string> = { auto: 'h-auto', sm: 'h-32', md: 'h-48', lg: 'h-72', xl: 'h-96' }
</script>

{#if safe}
  <img src={src} alt={toText(p.alt)} class={['w-full', height[p.height] ?? 'h-auto', p.fit === 'contain' ? 'object-contain' : 'object-cover', p.rounded ? 'rounded-xl' : '']} />
{:else}
  <div class={['bg-muted text-muted-foreground grid w-full place-items-center rounded-xl', height[p.height] === 'h-auto' || !height[p.height] ? 'h-40' : height[p.height]]}>
    <ImageIcon class="size-8 opacity-50" />
  </div>
{/if}
