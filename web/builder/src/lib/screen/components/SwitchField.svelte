<script lang="ts">
  import { Switch } from '$lib/components/ui/switch'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const f = new FieldBinding(node, () => p, 'boolean')
</script>

<FieldShell {f} help={p.help} inline>
  <Switch id={f.id} checked={!!f.value} disabled={f.disabled} onCheckedChange={(v) => { f.value = v; f.touch() }} />
</FieldShell>
