<script lang="ts">
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
  import EyeOffIcon from '@lucide/svelte/icons/eye-off'
  import TreeItem from './TreeItem.svelte'
  import { registry } from '../screen/registry'
  import { getDesigner } from './state.svelte'
  import { move } from './tree'
  import { toText } from '../screen/bindings'
  import type { Node } from '../screen/types'

  let { node, depth = 0, drag }: { node: Node; depth?: number; drag: { id: string | null } } = $props()
  const d = getDesigner()
  let open = $state(true)
  let drop = $state<'before' | 'after' | 'inside' | null>(null)
  const def = $derived(registry[node.type])
  const hint = $derived.by(() => {
    const p = node.props
    const t = p.title || p.text || p.label || (p.table ? d.table(p.table)?.name : '') || p.field || ''
    return typeof t === 'string' ? toText(t).slice(0, 40) : ''
  })

  function over(e: DragEvent) {
    if (!drag.id || drag.id === node.id) return
    e.preventDefault()
    const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
    const y = (e.clientY - r.top) / r.height
    drop = node.id === 'root' ? 'inside' : def?.container && y > 0.25 && y < 0.75 ? 'inside' : y < 0.5 ? 'before' : 'after'
  }
  function dropped(e: DragEvent) {
    e.preventDefault()
    const where = drop
    drop = null
    if (!drag.id || !where) return
    const id = drag.id
    d.change((root) => {
      if (!move(root, id, node.id, where)) queueMicrotask(() => (d.saveError = 'That component cannot go there.'))
    })
    drag.id = null
  }
</script>

<div
  role="treeitem" aria-selected={d.selected === node.id} aria-expanded={def?.container ? open : undefined} tabindex="-1"
  draggable={node.id !== 'root'}
  ondragstart={(e) => { drag.id = node.id; e.dataTransfer?.setData('text/plain', node.id) }}
  ondragend={() => { drag.id = null; drop = null }}
  ondragover={over} ondragleave={() => (drop = null)} ondrop={dropped}
  onclick={() => (d.selected = node.id)} onkeydown={(e) => e.key === 'Enter' && (d.selected = node.id)}
  onmouseenter={() => (d.hovered = node.id)} onmouseleave={() => (d.hovered = null)}
  class={['group relative flex h-8 cursor-pointer items-center gap-1.5 rounded-md pr-2 text-sm select-none',
    d.selected === node.id ? 'bg-primary/10 text-primary font-medium' : 'hover:bg-muted',
    drop === 'inside' ? 'ring-primary ring-2 ring-inset' : '']}
  style={`padding-left: ${depth * 14 + 6}px`}>
  {#if drop === 'before'}<span class="bg-primary pointer-events-none absolute inset-x-1 top-0 h-0.5 rounded"></span>{/if}
  {#if drop === 'after'}<span class="bg-primary pointer-events-none absolute inset-x-1 bottom-0 h-0.5 rounded"></span>{/if}
  {#if node.children.length}
    <button type="button" class="text-muted-foreground hover:text-foreground -ml-1 grid size-4 place-items-center" aria-label={open ? 'Collapse' : 'Expand'}
      onclick={(e) => { e.stopPropagation(); open = !open }}>
      <ChevronRightIcon class={['size-3.5 transition-transform', open ? 'rotate-90' : '']} />
    </button>
  {:else}<span class="w-3"></span>{/if}
  {#if def}<def.icon class="size-3.5 shrink-0 opacity-70" />{/if}
  <span class="truncate">{node.id === 'root' ? 'Screen' : node.name || def?.label || node.type}</span>
  {#if hint && node.id !== 'root'}<span class="text-muted-foreground truncate text-xs font-normal">{hint}</span>{/if}
  {#if node.props.visibleIf?.left}<EyeOffIcon class="text-muted-foreground ml-auto size-3 shrink-0" />{/if}
</div>
{#if open}
  {#each node.children as c (c.id)}
    <TreeItem node={c} depth={depth + 1} {drag} />
  {/each}
{/if}
