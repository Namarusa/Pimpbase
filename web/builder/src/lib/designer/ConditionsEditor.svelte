<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Switch } from '$lib/components/ui/switch'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import BindingInput from './BindingInput.svelte'
  import ScreenFormulaInput from './ScreenFormulaInput.svelte'
  import { fixedSettings, type SettingCondition } from '../screen/formulas'
  import type { BindingGroup } from './context'
  import type { Setting } from '../screen/types'

  // ConditionsEditor edits the settings a component changes while a
  // formula is true, such as a red badge for late orders or a disabled
  // button for closed ones.
  let { value, onChange, settings, props, groups }: {
    value: SettingCondition[]; onChange: (v: SettingCondition[] | undefined) => void
    settings: Setting[]; props: Record<string, any>; groups: BindingGroup[]
  } = $props()

  const editable = new Set(['text', 'textarea', 'boolean', 'select', 'segmented', 'number', 'color'])
  const choices = $derived(settings.filter((s) => editable.has(s.kind) && !fixedSettings.has(s.key)))
  const byKey = $derived(Object.fromEntries(choices.map((s) => [s.key, s])))

  function update(i: number, c: SettingCondition | null) {
    const next = value.slice()
    if (c) next[i] = c
    else next.splice(i, 1)
    onChange(next.length ? next : undefined)
  }
  const add = () => onChange([...value, { when: '', set: {} }])
  const label = (s: Setting) => (s.section ? `${s.label} · ${s.section}` : s.label)
</script>

<div class="flex flex-col gap-2">
  {#each value as c, i (i)}
    <div class="bg-muted/40 flex flex-col gap-2 rounded-md border p-2">
      <div class="flex items-center justify-between">
        <span class="text-muted-foreground text-xs font-medium">When</span>
        <Button variant="ghost" size="icon" class="size-6" aria-label="Remove this condition" onclick={() => update(i, null)}><XIcon class="size-3.5" /></Button>
      </div>
      <ScreenFormulaInput value={c.when} onChange={(v) => update(i, { ...c, when: v })} {groups} placeholder={'[Due] < TODAY()'} />
      {#each Object.entries(c.set) as [key, v] (key)}
        {@const s = byKey[key]}
        <div class="flex flex-col gap-1">
          <div class="flex items-center justify-between">
            <span class="text-xs">{s ? label(s) : key}</span>
            <Button variant="ghost" size="icon" class="size-6" aria-label={`Stop changing ${s?.label ?? key}`} onclick={() => { const set = { ...c.set }; delete set[key]; update(i, { ...c, set }) }}><XIcon class="size-3" /></Button>
          </div>
          {#if !s || s.kind === 'text' || s.kind === 'textarea'}
            <BindingInput value={v ?? ''} {groups} onChange={(nv) => update(i, { ...c, set: { ...c.set, [key]: nv } })} placeholder={s?.placeholder} />
          {:else if s.kind === 'boolean'}
            <Switch checked={!!v} onCheckedChange={(nv) => update(i, { ...c, set: { ...c.set, [key]: nv } })} aria-label={s.label} />
          {:else if s.kind === 'number'}
            <Input type="number" class="h-8" value={v ?? ''} oninput={(e) => update(i, { ...c, set: { ...c.set, [key]: e.currentTarget.value === '' ? '' : Number(e.currentTarget.value) } })} />
          {:else if s.kind === 'color'}
            <Input type="color" class="h-8 w-16 p-1" value={v || '#4f46e5'} oninput={(e) => update(i, { ...c, set: { ...c.set, [key]: e.currentTarget.value } })} />
          {:else}
            <Select.Root type="single" value={String(v ?? '')} onValueChange={(nv) => update(i, { ...c, set: { ...c.set, [key]: nv } })}>
              <Select.Trigger size="sm" class="w-full">{s.options?.find((o) => o.value === String(v))?.label ?? 'Choose'}</Select.Trigger>
              <Select.Content>{#each s.options ?? [] as o (o.value)}<Select.Item value={o.value} label={o.label}>{o.label}</Select.Item>{/each}</Select.Content>
            </Select.Root>
          {/if}
        </div>
      {/each}
      <Select.Root type="single" value="" onValueChange={(key) => key && update(i, { ...c, set: { ...c.set, [key]: props[key] ?? (byKey[key]?.kind === 'boolean' ? true : '') } })}>
        <Select.Trigger size="sm" class="text-muted-foreground w-full"><PlusIcon />Change a setting</Select.Trigger>
        <Select.Content class="max-h-72">
          {#each choices.filter((s) => !(s.key in c.set)) as s (s.key)}<Select.Item value={s.key} label={s.label}>{label(s)}</Select.Item>{/each}
        </Select.Content>
      </Select.Root>
    </div>
  {/each}
  <Button variant="outline" size="sm" class="h-8 self-start" onclick={add}><PlusIcon />Add a condition</Button>
  {#if value.length > 1}<p class="text-muted-foreground text-xs">When several are true, the lower one wins.</p>{/if}
</div>
