<script lang="ts">
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import { getRuntime, getScope, getForm } from '../context.svelte'
  import { runActions } from '../actions'
  import { toText } from '../bindings'
  import { icons } from '../icons'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const scope = getScope()
  const form = getForm()
  let busy = $state(false)
  const Icon = $derived(p.icon ? icons[p.icon] : null)
  const disabled = $derived(p.disabled === true || p.disabled === 'true')

  async function click() {
    busy = true
    try { await runActions(p.actions, rt, scope(), form ? { form: form.values } : {}) } finally { busy = false }
  }
</script>

<Button variant={p.variant ?? 'default'} size={p.size === 'sm' ? 'sm' : p.size === 'lg' ? 'lg' : p.text ? 'default' : 'icon'}
  class={p.fullWidth ? 'w-full' : ''} disabled={busy || disabled} onclick={click} title={p.tooltip || undefined}>
  {#if busy}<Spinner />{:else if Icon}<Icon />{/if}
  {toText(p.text)}
</Button>
