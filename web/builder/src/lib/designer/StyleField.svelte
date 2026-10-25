<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import * as ToggleGroup from '$lib/components/ui/toggle-group'
  import * as Popover from '$lib/components/ui/popover'
  import { Input } from '$lib/components/ui/input'
  import { Switch } from '$lib/components/ui/switch'
  import BracesIcon from '@lucide/svelte/icons/braces'
  import BindingInput from './BindingInput.svelte'
  import type { BindingGroup } from './context'
  import type { StyleProp } from '../screen/style'

  // StyleField edits one style value. Every kind of value can instead hold
  // text with {{ bindings }}: the braces button switches a choice or a
  // switch to a text box, and colours and sizes are text boxes already.
  let { prop, value, inherited = '', groups, colors, onChange }: {
    prop: StyleProp; value: string; inherited?: string; groups: BindingGroup[]
    // colors are the theme's colours and tokens, offered on the swatch.
    colors: { label: string; value: string; preview: string }[]
    onChange: (v: string) => void
  } = $props()

  const uid = $props.id()
  let typing = $state(false)
  const v = $derived(value ?? '')
  const offList = $derived(!!prop.options && v !== '' && !prop.options.some((o) => o.value === v))
  const asText = $derived(typing || v.includes('{{') || offList || (prop.input === 'switch' && v !== '' && v !== 'true'))
  const segmented = $derived((prop.options?.length ?? 0) <= 4 && (prop.options ?? []).every((o) => o.label.length <= 7))
  const suggestions = $derived(prop.suggestions ?? prop.options?.map((o) => o.value) ?? [])
  const placeholder = $derived(inherited ? `${inherited} (inherited)` : prop.placeholder ? `e.g. ${prop.placeholder}` : '')
  let colorOpen = $state(false)
  // A swatch shows CSS colours; a binding shows a pattern.
  // Theme colours and tokens show their colour too.
  const shown = (x: string) => colors.find((c) => c.value === x.trim())?.preview ?? (x.includes('{{') ? '' : x)
  const swatch = $derived(v ? shown(v) : inherited ? shown(inherited) : '')
  const hex = $derived(/^#[0-9a-f]{6}$/i.test(v) ? v : '#4f46e5')

  function toggleText() {
    if (asText && !v.includes('{{')) { typing = false; if (offList) onChange('') }
    else typing = true
  }
</script>

{#snippet bindButton()}
  <button type="button" class={['hover:bg-muted grid size-7 shrink-0 place-items-center rounded', asText ? 'text-primary' : 'text-muted-foreground']}
    title={asText ? 'Back to the choices' : 'Type a value or a binding'} aria-label={`Bind ${prop.label}`} onclick={toggleText}>
    <BracesIcon class="size-3.5" />
  </button>
{/snippet}

{#if suggestions.length}
  <datalist id={`dl-${uid}`}>{#each suggestions as s (s)}<option value={s}></option>{/each}</datalist>
{/if}

<div class="flex min-w-0 items-center gap-1">
  {#if (prop.input === 'choice' || prop.input === 'switch') && !asText}
    {#if prop.input === 'switch'}
      <Switch checked={v === 'true'} onCheckedChange={(c) => onChange(c ? 'true' : '')} aria-label={prop.label} />
      <span class="flex-1"></span>
    {:else if segmented}
      <ToggleGroup.Root type="single" variant="outline" size="sm" class="h-7 min-w-0 flex-1" value={v} onValueChange={(nv) => onChange(nv ?? '')}>
        {#each prop.options ?? [] as o (o.value)}
          <ToggleGroup.Item value={o.value} class={['h-7 min-w-0 flex-1 px-1 text-[11px]', !v && inherited === o.value ? 'bg-muted/60' : '']} title={o.value}>{o.label}</ToggleGroup.Item>
        {/each}
      </ToggleGroup.Root>
    {:else}
      <Select.Root type="single" value={v} onValueChange={(nv) => onChange(nv === '\u0000' ? '' : nv)}>
        <Select.Trigger size="sm" class="h-7 min-w-0 flex-1 text-xs">
          {#if v}{prop.options?.find((o) => o.value === v)?.label ?? v}{:else}<span class="text-muted-foreground truncate">{inherited ? `${prop.options?.find((o) => o.value === inherited)?.label ?? inherited} (inherited)` : 'Default'}</span>{/if}
        </Select.Trigger>
        <Select.Content>
          <Select.Item value={'\u0000'} label="Default"><span class="text-muted-foreground">Default</span></Select.Item>
          {#each prop.options ?? [] as o (o.value)}<Select.Item value={o.value} label={o.label}>{o.label}<span class="text-muted-foreground ml-auto font-mono text-[10px]">{o.value}</span></Select.Item>{/each}
        </Select.Content>
      </Select.Root>
    {/if}
    {@render bindButton()}
  {:else}
    {#if prop.input === 'color'}
      <Popover.Root bind:open={colorOpen}>
        <Popover.Trigger class="border-input relative size-7 shrink-0 overflow-hidden rounded-md border" title="Pick a colour" aria-label={`Pick ${prop.label}`}>
          <span class="bb-checker absolute inset-0"></span>
          <span class="absolute inset-0" style:background={swatch}></span>
        </Popover.Trigger>
        <Popover.Content class="flex w-64 flex-col gap-2 p-3" align="start">
          <div class="text-muted-foreground text-[11px] font-medium">Theme</div>
          <div class="grid grid-cols-8 gap-1.5">
            {#each colors as c (c.value)}
              <button type="button" class={['ring-offset-background size-6 rounded-md border ring-offset-1', v === c.value ? 'ring-primary ring-2' : '']} style:background={c.preview}
                title={c.label} aria-label={c.label} onclick={() => { onChange(c.value); colorOpen = false }}></button>
            {/each}
          </div>
          <div class="flex items-center gap-2">
            <Input type="color" class="h-7 w-10 cursor-pointer p-0.5" value={hex} oninput={(e) => onChange(e.currentTarget.value)} aria-label="Any colour" />
            <span class="text-muted-foreground text-[11px]">Any colour, or type rgb(), oklch() or a binding.</span>
          </div>
          <button type="button" class="text-muted-foreground hover:text-foreground self-start text-[11px] underline-offset-2 hover:underline" onclick={() => { onChange('transparent'); colorOpen = false }}>Transparent</button>
        </Popover.Content>
      </Popover.Root>
    {/if}
    <div class="min-w-0 flex-1">
      <BindingInput value={v} {groups} {placeholder} onChange={onChange} class="h-7 font-mono text-xs" list={suggestions.length ? `dl-${uid}` : undefined} />
    </div>
    {#if prop.input === 'choice' || prop.input === 'switch'}{@render bindButton()}{/if}
  {/if}
</div>
