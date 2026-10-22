<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import BindingInput from './BindingInput.svelte'
  import type { BindingGroup } from './context'
  import { buildPath, navTarget, routeParams, type QueryValue, type ScreenRef } from '../screen/navigation'
  import type { Action } from '../screen/types'

  // NavigateEditor sets where a Go to screen action goes: a screen, a value
  // for each :part of its route, and extra values for its query string.
  // A typed address stays possible for anything else.
  let { action, screens, groups, onChange }: {
    action: Action; screens: ScreenRef[]; groups: BindingGroup[]; onChange: (a: Action) => void
  } = $props()

  const address = '\u0000address'
  const target = $derived(navTarget(action, screens))
  const typed = $derived(!target && (!!action.address || !!action.route))
  const screen = $derived(target ? screens.find((s) => s.id === target.screen) : undefined)
  const parts = $derived(screen ? routeParams(screen.route) : [])
  const query = $derived<QueryValue[]>(target?.query ?? [])

  // set writes the screen form, which also replaces an older typed address.
  const set = (t: { screen: string; params: Record<string, string>; query: QueryValue[] }) =>
    onChange({ type: 'navigate', ...Object.fromEntries(Object.entries(action).filter(([k]) => !['route', 'address', 'screen', 'params', 'query'].includes(k))), ...t })

  function choose(v: string) {
    if (v === address) {
      const text = screen ? buildPath(screen.route, target!.params, target!.query, (s) => s) : action.route ?? ''
      const { screen: _s, params: _p, query: _q, ...rest } = action
      onChange({ ...rest, address: true, route: text })
      return
    }
    const s = screens.find((x) => x.id === v)
    if (!s) return
    // Values for parts both routes have carry over.
    const old = target?.params ?? {}
    const params = Object.fromEntries(routeParams(s.route).map((p) => [p, old[p] ?? (p === 'id' ? '{{ row._id }}' : '')]))
    set({ screen: s.id, params, query })
  }
  const setParam = (p: string, v: string) => target && set({ ...target, params: { ...target.params, [p]: v } })
  const setQuery = (q: QueryValue[]) => target && set({ ...target, query: q })
  const triggerLabel = $derived(typed ? 'Another address' : screen ? (screen.name || screen.route) : target ? 'A deleted screen' : 'Choose a screen')
</script>

<div class="flex flex-col gap-2">
  <Select.Root type="single" value={typed ? address : target?.screen ?? ''} onValueChange={choose}>
    <Select.Trigger size="sm" class="w-full">{triggerLabel}</Select.Trigger>
    <Select.Content>
      {#each screens as s (s.id)}
        <Select.Item value={s.id} label={s.name || s.route}>{s.name || s.route}<span class="text-muted-foreground ml-auto font-mono text-[10px]">{s.route}</span></Select.Item>
      {/each}
      <Select.Separator />
      <Select.Item value={address} label="Another address">Another address…</Select.Item>
    </Select.Content>
  </Select.Root>

  {#if typed}
    <BindingInput value={action.route ?? ''} {groups} placeholder={'/orders/{{ row._id }}?tab=notes'} mono onChange={(v) => onChange({ ...action, route: v })} />
    <p class="text-muted-foreground text-[11px] leading-snug">An address in this app, or a screen's route with its values written in.</p>
  {:else if screen && target}
    {@const reads = [...parts.map((p) => `url.${p}`), ...query.filter((q) => q.key).map((q) => `query.${q.key}`)]}
    {#if parts.length}
      <div class="flex flex-col gap-1">
        <Label class="text-muted-foreground text-xs font-normal">Route values</Label>
        {#each parts as p (p)}
          <div class="flex items-center gap-1">
            <span class="w-24 shrink-0 truncate font-mono text-xs" title={`Read on ${screen.name} as {{ url.${p} }}`}>:{p}</span>
            <div class="min-w-0 flex-1"><BindingInput value={target.params[p] ?? ''} {groups} placeholder={p === 'id' ? '{{ row._id }}' : 'value'} onChange={(v) => setParam(p, v)} /></div>
          </div>
        {/each}
      </div>
    {/if}
    <div class="flex flex-col gap-1">
      <Label class="text-muted-foreground text-xs font-normal">Extra values (query string)</Label>
      {#each query as q, k (k)}
        <div class="flex items-center gap-1">
          <Input class="h-8 w-24 shrink-0 font-mono text-xs" value={q.key} placeholder="name" aria-label="Value name"
            oninput={(e) => setQuery(query.map((x, m) => (m === k ? { ...x, key: e.currentTarget.value.replace(/[^\w-]/g, '') } : x)))} />
          <div class="min-w-0 flex-1"><BindingInput value={q.value} {groups} placeholder="value" onChange={(v) => setQuery(query.map((x, m) => (m === k ? { ...x, value: v } : x)))} /></div>
          <Button variant="ghost" size="icon" class="size-6 shrink-0" aria-label="Remove value" onclick={() => setQuery(query.filter((_, m) => m !== k))}><XIcon class="size-3" /></Button>
        </div>
      {/each}
      <Button variant="outline" size="sm" class="h-7 self-start text-xs" onclick={() => setQuery([...query, { key: '', value: '' }])}><PlusIcon />Add value</Button>
    </div>
    {#if reads.length}
      <p class="text-muted-foreground text-[11px] leading-snug">{screen.name || 'That screen'} reads {#each reads as r, i (r)}{i ? ', ' : ''}<code class="text-foreground font-mono">{`{{ ${r} }}`}</code>{/each}.</p>
    {/if}
  {:else if target}
    <p class="text-destructive text-xs">The screen this went to was deleted. Choose another.</p>
  {/if}
</div>
