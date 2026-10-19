<script lang="ts">
  import { Checkbox } from '$lib/components/ui/checkbox'
  import { Label } from '$lib/components/ui/label'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const f = new FieldBinding(node, () => p, 'multi_select')
  const chosen = $derived<string[]>(Array.isArray(f.value) ? f.value : [])
  function toggle(o: string, on: boolean) {
    f.value = on ? [...chosen, o] : chosen.filter((x) => x !== o)
    f.touch()
  }
</script>

<FieldShell {f} help={p.help}>
  <div id={f.id} class={p.direction === 'row' ? 'flex flex-wrap gap-5' : 'grid gap-2.5'}>
    {#each f.options as o, i (o)}
      <div class="flex items-center gap-2">
        <Checkbox id={`${f.id}-${i}`} checked={chosen.includes(o)} disabled={f.disabled} onCheckedChange={(v) => toggle(o, !!v)} />
        <Label for={`${f.id}-${i}`} class="font-normal">{o}</Label>
      </div>
    {/each}
  </div>
</FieldShell>
