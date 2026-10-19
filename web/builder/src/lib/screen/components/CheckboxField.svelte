<script lang="ts">
  import { Checkbox } from '$lib/components/ui/checkbox'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const f = new FieldBinding(node, () => p, 'boolean')
</script>

<FieldShell {f} help={p.help} inline>
  <Checkbox id={f.id} checked={!!f.value} disabled={f.disabled} aria-invalid={!!f.error} onCheckedChange={(v) => { f.value = !!v; f.touch() }} />
</FieldShell>
