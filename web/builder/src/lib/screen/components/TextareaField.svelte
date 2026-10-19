<script lang="ts">
  import { Textarea } from '$lib/components/ui/textarea'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const f = new FieldBinding(node, () => p, 'longtext')
</script>

<FieldShell {f} help={p.help}>
  <Textarea id={f.id} rows={Number(p.rows) || 4} placeholder={p.placeholder} disabled={f.disabled} aria-invalid={!!f.error}
    value={f.value ?? ''} oninput={(e) => (f.value = e.currentTarget.value)} onblur={() => f.touch()} />
</FieldShell>
