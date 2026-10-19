<script lang="ts">
  import { Slider } from '$lib/components/ui/slider'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const f = new FieldBinding(node, () => p, 'number')
  const min = $derived(Number(p.min ?? 0))
  const max = $derived(Number(p.max ?? 100))
</script>

<FieldShell {f} help={p.help}>
  <div class="flex items-center gap-4">
    <Slider type="single" id={f.id} {min} {max} step={Number(p.step) || 1} disabled={f.disabled}
      value={typeof f.value === 'number' ? f.value : min} onValueChange={(v: number) => (f.value = v)} />
    <span class="w-12 text-right text-sm tabular-nums">{f.value ?? '—'}</span>
  </div>
</FieldShell>
