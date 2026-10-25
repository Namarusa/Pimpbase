<script lang="ts">
  import * as Popover from '$lib/components/ui/popover'
  import { Input } from '$lib/components/ui/input'
  import { Textarea } from '$lib/components/ui/textarea'
  import BracesIcon from '@lucide/svelte/icons/braces'
  import type { BindingGroup } from './context'
  import { pipeHelp } from './context'
  import BindingPicker from './BindingPicker.svelte'
  import BindingPreview from './BindingPreview.svelte'

  // BindingInput is a text box with a picker that inserts {{ bindings }}.
  // With sample data (scope), the picker shows values and a line under the
  // box previews what the text gives.
  let { value, onChange, groups, multiline = false, placeholder = '', mono = false, id, help = pipeHelp, class: className = '', scope = null, showPreview = true, list }: {
    value: string; onChange: (v: string) => void; groups: BindingGroup[]
    multiline?: boolean; placeholder?: string; mono?: boolean; id?: string; help?: string; class?: string; scope?: Record<string, any> | null; showPreview?: boolean
    // list names a <datalist> of suggestions.
    list?: string
  } = $props()
  let open = $state(false)
  let el = $state<HTMLInputElement | HTMLTextAreaElement | null>(null)

  function insert(path: string) {
    const v = value ?? ''
    const text = `{{ ${path} }}`
    const at = el?.selectionStart ?? v.length
    const end = el?.selectionEnd ?? at
    onChange(v.slice(0, at) + text + v.slice(end))
    open = false
    requestAnimationFrame(() => { el?.focus(); el?.setSelectionRange(at + text.length, at + text.length) })
  }
</script>

<div class="relative">
  {#if multiline}
    <Textarea {id} bind:ref={el as HTMLTextAreaElement} rows={3} class={['pr-9 text-sm', mono ? 'font-mono text-xs' : '']} {placeholder} value={value ?? ''} oninput={(e) => onChange(e.currentTarget.value)} />
  {:else}
    <Input {id} {list} bind:ref={el as HTMLInputElement} class={['h-8 pr-9 text-sm', mono ? 'font-mono text-xs' : '', className]} {placeholder} value={value ?? ''} oninput={(e) => onChange(e.currentTarget.value)} />
  {/if}
  <Popover.Root bind:open>
    <Popover.Trigger class="text-muted-foreground hover:text-primary hover:bg-muted absolute top-1 right-1 grid size-6 place-items-center rounded" title="Insert a binding">
      <BracesIcon class="size-3.5" />
    </Popover.Trigger>
    <Popover.Content class={[scope ? 'w-[26rem]' : 'w-80', 'p-0']} align="end">
      <BindingPicker {groups} {scope} {help} onPick={insert} />
    </Popover.Content>
  </Popover.Root>
</div>
{#if scope && showPreview}<BindingPreview {value} {scope} />{/if}
