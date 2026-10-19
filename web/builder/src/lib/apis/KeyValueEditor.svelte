<script lang="ts">
  import { Button } from '$lib/components/ui/button'
  import { Checkbox } from '$lib/components/ui/checkbox'
  import { Input } from '$lib/components/ui/input'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import BindingInput from '../designer/BindingInput.svelte'
  import type { BindingGroup } from '../designer/context'
  import type { Pair } from '../api'
  import { apiHelp } from './bindings'

  // KeyValueEditor edits headers, query strings and form fields. Entries
  // switched off are kept but not sent.
  let { value, onChange, groups, keyLabel = 'Name', valueLabel = 'Value', addLabel = 'Add', keyPlaceholder = '', valuePlaceholder = '' }: {
    value: Pair[]; onChange: (v: Pair[]) => void; groups: BindingGroup[]
    keyLabel?: string; valueLabel?: string; addLabel?: string; keyPlaceholder?: string; valuePlaceholder?: string
  } = $props()
  const set = (i: number, p: Partial<Pair>) => onChange(value.map((x, j) => (j === i ? { ...x, ...p } : x)))
</script>

<div class="flex flex-col gap-1.5">
  {#if value.length}
    <div class="text-muted-foreground grid grid-cols-[1.25rem_minmax(0,2fr)_minmax(0,3fr)_1.75rem] gap-2 px-0.5 text-[11px] font-medium tracking-wide uppercase">
      <span></span><span>{keyLabel}</span><span>{valueLabel}</span><span></span>
    </div>
  {/if}
  {#each value as p, i (i)}
    <div class={['grid grid-cols-[1.25rem_minmax(0,2fr)_minmax(0,3fr)_1.75rem] items-center gap-2', p.off ? 'opacity-50' : '']}>
      <Checkbox checked={!p.off} onCheckedChange={(v) => set(i, { off: !v || undefined })} aria-label="Send this entry" />
      <Input class="h-8 font-mono text-xs" value={p.key} placeholder={keyPlaceholder} oninput={(e) => set(i, { key: e.currentTarget.value })} aria-label={keyLabel} />
      <BindingInput mono value={p.value} {groups} help={apiHelp} placeholder={valuePlaceholder} onChange={(v) => set(i, { value: v })} />
      <Button variant="ghost" size="icon" class="size-7" aria-label="Remove" onclick={() => onChange(value.filter((_, j) => j !== i))}><XIcon class="size-3.5" /></Button>
    </div>
  {/each}
  <Button variant="outline" size="sm" class="h-7 self-start border-dashed text-xs" onclick={() => onChange([...value, { key: '', value: '' }])}><PlusIcon />{addLabel}</Button>
</div>
