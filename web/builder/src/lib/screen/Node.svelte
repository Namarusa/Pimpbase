<script lang="ts">
  import { registry } from './registry'
  import { spanClass } from './styles'
  import { getRuntime, getScope } from './context.svelte'
  import { checkCondition, hasBinding, resolve } from './bindings'
  import type { Node } from './types'

  let { node }: { node: Node } = $props()
  const rt = getRuntime()
  const scopeFn = getScope()

  const def = $derived(registry[node.type])
  const scope = $derived(scopeFn())
  // Settings that keep their bindings for the component to resolve later.
  const raw = new Set(['actions', 'condition', 'filter', 'options', 'fields'])
  const p = $derived.by(() => {
    const out: Record<string, any> = { ...def?.defaults(), ...node.props }
    for (const s of def?.settings ?? []) if (!raw.has(s.kind) && hasBinding(out[s.key])) out[s.key] = resolve(out[s.key], scope)
    return out
  })
  const visible = $derived(rt.editing || checkCondition(node.props.visibleIf, scope))
  const wrap = $derived([
    'bb-node relative min-w-0',
    p.grow ? 'flex-1' : '',
    spanClass[p.span] ?? '',
    p.printHide ? 'print:hidden' : '',
    def?.wrapperClass?.(p) ?? '',
    rt.editing ? 'bb-edit-node' : '',
    rt.editing && rt.selected === node.id && node.id !== 'root' ? 'bb-selected' : '',
    rt.editing && rt.hovered === node.id && rt.selected !== node.id ? 'bb-hovered' : '',
    rt.editing && node.props.visibleIf?.left ? 'bb-conditional' : '',
  ].filter(Boolean).join(' '))

  function select(e: MouseEvent) {
    if (!rt.editing) return
    e.stopPropagation()
    rt.select(node.id)
  }
  function hover(e: MouseEvent) {
    if (!rt.editing) return
    e.stopPropagation()
    rt.hovered = node.id
  }
</script>

{#if def && visible}
  {#if rt.editing}
    <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions, a11y_mouse_events_have_key_events -->
    <div class={wrap} data-node={node.id} onclick={select} onmouseover={hover}>
      {#if rt.selected === node.id && node.id !== 'root'}
        <span class="bb-tag">{node.name || def.label}</span>
      {/if}
      <def.component {node} {p} />
    </div>
  {:else}
    <div class={wrap} data-node={node.id}><def.component {node} {p} /></div>
  {/if}
{:else if !def}
  <div class="text-destructive text-sm">Unknown component “{node.type}”</div>
{/if}
