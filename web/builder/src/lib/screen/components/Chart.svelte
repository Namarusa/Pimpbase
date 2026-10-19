<script lang="ts">
  import * as Chart from '$lib/components/ui/chart'
  import * as Card from '$lib/components/ui/card'
  import { Skeleton } from '$lib/components/ui/skeleton'
  import { AreaChart, BarChart, LineChart, PieChart } from 'layerchart'
  import { scaleBand, scalePoint } from 'd3-scale'
  import { curveMonotoneX } from 'd3-shape'
  import { getRuntime, getScope } from '../context.svelte'
  import { RowSource, aggregate } from '../rows.svelte'
  import { toText } from '../bindings'
  import type { Row } from '../../api'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const scope = getScope()
  const src = new RowSource(rt, node.id, () => ({ props: p, scope: scope(), limit: 1000 }))

  const colors = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)']
  const kind = $derived(p.chartType ?? 'bar')
  const values = $derived<string[]>(Array.isArray(p.valueFields) && p.valueFields.length ? p.valueFields : [''])
  const opLabel: Record<string, string> = { count: 'Count', sum: 'Total', avg: 'Average', min: 'Minimum', max: 'Maximum' }

  // bucket turns a value into its group label and a key that sorts in
  // time order for dates.
  function bucket(v: any): [string, string] {
    const field = src.table?.fields.find((f) => f.name === p.labelField)
    const isDate = field && (field.type === 'date' || (field.type === 'formula' && field.resultType === 'date'))
    if (isDate && typeof v === 'string' && v) {
      const d = new Date(v)
      switch (p.dateGroup ?? 'month') {
        case 'day': return [d.toISOString().slice(0, 10), d.toLocaleDateString(undefined, { month: 'short', day: 'numeric', timeZone: 'UTC' })]
        case 'year': return [String(d.getUTCFullYear()), String(d.getUTCFullYear())]
        default: return [d.toISOString().slice(0, 7), d.toLocaleDateString(undefined, { month: 'short', year: '2-digit', timeZone: 'UTC' })]
      }
    }
    if (Array.isArray(v)) v = v[0]
    const t = toText(v) || '(blank)'
    return [t, t]
  }

  const points = $derived.by((): Record<string, any>[] => {
    if (!p.labelField) return []
    const groups = new Map<string, { label: string; rows: Row[] }>()
    for (const r of src.rows) {
      const [key, label] = bucket(r[p.labelField])
      if (!groups.has(key)) groups.set(key, { label, rows: [] })
      groups.get(key)!.rows.push(r)
    }
    let out: Record<string, any>[] = [...groups.entries()].map(([key, g], i) => {
      const pt: Record<string, any> = { key, label: g.label, color: colors[i % colors.length] }
      values.forEach((f, j) => (pt[`v${j}`] = Math.round(aggregate(g.rows, p.op ?? 'count', f) * 100) / 100))
      return pt
    })
    if (p.sortBy === 'value') out.sort((a, b) => b.v0 - a.v0)
    else out.sort((a, b) => a.key.localeCompare(b.key, undefined, { numeric: true }))
    const top = Number(p.limitGroups) || 0
    if (top > 0) out = out.slice(0, top)
    return out.map((pt, i) => ({ ...pt, color: colors[i % colors.length] }))
  })

  const series = $derived(values.map((f, j) => ({
    key: `v${j}`,
    label: (p.op ?? 'count') === 'count' ? 'Rows' : `${opLabel[p.op] ?? ''} ${f}`.trim(),
    color: colors[j % colors.length],
  })))
  const config = $derived(Object.fromEntries([
    ...series.map((s) => [s.key, { label: s.label, color: s.color }]),
    ...points.map((pt) => [pt.label, { label: pt.label, color: pt.color }]),
  ])) as Chart.ChartConfig
  const height: Record<string, string> = { sm: 'h-48', md: 'h-64', lg: 'h-80', xl: 'h-[28rem]' }
  const fmt = (v: number) => (typeof v === 'number' ? v.toLocaleString(undefined, { maximumFractionDigits: 2 }) : String(v))
  const total = $derived(points.reduce((a, b) => a + (b.v0 ?? 0), 0))
</script>

<Card.Root class="h-full gap-4">
  {#if p.title || p.description}
    <Card.Header>
      {#if p.title}<Card.Title>{toText(p.title)}</Card.Title>{/if}
      {#if p.description}<Card.Description>{toText(p.description)}</Card.Description>{/if}
    </Card.Header>
  {/if}
  <Card.Content>
    {#if src.loading && !src.rows.length}
      <Skeleton class={['w-full', height[p.height] ?? height.md]} />
    {:else if src.error}
      <p class="text-destructive text-sm">{src.error}</p>
    {:else if !p.labelField}
      <div class={['text-muted-foreground grid place-items-center rounded-lg border border-dashed text-sm', height[p.height] ?? height.md]}>Choose the field to group by.</div>
    {:else if points.length === 0}
      <div class={['text-muted-foreground grid place-items-center text-sm', height[p.height] ?? height.md]}>No data to chart</div>
    {:else}
      <Chart.Container {config} class={['aspect-auto w-full', height[p.height] ?? height.md]}>
        {#if kind === 'pie' || kind === 'donut'}
          <PieChart data={points} key="label" value="v0" c="color" innerRadius={kind === 'donut' ? 0.6 : 0} padding={8}
            props={{ pie: { motion: 'tween' } }} legend={p.legend !== false}>
            {#snippet tooltip()}<Chart.Tooltip hideLabel />{/snippet}
            {#snippet aboveMarks()}
              {#if kind === 'donut'}
                <text text-anchor="middle" dominant-baseline="middle" class="fill-foreground text-2xl font-bold">{fmt(total)}</text>
              {/if}
            {/snippet}
          </PieChart>
        {:else if kind === 'horizontal_bar'}
          <BarChart data={points} orientation="horizontal" y="label" yScale={scaleBand().padding(0.25)} {series} axis="y" grid={false} rule={false}
            seriesLayout={series.length > 1 ? 'group' : undefined} padding={{ left: 90, right: 16 }}
            props={{ bars: { stroke: 'none', rounded: 'all', radius: 4 }, highlight: { area: { fill: 'none' } } }} legend={p.legend && series.length > 1}>
            {#snippet tooltip()}<Chart.Tooltip />{/snippet}
          </BarChart>
        {:else if kind === 'line'}
          <LineChart data={points} x="label" xScale={scalePoint().padding(0.3)} {series} axis="x" legend={p.legend && series.length > 1}
            props={{ spline: { curve: curveMonotoneX, strokeWidth: 2 }, highlight: { points: { r: 4 } } }}>
            {#snippet tooltip()}<Chart.Tooltip />{/snippet}
          </LineChart>
        {:else if kind === 'area'}
          <AreaChart data={points} x="label" xScale={scalePoint().padding(0.3)} {series} axis="x" legend={p.legend && series.length > 1}
            props={{ area: { curve: curveMonotoneX, fillOpacity: 0.35, line: { class: 'stroke-2' } } }}>
            {#snippet tooltip()}<Chart.Tooltip indicator="line" />{/snippet}
          </AreaChart>
        {:else}
          <BarChart data={points} x="label" xScale={scaleBand().padding(0.25)} {series} axis="x"
            seriesLayout={series.length > 1 ? 'group' : undefined} legend={p.legend && series.length > 1}
            props={{ bars: { stroke: 'none', rounded: 'all', radius: 6 }, highlight: { area: { fill: 'none' } } }}>
            {#snippet tooltip()}<Chart.Tooltip />{/snippet}
          </BarChart>
        {/if}
      </Chart.Container>
    {/if}
  </Card.Content>
</Card.Root>
