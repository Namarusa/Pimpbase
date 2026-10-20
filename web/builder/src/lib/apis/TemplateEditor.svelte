<script lang="ts">
  import * as Popover from '$lib/components/ui/popover'
  import * as Command from '$lib/components/ui/command'
  import BracesIcon from '@lucide/svelte/icons/braces'
  import type { BindingGroup } from '../designer/context'
  import { apiHelp } from './bindings'

  // TemplateEditor is a code box for request bodies: {{ bindings }} are
  // highlighted, and the picker inserts them at the cursor.
  let { value, onChange, groups, placeholder = '', minRows = 10, id, help = apiHelp }: {
    value: string; onChange: (v: string) => void; groups: BindingGroup[]; placeholder?: string; minRows?: number; id?: string; help?: string
  } = $props()
  let open = $state(false)
  let el = $state<HTMLTextAreaElement | null>(null)
  let back = $state<HTMLPreElement | null>(null)

  type Piece = { text: string; binding: boolean }
  const pieces = $derived.by((): Piece[] => {
    const out: Piece[] = []
    const re = /\{\{[\s\S]*?\}\}/g
    let at = 0
    for (const m of (value ?? '').matchAll(re)) {
      if (m.index! > at) out.push({ text: value.slice(at, m.index), binding: false })
      out.push({ text: m[0], binding: true })
      at = m.index! + m[0].length
    }
    out.push({ text: (value ?? '').slice(at) + '\n', binding: false })
    return out
  })

  function insert(path: string) {
    const v = value ?? ''
    const at = el?.selectionStart ?? v.length
    const end = el?.selectionEnd ?? at
    const text = `{{ ${path} }}`
    onChange(v.slice(0, at) + text + v.slice(end))
    open = false
    requestAnimationFrame(() => { el?.focus(); el?.setSelectionRange(at + text.length, at + text.length) })
  }

  // Tab indents instead of leaving the box.
  function keydown(e: KeyboardEvent) {
    if (e.key !== 'Tab' || e.shiftKey || !el) return
    e.preventDefault()
    const at = el.selectionStart, end = el.selectionEnd
    onChange(value.slice(0, at) + '  ' + value.slice(end))
    requestAnimationFrame(() => el?.setSelectionRange(at + 2, at + 2))
  }
  const sync = () => { if (back && el) { back.scrollTop = el.scrollTop; back.scrollLeft = el.scrollLeft } }
</script>

<div class="bg-background focus-within:border-ring focus-within:ring-ring/50 relative rounded-md border font-mono text-xs leading-5 shadow-xs focus-within:ring-[3px] dark:bg-input/30">
  <pre bind:this={back} aria-hidden="true" class="pointer-events-none absolute inset-0 m-0 overflow-hidden px-3 py-2 pr-10 break-words whitespace-pre-wrap">{#each pieces as p, i (i)}{#if p.binding}<mark class="bg-primary/15 text-primary rounded-sm">{p.text}</mark>{:else}{p.text}{/if}{/each}</pre>
  <textarea bind:this={el} {id} spellcheck="false" {placeholder} value={value ?? ''} rows={minRows}
    oninput={(e) => onChange(e.currentTarget.value)} onscroll={sync} onkeydown={keydown}
    class="placeholder:text-muted-foreground relative block w-full resize-y bg-transparent px-3 py-2 pr-10 break-words whitespace-pre-wrap text-transparent outline-none selection:bg-primary/25 [caret-color:var(--foreground)]"></textarea>
  <Popover.Root bind:open>
    <Popover.Trigger class="text-muted-foreground hover:text-primary hover:bg-muted bg-background absolute top-1.5 right-1.5 grid size-7 place-items-center rounded border" title="Insert a binding">
      <BracesIcon class="size-3.5" />
    </Popover.Trigger>
    <Popover.Content class="w-80 p-0" align="end">
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
