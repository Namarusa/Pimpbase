<script lang="ts">
  import * as Empty from '$lib/components/ui/empty'
  import Children from '../Children.svelte'
  import { toText } from '../bindings'
  import { icons } from '../icons'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const Icon = $derived(icons[p.icon] ?? icons['folder'])
</script>

<Empty.Root class="border">
  <Empty.Header>
    <Empty.Media variant="icon"><Icon /></Empty.Media>
    <Empty.Title>{toText(p.title)}</Empty.Title>
    {#if p.description}<Empty.Description>{toText(p.description)}</Empty.Description>{/if}
  </Empty.Header>
  <Empty.Content><div class="flex flex-wrap justify-center gap-2"><Children {node} empty="Add buttons here" /></div></Empty.Content>
</Empty.Root>
