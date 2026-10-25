<script lang="ts">
  import * as ToggleGroup from '$lib/components/ui/toggle-group'
  import { Input } from '$lib/components/ui/input'
  import { Button } from '$lib/components/ui/button'
  import MonitorIcon from '@lucide/svelte/icons/monitor'
  import TabletIcon from '@lucide/svelte/icons/tablet'
  import SmartphoneIcon from '@lucide/svelte/icons/smartphone'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
  import XIcon from '@lucide/svelte/icons/x'
  import SearchIcon from '@lucide/svelte/icons/search'
  import BindingInput from './BindingInput.svelte'
  import StyleField from './StyleField.svelte'
  import { getDesigner } from './state.svelte'
  import { findIn } from './tree'
  import type { BindingGroup } from './context'
  import { countSet, hasStyle, setStyle, styleGroups, styleProps, type Style, type StyleLayer, type StyleProp } from '../screen/style'
  import { themeColors } from '../screen/theme'
  import type { ComponentDef, Node } from '../screen/types'

  // StylePanel is the Style tab: every CSS value a component can set, by
  // device and by state, each one bindable.
  let { node, def, groups }: { node: Node; def: ComponentDef; groups: BindingGroup[] } = $props()
  const d = getDesigner()

  type Device = 'base' | 'tablet' | 'phone'
  type State = 'normal' | 'hover' | 'focus' | 'active'
  let device = $state<Device>('base')
  let stateName = $state<State>('normal')
  let search = $state('')
  let opened = $state<Record<string, boolean>>({})
  let sidesOpen = $state<Record<string, boolean>>({})

  const style = $derived((node.props.style ?? {}) as Style)
  const layer = $derived<StyleLayer>(stateName === 'normal' ? device : stateName)
  const values = $derived(style[layer] ?? {})
  // inherited is what applies when this layer leaves a value unset.
  const inherited = $derived<Record<string, string>>(
    layer === 'base' ? {} : layer === 'tablet' ? { ...style.base } : layer === 'phone' ? { ...style.base, ...style.tablet } : { ...style.base },
  )
  const colors = $derived([
    // Tokens that hold a colour; sizes and other values are left out.
    ...(d.app.design.tokens ?? []).filter((t) => /^[A-Za-z][A-Za-z0-9_]*$/.test(t.name) && looksLikeColour(t.value))
      .map((t) => ({ label: `${t.name} · token`, value: `{{ theme.${t.name} }}`, preview: t.value })),
    ...themeColors.map((c) => ({ label: c.label, value: `{{ theme.${c.name} }}`, preview: c.css })),
  ])

  const looksLikeColour = (v: string) => /^(#[0-9a-f]{3,8}$|(rgb|hsl|oklch|oklab|lab|lch|color-mix|light-dark|color)\(|var\(--)/i.test(v.trim()) || /^[a-z]+$/i.test(v.trim())

  // Layout and Grid lay out children, so only containers have them.
  const holdsChildren = $derived(!!def.container || node.id === 'root')
  const shownGroups = $derived(styleGroups.filter((g) => holdsChildren || (g !== 'Layout' && g !== 'Grid')))
  const q = $derived(search.trim().toLowerCase())
  const match = (p: StyleProp) => !q || p.label.toLowerCase().includes(q) || p.key.includes(q) || p.group.toLowerCase().includes(q)
  const defaultOpen = (g: string) => (holdsChildren ? ['Layout', 'Spacing'] : ['Spacing', 'Typography']).includes(g)
  const isOpen = (g: string) => !!q || (opened[g] ?? (defaultOpen(g) || countSet(values, g) > 0))

  // Sides (top, right, bottom, left) share a row of four boxes.
  const sideLabels = new Set(['Top', 'Right', 'Bottom', 'Left'])
  function rowsOf(group: string): (StyleProp | StyleProp[])[] {
    const out: (StyleProp | StyleProp[])[] = []
    for (const p of styleProps.filter((x) => x.group === group && match(x))) {
      const last = out[out.length - 1]
      if (sideLabels.has(p.label)) {
        if (Array.isArray(last) && last.length < 4) last.push(p)
        else out.push([p])
      } else out.push(p)
    }
    return out
  }

  function set(key: string, v: string) {
    const id = node.id
    d.change((root) => {
      const n = findIn(root, id)
      if (!n) return
      const next = setStyle(n.props.style, layer, key, v)
      if (next) n.props.style = next
      else delete n.props.style
    }, true)
  }
  function setCss(v: string) {
    const id = node.id
    d.change((root) => {
      const n = findIn(root, id)
      if (!n) return
      const next: Style = { ...(n.props.style ?? {}), css: v || undefined }
      if (!v) delete next.css
      if (hasStyle(next)) n.props.style = next
      else delete n.props.style
    }, true)
  }
  function clearLayer() {
    const id = node.id
    d.change((root) => {
      const n = findIn(root, id)
      if (!n?.props.style) return
      const next: Style = { ...n.props.style }
      delete next[layer]
      if (hasStyle(next)) n.props.style = next
      else delete n.props.style
    })
  }
  // Picking a device layer shows the canvas at that size.
  function pickDevice(v: Device) {
    device = v
    d.device = v === 'base' ? d.device : v
  }
  const has = (l: StyleLayer) => !!style[l] && Object.keys(style[l]!).length > 0
  const layerName = $derived(stateName !== 'normal' ? { hover: 'when hovered', focus: 'when focused', active: 'while pressed' }[stateName] : device === 'base' ? 'on every device' : device === 'tablet' ? 'on tablets and phones' : 'on phones')
</script>

{#snippet dot(on: boolean)}{#if on}<span class="bg-primary absolute top-0.5 right-0.5 size-1.5 rounded-full"></span>{/if}{/snippet}

{#snippet field(p: StyleProp, compact: boolean)}
  {@const set_ = values[p.key] !== undefined && values[p.key] !== ''}
  <div class={compact ? 'flex min-w-0 flex-col gap-0.5' : 'grid grid-cols-[5.5rem_minmax(0,1fr)] items-center gap-2'}>
    <div class="flex min-w-0 items-center gap-0.5">
      <span class={['truncate text-[11px]', set_ ? 'text-primary font-medium' : 'text-muted-foreground']} title={p.key}>{p.label}</span>
      {#if set_}<button type="button" class="text-muted-foreground hover:text-foreground shrink-0" title="Reset" aria-label={`Reset ${p.label}`} onclick={() => set(p.key, '')}><XIcon class="size-3" /></button>{/if}
    </div>
    <StyleField prop={p} value={values[p.key] ?? ''} inherited={inherited[p.key] ?? ''} {groups} {colors} onChange={(v) => set(p.key, v)} />
  </div>
{/snippet}

<div class="flex flex-col">
  <div class="flex flex-col gap-2 border-b px-4 py-3">
    <div class="grid grid-cols-[3.5rem_minmax(0,1fr)] items-center gap-x-2 gap-y-1.5">
      <span class="text-muted-foreground text-[11px]">Device</span>
      <ToggleGroup.Root type="single" variant="outline" size="sm" class="w-full" value={stateName === 'normal' ? device : ''} onValueChange={(v) => { if (v) { stateName = 'normal'; pickDevice(v as Device) } }} aria-label="Device">
        <ToggleGroup.Item value="base" class="relative flex-1 px-1 text-[11px]" title="Every device"><MonitorIcon />All{@render dot(has('base'))}</ToggleGroup.Item>
        <ToggleGroup.Item value="tablet" class="relative flex-1 px-1 text-[11px]" title="Tablets and phones"><TabletIcon />Tablet{@render dot(has('tablet'))}</ToggleGroup.Item>
        <ToggleGroup.Item value="phone" class="relative flex-1 px-1 text-[11px]" title="Phones"><SmartphoneIcon />Phone{@render dot(has('phone'))}</ToggleGroup.Item>
      </ToggleGroup.Root>
      <span class="text-muted-foreground text-[11px]">State</span>
      <ToggleGroup.Root type="single" variant="outline" size="sm" class="w-full" value={stateName} onValueChange={(v) => v && (stateName = v as State)} aria-label="State">
        <ToggleGroup.Item value="normal" class="flex-1 px-1 text-[11px]">Normal</ToggleGroup.Item>
        <ToggleGroup.Item value="hover" class="relative flex-1 px-1 text-[11px]">Hover{@render dot(has('hover'))}</ToggleGroup.Item>
        <ToggleGroup.Item value="focus" class="relative flex-1 px-1 text-[11px]">Focus{@render dot(has('focus'))}</ToggleGroup.Item>
        <ToggleGroup.Item value="active" class="relative flex-1 px-1 text-[11px]">Pressed{@render dot(has('active'))}</ToggleGroup.Item>
      </ToggleGroup.Root>
    </div>
    <div class="flex items-center gap-2">
      <p class="text-muted-foreground min-w-0 flex-1 text-[11px] leading-snug">Values set here apply <span class="text-foreground font-medium">{layerName}</span>{#if layer === 'phone'}, over the tablet ones{/if}.</p>
      {#if has(layer)}<Button variant="link" size="sm" class="h-auto p-0 text-[11px]" onclick={clearLayer}>Clear</Button>{/if}
    </div>
    <div class="relative">
      <SearchIcon class="text-muted-foreground pointer-events-none absolute top-2 left-2 size-3.5" />
      <Input class="h-7 pl-7 text-xs" placeholder="Find a style, such as radius" bind:value={search} />
    </div>
  </div>

  {#each shownGroups as g (g)}
    {@const rows = rowsOf(g)}
    {#if rows.length}
      {@const n = countSet(values, g)}
      <div class="border-b">
        <button type="button" class="hover:bg-muted/50 flex w-full items-center gap-1.5 px-4 py-2 text-left" aria-expanded={isOpen(g)} onclick={() => (opened[g] = !isOpen(g))}>
          <ChevronRightIcon class={['text-muted-foreground size-3.5 transition-transform', isOpen(g) ? 'rotate-90' : '']} />
          <span class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">{g}</span>
          {#if n}<span class="bg-primary/10 text-primary ml-auto rounded px-1.5 text-[10px] font-medium">{n}</span>{/if}
        </button>
        {#if isOpen(g)}
          <div class="flex flex-col gap-2 px-4 pb-3">
            {#each rows as r, i (i)}
              {#if Array.isArray(r)}
                {@const key = r[0].key}
                {@const any = r.some((p) => values[p.key])}
                {#if q || any || sidesOpen[key] || g === 'Position'}
                  <div class="grid grid-cols-2 gap-x-2 gap-y-1.5 pl-3">
                    {#each r as p (p.key)}{@render field(p, true)}{/each}
                  </div>
                {:else}
                  <button type="button" class="text-muted-foreground hover:text-foreground -mt-1 self-end text-[11px] underline-offset-2 hover:underline" onclick={() => (sidesOpen[key] = true)}>Each side…</button>
                {/if}
              {:else}
                {@render field(r, false)}
              {/if}
            {/each}
          </div>
        {/if}
      </div>
    {/if}
  {/each}

  {#if !q || 'custom css'.includes(q)}
    <div class="flex flex-col gap-1.5 px-4 py-3">
      <div class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">Custom CSS</div>
      <BindingInput multiline mono value={style.css ?? ''} {groups} onChange={setCss} placeholder={'outline: 2px solid {{ theme.primary }};\nscroll-snap-align: start;'} />
      <p class="text-muted-foreground text-[11px] leading-snug">CSS declarations for the component, on every device. Bindings work here too.</p>
    </div>
  {/if}
</div>
