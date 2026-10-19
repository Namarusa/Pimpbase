<script lang="ts">
  import { getRuntime } from '../context.svelte'
  import { toText } from '../bindings'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const url = $derived(toText(p.url))
  const external = $derived(/^(https?:|mailto:|tel:)/.test(url))
  const href = $derived(external ? url : `/app/${rt.appSlug}${url.startsWith('/') ? url : '/' + url}`)

  function click(e: MouseEvent) {
    if (rt.editing) { e.preventDefault(); return }
    if (!external) { e.preventDefault(); rt.navigate(url) }
  }
</script>

<a {href} onclick={click} target={external && p.newTab ? '_blank' : undefined} rel={external ? 'noopener noreferrer' : undefined}
  class="text-primary text-sm font-medium underline-offset-4 hover:underline">{toText(p.text) || url}</a>
