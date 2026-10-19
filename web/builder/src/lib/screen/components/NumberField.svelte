<script lang="ts">
  import { Input } from '$lib/components/ui/input'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const f = new FieldBinding(node, () => p, 'number')
</script>

<FieldShell {f} help={p.help}>
  <div class="relative">
    {#if p.prefix}<span class="text-muted-foreground absolute top-1/2 left-3 -translate-y-1/2 text-sm">{p.prefix}</span>{/if}
    <Input id={f.id} type="number" step={p.step || 'any'} min={p.min} max={p.max} placeholder={p.placeholder} disabled={f.disabled}
      aria-invalid={!!f.error} class={p.prefix ? 'pl-7' : ''}
      value={f.value ?? ''} oninput={(e) => (f.value = e.currentTarget.value === '' ? null : Number(e.currentTarget.value))} onblur={() => f.touch()} />
  </div>
</FieldShell>
