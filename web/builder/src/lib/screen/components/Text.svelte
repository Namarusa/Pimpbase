<script lang="ts">
  import { toText } from '../bindings'
  import { textAlignClass } from '../styles'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const size: Record<string, string> = { xs: 'text-xs', sm: 'text-sm', base: 'text-base', lg: 'text-lg', xl: 'text-xl' }
  const weight: Record<string, string> = { normal: 'font-normal', medium: 'font-medium', bold: 'font-semibold' }
</script>

<p class={['leading-relaxed whitespace-pre-line', size[p.size] ?? 'text-sm', weight[p.weight], textAlignClass[p.align], p.tone === 'muted' ? 'text-muted-foreground' : p.tone === 'destructive' ? 'text-destructive' : '']}>
  {toText(p.text)}
</p>
