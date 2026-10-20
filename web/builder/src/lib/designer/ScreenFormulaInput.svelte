<script lang="ts">
  import * as Popover from '$lib/components/ui/popover'
  import * as Command from '$lib/components/ui/command'
  import { Input } from '$lib/components/ui/input'
  import BracesIcon from '@lucide/svelte/icons/braces'
  import SigmaIcon from '@lucide/svelte/icons/sigma'
  import { api, type FunctionInfo } from '../api'
  import { formulaName, formulaProblem } from '../screen/formulas'
  import type { BindingGroup } from './context'

  // ScreenFormulaInput edits a formula over the screen's scope, such as a
  // show-when rule, checking it as it is typed.
  let { value, onChange, groups, placeholder = '', id }: {
    value: string; onChange: (v: string) => void; groups: BindingGroup[]; placeholder?: string; id?: string
  } = $props()
  let el = $state<HTMLInputElement | null>(null)
  let namesOpen = $state(false)
  let fnsOpen = $state(false)
  let fns = $state<FunctionInfo[]>([])

  const problem = $derived(formulaProblem(value ?? ''))
  const nameGroups = $derived([
    ...groups.map((g) => ({ label: g.label, items: g.items.filter((b) => !b.path.includes('|')).map((b) => ({ path: formulaName(b.path), hint: b.hint })) })),
    { label: 'Screen', items: [{ path: 'device', hint: 'desktop, tablet or phone' }] },
  ])

  $effect(() => {
    if (fnsOpen && !fns.length) api.functions().then((f) => (fns = f.filter((x) => x.category !== 'Links'))).catch(() => {})
  })

  function insert(text: string, caretBack = 0) {
    const v = value ?? ''
    const at = el?.selectionStart ?? v.length
    const end = el?.selectionEnd ?? at
    onChange(v.slice(0, at) + text + v.slice(end))
    namesOpen = fnsOpen = false
    queueMicrotask(() => { el?.focus(); const p = at + text.length - caretBack; el?.setSelectionRange(p, p) })
  }
</script>

<div class="flex flex-col gap-1">
  <div class="relative">
    <span class="text-primary pointer-events-none absolute top-1.5 left-2 text-sm font-semibold italic">ƒ</span>
    <Input {id} bind:ref={el} class={['h-8 pr-16 pl-6 font-mono text-xs', problem ? 'border-destructive' : '']} {placeholder} spellcheck={false}
      value={value ?? ''} oninput={(e) => onChange(e.currentTarget.value)} aria-invalid={!!problem} />
    <div class="absolute top-1 right-1 flex gap-0.5">
      <Popover.Root bind:open={namesOpen}>
        <Popover.Trigger class="text-muted-foreground hover:text-primary hover:bg-muted grid size-6 place-items-center rounded" title="Insert a name">
          <BracesIcon class="size-3.5" />
        </Popover.Trigger>
        <Popover.Content class="w-72 p-0" align="end">
          <Command.Root>
            <Command.Input placeholder="Find a name…" />
            <Command.List class="max-h-72">
              <Command.Empty>No names here.</Command.Empty>
              {#each nameGroups as g (g.label)}
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
          </Command.Root>
        </Popover.Content>
      </Popover.Root>
      <Popover.Root bind:open={fnsOpen}>
        <Popover.Trigger class="text-muted-foreground hover:text-primary hover:bg-muted grid size-6 place-items-center rounded" title="Insert a function">
          <SigmaIcon class="size-3.5" />
        </Popover.Trigger>
        <Popover.Content class="w-96 p-0" align="end">
          <Command.Root>
            <Command.Input placeholder="Find a function…" />
            <Command.List class="max-h-80">
              <Command.Empty>{fns.length ? 'No such function.' : 'Loading…'}</Command.Empty>
              {#each [...new Set(fns.map((f) => f.category))] as cat (cat)}
                <Command.Group heading={cat}>
                  {#each fns.filter((f) => f.category === cat) as f (f.name)}
                    <Command.Item value={`${f.name} ${f.description}`} onSelect={() => insert(f.name + '()', 1)} class="flex-col items-start gap-0.5">
                      <span class="font-mono text-xs">{f.signature}</span>
                      <span class="text-muted-foreground text-xs">{f.description}</span>
                    </Command.Item>
                  {/each}
                </Command.Group>
              {/each}
            </Command.List>
          </Command.Root>
        </Popover.Content>
      </Popover.Root>
    </div>
  </div>
  {#if problem}
    <p class="text-destructive text-xs" role="alert">{problem.message}</p>
  {/if}
</div>
