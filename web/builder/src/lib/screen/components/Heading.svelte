<script lang="ts">
  import { toText } from '../bindings'
  import { textAlignClass } from '../styles'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const size: Record<string, string> = {
    h1: 'text-3xl font-bold tracking-tight',
    h2: 'text-2xl font-semibold tracking-tight',
    h3: 'text-xl font-semibold',
    h4: 'text-base font-semibold',
  }
  const tag = $derived(['h1', 'h2', 'h3', 'h4'].includes(p.level) ? p.level : 'h2')
</script>

<svelte:element this={tag} class={[size[tag], textAlignClass[p.align], p.tone === 'muted' ? 'text-muted-foreground' : p.tone === 'primary' ? 'text-primary' : '']}>
  {toText(p.text)}
</svelte:element>
{#if p.subtitle}<p class={['text-muted-foreground mt-1 text-sm', textAlignClass[p.align]]}>{toText(p.subtitle)}</p>{/if}
