<script lang="ts">
  import * as Popover from '$lib/components/ui/popover'
  import * as Command from '$lib/components/ui/command'
  import { Input } from '$lib/components/ui/input'
  import { Textarea } from '$lib/components/ui/textarea'
  import BracesIcon from '@lucide/svelte/icons/braces'
  import type { BindingGroup } from './context'
  import { pipeHelp } from './context'

  // BindingInput is a text box with a picker that inserts {{ bindings }}.
  let { value, onChange, groups, multiline = false, placeholder = '', mono = false, id, help = pipeHelp, class: className = '' }: {
    value: string; onChange: (v: string) => void; groups: BindingGroup[]
    multiline?: boolean; placeholder?: string; mono?: boolean; id?: string; help?: string; class?: string
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
  }
</script>

<div class="relative">
  {#if multiline}
    <Textarea {id} bind:ref={el as HTMLTextAreaElement} rows={3} class={['pr-9 text-sm', mono ? 'font-mono text-xs' : '']} {placeholder} value={value ?? ''} oninput={(e) => onChange(e.currentTarget.value)} />
  {:else}
    <Input {id} bind:ref={el as HTMLInputElement} class={['h-8 pr-9 text-sm', mono ? 'font-mono text-xs' : '', className]} {placeholder} value={value ?? ''} oninput={(e) => onChange(e.currentTarget.value)} />
  {/if}
  <Popover.Root bind:open>
    <Popover.Trigger class="text-muted-foreground hover:text-primary hover:bg-muted absolute top-1 right-1 grid size-6 place-items-center rounded" title="Insert a binding">
      <BracesIcon class="size-3.5" />
    </Popover.Trigger>
    <Popover.Content class="w-72 p-0" align="end">
      <Command.Root>
        <Command.Input placeholder="Find a binding…" />
        <Command.List class="max-h-72">
          <Command.Empty>No bindings here.</Command.Empty>
          {#each groups as g}
            <Command.Group heading={g.label}>
              {#each g.items as b (b.path)}
                <Command.Item value={b.path} onSelect={() => insert(b.path)} class="justify-between">
                  <span class="truncate font-mono text-xs">{b.path}</span>
                  {#if b.hint}<span class="text-muted-foreground text-xs">{b.hint}</span>{/if}
                </Command.Item>
              {/each}
            </Command.Group>
          {/each}
        </Command.List>
        <p class="text-muted-foreground border-t p-2 text-xs">{help}</p>
      </Command.Root>
    </Popover.Content>
  </Popover.Root>
</div>
