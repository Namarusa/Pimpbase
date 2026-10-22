<script lang="ts">
  import * as Command from '$lib/components/ui/command'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
  import type { BindingGroup } from './context'
  import { formatPath, parsePath, walk } from '../paths'
  import { describe, kindOf } from '../preview'

  // BindingPicker lists the bindings a text can read as a tree. With
  // sample data (scope), objects and lists open to show their keys and
  // items, and each row shows the value it holds.
  let { groups, scope = null, help, onPick }: {
    groups: BindingGroup[]; scope?: Record<string, any> | null; help: string; onPick: (path: string) => void
  } = $props()

  interface Node { segs: string[]; path: string; label: string; hint?: string; children: Node[] }
  interface Row { node: Node; depth: number; value: any; found: boolean; expandable: boolean; more?: number }

  const sameStart = (a: string[], b: string[]) => a.length < b.length && a.every((x, i) => x.toLowerCase() === b[i].toLowerCase())

  // Each group's items become a tree: an item under another one's path
  // is its child.
  const trees = $derived(groups.map((g) => {
    const nodes: Node[] = []
    const all: Node[] = []
    const items = g.items.map((it, i) => ({ it, i, segs: parsePath(it.path) ?? [it.path] }))
    for (const { it, segs } of [...items].sort((a, b) => a.segs.length - b.segs.length || a.i - b.i)) {
      if (all.some((n) => n.segs.join('\u0000').toLowerCase() === segs.join('\u0000').toLowerCase())) continue
      let parent: Node | undefined
      for (const n of all) if (sameStart(n.segs, segs) && (!parent || n.segs.length > parent.segs.length)) parent = n
      const node: Node = { segs, path: formatPath(segs), label: '', hint: it.hint, children: [] }
      node.label = parent ? formatPath(segs.slice(parent.segs.length)) : node.path
      all.push(node)
      ;(parent ? parent.children : nodes).push(node)
    }
    const order = new Map(items.map((x) => [formatPath(x.segs), x.i]))
    const sort = (list: Node[]) => { list.sort((a, b) => (order.get(a.path) ?? 0) - (order.get(b.path) ?? 0)); list.forEach((n) => sort(n.children)) }
    sort(nodes)
    return { group: g, nodes }
  }))

  const valueOf = (segs: string[]) => (scope ? walk(scope, segs) : { value: undefined, found: false })
  const isBox = (v: any) => v !== null && typeof v === 'object'
  const maxItems = 10

  // childrenOf merges what the sample holds with the listed children.
  function childrenOf(n: Node, value: any, found: boolean): { list: Node[]; more: number } {
    if (!found || !isBox(value)) return { list: n.children, more: 0 }
    const keys = Array.isArray(value) ? value.slice(0, maxItems).map((_, i) => String(i)) : Object.keys(value)
    const list: Node[] = keys.map((k) => {
      const segs = [...n.segs, k]
      const listed = n.children.find((c) => c.segs.length === segs.length && c.segs[segs.length - 1].toLowerCase() === k.toLowerCase())
      return { segs, path: formatPath(segs), label: Array.isArray(value) ? `[${k}]` : formatPath([k]), hint: listed?.hint, children: listed?.children ?? n.children.filter((c) => c.segs.length > segs.length && sameStart(segs, c.segs)) }
    })
    // Listed children the sample lacks stay at the end.
    for (const c of n.children) if (!list.some((x) => x.segs.length <= c.segs.length && (x.segs.length === c.segs.length ? x.path.toLowerCase() === c.path.toLowerCase() : sameStart(x.segs, c.segs)))) list.push(c)
    return { list, more: Array.isArray(value) ? Math.max(0, value.length - maxItems) : 0 }
  }

  let query = $state('')
  let opened = $state<Record<string, boolean>>({})
  let highlighted = $state('')
  const isOpen = (path: string, depth: number, g: BindingGroup) => opened[path] ?? (!!g.open && depth === 0)

  function rowsOf(list: Node[], depth: number, g: BindingGroup, out: Row[]) {
    for (const n of list) {
      const { value, found } = valueOf(n.segs)
      const kids = childrenOf(n, value, found)
      const expandable = kids.list.length > 0
      out.push({ node: n, depth, value, found, expandable })
      if (expandable && isOpen(n.path, depth, g) && depth < 8) {
        rowsOf(kids.list, depth + 1, g, out)
        if (kids.more) out.push({ node: { segs: [], path: `${n.path}\u0000more`, label: `… ${kids.more} more items`, children: [] }, depth: depth + 1, value: undefined, found: false, expandable: false, more: kids.more })
      }
    }
    return out
  }

  // Searching looks through every level (lists by their first item).
  function flatten(list: Node[], out: Row[], depth = 0) {
    for (const n of list) {
      const { value, found } = valueOf(n.segs)
      out.push({ node: { ...n, label: n.path }, depth: 0, value, found, expandable: false })
      if (depth < 5) {
        const kids = childrenOf(n, value, found)
        flatten(Array.isArray(value) ? kids.list.slice(0, 1) : kids.list, out, depth + 1)
      }
    }
    return out
  }

  const shown = $derived.by(() => {
    const q = query.trim().toLowerCase()
    return trees.map(({ group, nodes }) => {
      const rows = q ? flatten(nodes, []).filter((r) => r.node.path.toLowerCase().includes(q)).slice(0, 60) : rowsOf(nodes, 0, group, [])
      return { group, rows }
    }).filter((g) => g.rows.length)
  })

  function toggle(path: string, depth: number, g: BindingGroup, e?: Event) {
    e?.stopPropagation()
    e?.preventDefault()
    opened[path] = !isOpen(path, depth, g)
  }
  function keydown(e: KeyboardEvent) {
    if (e.key !== 'ArrowRight' && e.key !== 'ArrowLeft') return
    for (const { group, rows } of shown) {
      const r = rows.find((x) => x.node.path === highlighted)
      if (r?.expandable) {
        e.preventDefault()
        opened[r.node.path] = e.key === 'ArrowRight'
      }
    }
  }
  const example = $derived(groups.flatMap((g) => g.items)[0]?.path ?? 'row.Name')
  const inLoops = $derived(groups.some((g) => g.items.some((i) => i.path.startsWith('loop.'))))
  const valueClass = (v: any) => typeof v === 'number' ? 'text-sky-600 dark:text-sky-400' : typeof v === 'boolean' ? 'text-violet-600 dark:text-violet-400' : typeof v === 'string' ? 'text-emerald-700 dark:text-emerald-400' : 'text-muted-foreground'
</script>

<Command.Root shouldFilter={false} bind:value={highlighted} onkeydown={keydown}>
  <Command.Input placeholder="Find a binding…" bind:value={query} />
  <Command.List class="max-h-80">
    <Command.Empty>No bindings here.</Command.Empty>
    {#each shown as { group, rows } (group.label)}
      <Command.Group heading={group.label}>
        {#if group.note && !query}<p class="text-muted-foreground px-2 pb-1 text-[11px] leading-snug">{group.note}</p>{/if}
        {#each rows as r (r.node.path)}
          {#if r.more}
            <div class="text-muted-foreground py-0.5 text-[11px]" style={`padding-left:${r.depth * 14 + 28}px`}>{r.node.label}</div>
          {:else}
            <Command.Item value={r.node.path} onSelect={() => onPick(r.node.path)} class="gap-1 py-1" title={`{{ ${r.node.path} }}`}>
              <span class="flex shrink-0 items-center" style={`padding-left:${r.depth * 14}px`}>
                {#if r.expandable}
                  <button type="button" class="hover:bg-muted-foreground/15 grid size-4 place-items-center rounded" aria-label={isOpen(r.node.path, r.depth, group) ? 'Fold' : 'Unfold'}
                    onpointerdown={(e) => e.stopPropagation()} onclick={(e) => toggle(r.node.path, r.depth, group, e)}>
                    <ChevronRightIcon class={['size-3 transition-transform', isOpen(r.node.path, r.depth, group) ? 'rotate-90' : '']} />
                  </button>
                {:else}<span class="size-4"></span>{/if}
              </span>
              <span class="min-w-0 shrink truncate font-mono text-xs">{r.node.label}</span>
              {#if r.found}
                <span class={['ml-auto min-w-0 max-w-[55%] shrink-0 truncate pl-2 text-right font-mono text-[11px]', valueClass(r.value)]}>{describe(r.value, 40)}</span>
              {:else if r.node.hint}
                <span class="text-muted-foreground ml-auto shrink-0 pl-2 text-[11px]">{r.node.hint}</span>
              {/if}
            </Command.Item>
          {/if}
        {/each}
      </Command.Group>
    {/each}
  </Command.List>
  <details class="group border-t text-xs">
    <summary class="text-muted-foreground hover:text-foreground flex cursor-pointer list-none items-center gap-1 px-2 py-1.5 select-none">
      <ChevronRightIcon class="size-3 transition-transform group-open:rotate-90" />How to write a binding
      {#if scope}<span class="ml-auto text-[10px]">values from sample data</span>{/if}
    </summary>
    <div class="text-muted-foreground flex flex-col gap-1 px-3 pb-2 leading-snug">
      <p>A binding is a path in double braces: <code class="text-foreground font-mono">{`{{ ${example} }}`}</code>.</p>
      <p><code class="text-foreground font-mono">.name</code> reads a key of an object.</p>
      <p><code class="text-foreground font-mono">.0</code> or <code class="text-foreground font-mono">[0]</code> reads a list item by position, from 0.</p>
      <p><code class="text-foreground font-mono">["first name"]</code> reads a key with spaces, dots or signs.</p>
      {#if inLoops}<p>Inside a loop, <code class="text-foreground font-mono">loop.item</code> is the current item and <code class="text-foreground font-mono">loop.number</code> counts from 1.</p>{/if}
      <p class="pt-1">{help}</p>
    </div>
  </details>
</Command.Root>
