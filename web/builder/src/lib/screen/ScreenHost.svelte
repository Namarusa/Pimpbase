<script lang="ts">
  import NodeView from './Node.svelte'
  import { setRuntime, setScope, type Runtime } from './context.svelte'
  import type { Node, Scope } from './types'

  // ScreenHost renders a screen's component tree for the published app and
  // for the builder's canvas.
  let { rt, scope, root }: { rt: Runtime; scope: () => Scope; root: Node } = $props()
  setRuntime(rt)
  setScope(() => ({ ...scope(), device: rt.device }))

  // In the app, device follows the window; the builder sets it.
  let width = $state(0)
  $effect(() => {
    if (!rt.editing && width) rt.device = width < 640 ? 'phone' : width < 1024 ? 'tablet' : 'desktop'
  })
</script>

<svelte:window bind:innerWidth={width} />

<div class="@container min-h-full">
  <NodeView node={root} />
</div>
