<script lang="ts">
  import * as RadioGroup from '$lib/components/ui/radio-group'
  import { Label } from '$lib/components/ui/label'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const f = new FieldBinding(node, () => p, 'single_select')
</script>

<FieldShell {f} help={p.help}>
  <RadioGroup.Root id={f.id} value={f.value ?? ''} onValueChange={(v) => { f.value = v; f.touch() }} disabled={f.disabled}
    class={p.direction === 'row' ? 'flex flex-wrap gap-5' : 'grid gap-2.5'}>
    {#each f.options as o, i (o)}
      <div class="flex items-center gap-2">
        <RadioGroup.Item value={o} id={`${f.id}-${i}`} aria-invalid={!!f.error} />
        <Label for={`${f.id}-${i}`} class="font-normal">{o}</Label>
      </div>
    {/each}
  </RadioGroup.Root>
</FieldShell>
