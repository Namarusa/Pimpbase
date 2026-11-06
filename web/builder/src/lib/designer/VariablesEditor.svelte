<script lang="ts">
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import BindingInput from './BindingInput.svelte'
  import type { BindingGroup } from './context'
  import { validName, type Variable } from '../screen/variables'

  // VariablesEditor lists a screen's variables: a name and a first value.
  // Components read them as {{ state.name }}; Update variable actions
  // change them.
  let { value, onChange, groups }: { value: Variable[]; onChange: (v: Variable[]) => void; groups: BindingGroup[] } = $props()
  const list = $derived(Array.isArray(value) ? value : [])
  const set = (i: number, p: Partial<Variable>) => onChange(list.map((v, j) => (j === i ? { ...v, ...p } : v)))
  function add() {
    let n = 1
    while (list.some((v) => v.name === `variable${n}`)) n++
    onChange([...list, { name: `variable${n}`, value: '' }])
  }
  const dup = (name: string) => list.filter((v) => v.name === name).length > 1
</script>

<div class="flex flex-col gap-1.5">
  {#each list as v, i (i)}
    <div class="flex items-start gap-1">
      <div class="w-2/5 shrink-0">
        <Input class={['h-8 font-mono text-xs', !validName(v.name) || dup(v.name) ? 'border-destructive' : '']} value={v.name} aria-label="Variable name"
          oninput={(e) => set(i, { name: e.currentTarget.value.trim() })} />
      </div>
      <div class="min-w-0 flex-1"><BindingInput value={v.value ?? ''} {groups} placeholder="first value" onChange={(nv) => set(i, { value: nv })} /></div>
      <Button variant="ghost" size="icon" class="size-8 shrink-0" aria-label="Remove variable" onclick={() => onChange(list.filter((_, j) => j !== i))}><XIcon class="size-3" /></Button>
    </div>
    {#if !validName(v.name)}<p class="text-destructive text-[11px]">Use letters, digits and _, starting with a letter.</p>
    {:else if dup(v.name)}<p class="text-destructive text-[11px]">Another variable has this name.</p>{/if}
  {/each}
  <Button variant="outline" size="sm" class="h-8 w-full border-dashed" onclick={add}><PlusIcon />Add variable</Button>
  <p class="text-muted-foreground text-xs">Read one anywhere on the screen as <code class="font-mono">{`{{ state.${list[0]?.name || 'name'} }}`}</code>; change it with the <em>Update variable</em> action. A data provider or API query that reads it reloads when it changes.</p>
</div>
