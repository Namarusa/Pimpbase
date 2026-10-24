<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import { Textarea } from '$lib/components/ui/textarea'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import BracesIcon from '@lucide/svelte/icons/braces'
  import ListIcon from '@lucide/svelte/icons/list'
  import BindingInput from '../designer/BindingInput.svelte'
  import TemplateEditor from '../apis/TemplateEditor.svelte'
  import KeyValueEditor from '../apis/KeyValueEditor.svelte'
  import type { BindingGroup } from '../designer/context'
  import type { IntegrationInput, Pair } from '../api'
  import { flowHelp } from '../flows/steps'
  import InputField from './InputField.svelte'

  // InputField edits one setting of an integration action. In a flow
  // (groups given) values are {{ binding }} templates; on the Integrations
  // tab they are plain values for a try.
  let { input, value, onChange, groups, scope = null, compact = false }: {
    input: IntegrationInput; value: any; onChange: (v: any) => void
    groups?: BindingGroup[]; scope?: Record<string, any> | null; compact?: boolean
  } = $props()
  const id = $derived(`in-${input.key}-${Math.random().toString(36).slice(2, 7)}`)
  const flow = $derived(!!groups)
  const shown = $derived(value ?? (input.kind === 'boolean' ? input.default ?? false : ''))
  const label = $derived(input.label + (input.required ? '' : ''))
  // Items are edited as rows, or given as one list binding.
  const rows = $derived(Array.isArray(value) ? (value as Record<string, any>[]) : [])
  const asBinding = $derived(input.kind === 'items' && typeof value === 'string')
  function setRow(i: number, k: string, v: any) { onChange(rows.map((r, j) => (j === i ? { ...r, [k]: v } : r))) }
  function newRow() {
    const r: Record<string, any> = {}
    for (const f of input.fields ?? []) if (f.default !== undefined) r[f.key] = String(f.default)
    return r
  }
</script>

{#if input.kind === 'boolean'}
  <div class="flex items-start gap-2">
    <Switch {id} checked={value === undefined || value === '' ? !!input.default : value === true || value === 'true'} onCheckedChange={(v) => onChange(v)} />
    <Label for={id} class="flex flex-col items-start gap-0.5 text-xs font-normal">{input.label}{#if input.help}<span class="text-muted-foreground">{input.help}</span>{/if}</Label>
  </div>
{:else}
  <div class="flex flex-col gap-1.5">
    <div class="flex items-center gap-1.5">
      <Label for={id} class={compact ? 'text-[11px]' : 'text-xs'}>{label}</Label>
      {#if input.required}<span class="text-destructive text-[10px] font-medium" title="Required">required</span>{/if}
      {#if input.kind === 'items' && flow}
        <Button variant="ghost" size="sm" class="ml-auto h-6 px-1.5 text-[11px]" onclick={() => onChange(asBinding ? [newRow()] : '')}>
          {#if asBinding}<ListIcon />Edit as rows{:else}<BracesIcon />Use a list{/if}
        </Button>
      {/if}
    </div>
    {#if input.kind === 'select'}
      <Select.Root type="single" value={String(shown || input.default || '')} onValueChange={(v) => onChange(v === '\u0000' ? '' : v)}>
        <Select.Trigger {id} size="sm" class="w-full">{input.options?.find((o) => o.value === (shown || input.default))?.label ?? 'Choose'}</Select.Trigger>
        <Select.Content>
          {#if !input.required}<Select.Item value={'\u0000'} label="None"><span class="text-muted-foreground">None</span></Select.Item>{/if}
          {#each input.options ?? [] as o (o.value)}<Select.Item value={o.value} label={o.label}>{o.label}</Select.Item>{/each}
        </Select.Content>
      </Select.Root>
    {:else if input.kind === 'pairs'}
      {#if flow}
        <KeyValueEditor value={(Array.isArray(value) ? value : []) as Pair[]} groups={groups!} {scope} addLabel="Add" keyPlaceholder="key" onChange={onChange} />
      {:else}
        <Textarea {id} class="min-h-16 font-mono text-xs" value={typeof value === 'string' ? value : value ? JSON.stringify(value) : ''} placeholder={'{"key": "value"}'} oninput={(e) => onChange(e.currentTarget.value)} />
      {/if}
    {:else if input.kind === 'items'}
      {#if asBinding}
        <BindingInput value={value} mono groups={groups!} {scope} help={flowHelp} placeholder={'{{ steps.Found.rows }}'} onChange={onChange} />
        <p class="text-muted-foreground text-xs">A list of objects with {(input.fields ?? []).map((f) => f.key).join(', ')}.</p>
      {:else}
        {#each rows as r, i (i)}
          <div class="bg-muted/30 relative flex flex-col gap-2 rounded-md border p-2 pr-8">
            <Button variant="ghost" size="icon" class="absolute top-1 right-1 size-6" aria-label="Remove" onclick={() => onChange(rows.filter((_, j) => j !== i))}><XIcon class="size-3.5" /></Button>
            <div class="grid grid-cols-2 gap-2">
              {#each input.fields ?? [] as f (f.key)}
                <div class={f.key === 'name' || f.key === 'description' ? 'col-span-2' : ''}>
                  <InputField input={f} value={r[f.key]} {groups} {scope} compact onChange={(v) => setRow(i, f.key, v)} />
                </div>
              {/each}
            </div>
          </div>
        {/each}
        <Button variant="outline" size="sm" class="h-7 self-start border-dashed text-xs" onclick={() => onChange([...rows, newRow()])}><PlusIcon />Add {input.label.toLowerCase().replace(/s$/, '')}</Button>
      {/if}
    {:else if input.kind === 'long' || input.kind === 'json'}
      {#if flow}
        <TemplateEditor value={String(shown)} groups={groups!} {scope} help={flowHelp} minRows={input.kind === 'json' ? 6 : 4} placeholder={input.placeholder} onChange={onChange} />
      {:else}
        <Textarea {id} class={['min-h-20 text-xs', input.kind === 'json' && 'font-mono']} value={String(shown)} placeholder={input.placeholder} oninput={(e) => onChange(e.currentTarget.value)} />
      {/if}
    {:else if flow}
      <BindingInput value={String(shown)} mono={String(shown).includes('{{')} groups={groups!} {scope} help={flowHelp} placeholder={input.placeholder ?? (input.default !== undefined ? String(input.default) : '')} onChange={onChange} />
    {:else}
      <Input {id} class={compact ? 'h-7 text-xs' : 'h-8 text-xs'} value={String(shown)} placeholder={input.placeholder ?? (input.default !== undefined ? String(input.default) : '')} oninput={(e) => onChange(e.currentTarget.value)} />
    {/if}
    {#if input.help && !compact}<p class="text-muted-foreground text-xs">{input.help}</p>{/if}
  </div>
{/if}
