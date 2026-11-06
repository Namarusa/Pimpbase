<script lang="ts">
  import { untrack } from 'svelte'
  import NodeView from './Node.svelte'
  import { setRuntime, setScope, type Runtime } from './context.svelte'
  import { componentScope } from './components'
  import { initVariables } from './variables'
  import { runActions } from './actions'
  import type { Node, Scope } from './types'

  // ScreenHost renders a screen's component tree for the published app and
  // for the builder's canvas.
  let { rt, scope, root }: { rt: Runtime; scope: () => Scope; root: Node } = $props()
  setRuntime(rt)
  // components reads the values other components share, by name.
  const components = $derived(componentScope(root, (id) => rt.sharedValues(id)))
  setScope(() => ({ ...scope(), device: rt.device, components }))

  // The screen's variables start from their first values each time the
  // screen opens, then its "When the screen opens" actions run.
  untrack(() => { rt.variables = root.props.variables ?? []; initVariables(rt.variables, rt.state, scope()) })
  // In the builder, editing a variable's first value shows it at once.
  let firstRun = true
  $effect.pre(() => {
    const vars = JSON.stringify(root.props.variables ?? [])
    if (firstRun) { firstRun = false; return }
    untrack(() => { rt.variables = JSON.parse(vars); initVariables(rt.variables, rt.state, scope()) })
  })
  $effect(() => {
    const actions = untrack(() => root.props.onLoad)
    if (actions?.length) untrack(() => runActions(actions, rt, { ...scope(), device: rt.device, components }))
  })

  // In the app, device follows the window; the builder sets it.
  let width = $state(0)
  $effect(() => {
    if (!rt.editing && width) rt.device = width < 640 ? 'phone' : width < 1024 ? 'tablet' : 'desktop'
  })
</script>

<svelte:window bind:innerWidth={width} />

<div class="@container flex min-h-full flex-col">
  <NodeView node={root} />
</div>
