<script lang="ts">
  import * as Card from '$lib/components/ui/card'
  import { Skeleton } from '$lib/components/ui/skeleton'
  import { getRuntime, getScope } from '../context.svelte'
  import { RowSource, aggregate } from '../rows.svelte'
  import { toText } from '../bindings'
  import { icons } from '../icons'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const scope = getScope()
  const fromRows = $derived(p.mode !== 'value')
  // A stat on its own table is worked out on the server, over every row.
  const src = new RowSource(rt, node.id, () => {
    const own = fromRows ? rt.table(p.table) : undefined
    const op = p.op ?? 'count'
    const agg = own && !own.id.startsWith('query:') ? { values: [op === 'count' || !p.field ? 'count' : `${op}:${p.field}`] } : undefined
    return { props: fromRows ? p : {}, scope: fromRows ? scope() : { ...scope(), data: undefined }, limit: 1000, agg }
  })
  const Icon = $derived(p.icon ? icons[p.icon] : null)

  const raw = $derived(!fromRows ? p.value
    : src.groups ? (src.groups[0]?.values[0] ?? 0)
    : (p.op ?? 'count') === 'count' && !src.fromParent ? src.total : aggregate(src.rows, p.op ?? 'count', p.field))
  const shown = $derived.by(() => {
    const n = Number(raw)
    if (raw === '' || raw === null || raw === undefined || isNaN(n)) return toText(raw)
    switch (p.format) {
      case 'currency': return n.toLocaleString(undefined, { style: 'currency', currency: p.currency || 'USD', maximumFractionDigits: 0 })
      case 'percent': return n.toLocaleString(undefined, { style: 'percent', maximumFractionDigits: 1 })
      case 'decimal': return n.toLocaleString(undefined, { maximumFractionDigits: 2 })
      default: return n.toLocaleString(undefined, { maximumFractionDigits: 0 })
    }
  })
</script>

<Card.Root class="h-full gap-2 py-5">
  <Card.Header class="px-5">
    <Card.Description class="flex items-center justify-between gap-2">
      <span>{toText(p.title)}</span>
      {#if Icon}<span class="bg-primary/10 text-primary grid size-8 place-items-center rounded-lg"><Icon class="size-4" /></span>{/if}
    </Card.Description>
    {#if fromRows && src.loading}
      <Skeleton class="h-8 w-24" />
    {:else}
      <Card.Title class="text-3xl font-semibold tabular-nums">{p.prefix ?? ''}{shown}{p.suffix ?? ''}</Card.Title>
    {/if}
  </Card.Header>
  {#if p.caption}<Card.Content class="text-muted-foreground px-5 text-xs">{toText(p.caption)}</Card.Content>{/if}
</Card.Root>
