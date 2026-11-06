<script lang="ts">
  import { Input } from '$lib/components/ui/input'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const f = new FieldBinding(node, () => p, 'text')
</script>

<FieldShell {f} help={p.help}>
  <Input id={f.id} bind:ref={f.el} type={p.inputType || 'text'} placeholder={p.placeholder} disabled={f.disabled} aria-invalid={!!f.error}
    value={f.value ?? ''} oninput={(e) => (f.value = e.currentTarget.value)} onblur={() => f.touch()}
    onkeydown={(e) => { if (e.key === 'Enter' && !e.isComposing) { if (p.onEnter?.length) e.preventDefault(); f.enter() } }} />
</FieldShell>
