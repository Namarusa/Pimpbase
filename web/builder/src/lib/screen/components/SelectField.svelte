<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const f = new FieldBinding(node, () => p, 'single_select')
  const none = '\u0000none'
</script>

<FieldShell {f} help={p.help}>
  <Select.Root type="single" disabled={f.disabled} value={f.value ?? ''} onValueChange={(v) => { f.value = v === none ? null : v; f.touch() }}>
    <Select.Trigger id={f.id} class="w-full" aria-invalid={!!f.error}>
      {#if f.value}{f.value}{:else}<span class="text-muted-foreground">{p.placeholder || 'Choose…'}</span>{/if}
    </Select.Trigger>
    <Select.Content>
      {#if !f.required}<Select.Item value={none} label="—"><span class="text-muted-foreground">None</span></Select.Item>{/if}
      {#each f.options as o (o)}<Select.Item value={o} label={o}>{o}</Select.Item>{/each}
      {#if f.options.length === 0}<div class="text-muted-foreground px-2 py-1.5 text-sm">No options yet</div>{/if}
    </Select.Content>
  </Select.Root>
</FieldShell>
