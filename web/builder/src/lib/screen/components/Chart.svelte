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

  // Five theme colours, then lighter and darker tints of them, so slices
  // and series stay distinct in the legend up to fifteen groups.
  const base = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)']
  const colors = [
    ...base,
    ...base.map((c) => `color-mix(in oklch, ${c} 55%, var(--background))`),
    ...base.map((c) => `color-mix(in oklch, ${c} 65%, var(--foreground))`),
  ]
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

  // The legend is drawn below the chart in normal flow rather than with
  // LayerChart's legend, which floats over the plot and covers the marks
  // once it wraps. Clicking an item hides that slice or series.
  const isPie = $derived(kind === 'pie' || kind === 'donut')
  let hidden = $state<string[]>([])
  const legendItems = $derived(isPie
    ? points.map((pt) => ({ key: pt.label, label: pt.label, color: pt.color }))
    : series.length > 1 ? series : [])
  const showLegend = $derived(legendItems.length > 0 && (isPie ? p.legend !== false : !!p.legend))
  const toggle = (key: string) => (hidden = hidden.includes(key) ? hidden.filter((k) => k !== key) : [...hidden, key])
  const shownPoints = $derived(isPie ? points.filter((pt) => !hidden.includes(pt.label)) : points)
  const shownSeries = $derived.by(() => {
    const s = series.filter((x) => !hidden.includes(x.key))
    return s.length ? s : series
  })
  const total = $derived(shownPoints.reduce((a, b) => a + (b.v0 ?? 0), 0))

  // Horizontal bars get room for their longest label, up to a limit;
  // longer labels are cut with an ellipsis.
  const maxLabel = 22
  const clip = (v: any, n: number) => { const t = String(v); return t.length > n ? t.slice(0, Math.max(1, n - 1)) + '…' : t }
  const shortLabel = (v: any) => clip(v, maxLabel)

  // Labels along the x axis are thinned out and shortened so they never
  // run into each other on narrow screens. charW is the average width of
  // one character of axis text.
  const charW = 6.2
  let plotWidth = $state(0)
  const xAxis = $derived.by(() => {
    const labels = points.map((pt) => String(pt.label))
    if (!plotWidth || !labels.length) return {}
    const slot = plotWidth / labels.length
    const want = Math.min(10, Math.max(...labels.map((l) => l.length))) * charW + 6
    const step = Math.max(1, Math.ceil(want / slot))
    const chars = Math.max(3, Math.floor((slot * step - 6) / charW))
    return { ticks: labels.filter((_, i) => i % step === 0), format: (v: any) => clip(v, chars) }
  })
  const labelRoom = $derived(Math.max(40, Math.min(maxLabel, Math.max(0, ...points.map((pt) => String(pt.label).length))) * 6.5 + 12))
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
      <div class={['flex w-full flex-col gap-3', height[p.height] ?? height.md]} bind:clientWidth={plotWidth}>
      <Chart.Container {config} class="aspect-auto min-h-0 w-full flex-1">
        {#if isPie}
          <PieChart data={shownPoints} key="label" value="v0" c="color"
            innerRadius={kind === 'donut' ? 0.6 : 0} padding={8} props={{ pie: { motion: 'tween' } }}>
            {#snippet tooltip()}<Chart.Tooltip hideLabel />{/snippet}
            {#snippet aboveMarks()}
              {#if kind === 'donut'}
                <text text-anchor="middle" dominant-baseline="middle" class="fill-foreground text-2xl font-bold">{fmt(total)}</text>
              {/if}
            {/snippet}
          </PieChart>
        {:else if kind === 'horizontal_bar'}
          <BarChart data={points} orientation="horizontal" y="label" yScale={scaleBand().padding(0.25)} series={shownSeries} axis="y" grid={false} rule={false}
            seriesLayout={shownSeries.length > 1 ? 'group' : undefined} padding={{ left: labelRoom, right: 16 }}
            props={{ bars: { stroke: 'none', rounded: 'all', radius: 4 }, highlight: { area: { fill: 'none' } }, yAxis: { format: shortLabel } }}>
            {#snippet tooltip()}<Chart.Tooltip />{/snippet}
          </BarChart>
        {:else if kind === 'line'}
          <LineChart data={points} x="label" xScale={scalePoint().padding(0.3)} series={shownSeries} axis="x"
            props={{ spline: { curve: curveMonotoneX, strokeWidth: 2 }, highlight: { points: { r: 4 } }, xAxis }}>
            {#snippet tooltip()}<Chart.Tooltip />{/snippet}
          </LineChart>
        {:else if kind === 'area'}
          <AreaChart data={points} x="label" xScale={scalePoint().padding(0.3)} series={shownSeries} axis="x"
            props={{ area: { curve: curveMonotoneX, fillOpacity: 0.35, line: { class: 'stroke-2' } }, xAxis }}>
            {#snippet tooltip()}<Chart.Tooltip indicator="line" />{/snippet}
          </AreaChart>
        {:else}
          <BarChart data={points} x="label" xScale={scaleBand().padding(0.25)} series={shownSeries} axis="x"
            seriesLayout={shownSeries.length > 1 ? 'group' : undefined}
            props={{ bars: { stroke: 'none', rounded: 'all', radius: 6 }, highlight: { area: { fill: 'none' } }, xAxis }}>
            {#snippet tooltip()}<Chart.Tooltip />{/snippet}
          </BarChart>
        {/if}
      </Chart.Container>
      {#if showLegend}
        <div class="flex flex-wrap items-center justify-center gap-x-4 gap-y-1.5 text-xs">
          {#each legendItems as item (item.key)}
            <button type="button" aria-pressed={!hidden.includes(item.key)} onclick={() => toggle(item.key)}
              class={['flex min-w-0 items-center gap-1.5 rounded-sm', hidden.includes(item.key) && 'text-muted-foreground line-through opacity-60']}>
              <span class="size-2.5 shrink-0 rounded-[2px]" style:background={item.color}></span>
              <span class="truncate">{item.label}</span>
            </button>
          {/each}
        </div>
      {/if}
      </div>
    {/if}
  </Card.Content>
</Card.Root>
