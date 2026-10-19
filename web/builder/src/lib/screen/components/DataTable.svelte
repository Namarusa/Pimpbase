<script lang="ts">
  import * as Table from '$lib/components/ui/table'
  import { Input } from '$lib/components/ui/input'
  import { Button } from '$lib/components/ui/button'
  import { Checkbox } from '$lib/components/ui/checkbox'
  import { Skeleton } from '$lib/components/ui/skeleton'
  import ArrowUpIcon from '@lucide/svelte/icons/arrow-up'
  import ArrowDownIcon from '@lucide/svelte/icons/arrow-down'
  import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down'
  import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
  import SearchIcon from '@lucide/svelte/icons/search'
  import TrashIcon from '@lucide/svelte/icons/trash'
  import DownloadIcon from '@lucide/svelte/icons/download'
  import { api, type Field, type Row } from '../../api'
  import { getRuntime, getScope } from '../context.svelte'
  import { RowSource } from '../rows.svelte'
  import { runActions } from '../actions'
  import { resolveFilter, toText } from '../bindings'
  import CellValue from './CellValue.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const scope = getScope()

  let page = $state(0)
  let sort = $state<string | undefined>()
  let desc = $state(false)
  let search = $state('')
  let searchQ = $state('')
  let selected = $state<number[]>([])
  const size = $derived(Number(p.pageSize) || 10)

  $effect(() => {
    const q = search
    const h = setTimeout(() => { searchQ = q; page = 0 }, 250)
    return () => clearTimeout(h)
  })

  const src = new RowSource(rt, node.id, () => ({
    props: p, scope: scope(), offset: page * size, limit: size, sort, desc: sort ? desc : undefined, search: searchQ,
  }))

  const columns = $derived.by((): Field[] => {
    const fields = src.table?.fields ?? []
    const chosen: string[] = Array.isArray(p.columns) ? p.columns : []
    if (!chosen.length) return fields.slice(0, 8)
    return chosen.map((n) => fields.find((f) => f.name === n)).filter(Boolean) as Field[]
  })

  // Rows from a surrounding provider are sorted, searched and paged here.
  const view = $derived.by(() => {
    if (!src.fromParent) return { rows: src.rows, total: src.total }
    let rows = src.rows
    if (searchQ) rows = rows.filter((r) => columns.some((c) => toText(r[c.name]).toLowerCase().includes(searchQ.toLowerCase())))
    if (sort) {
      const k = sort
      rows = [...rows].sort((a, b) => {
        const x = a[k], y = b[k]
        const c = typeof x === 'number' && typeof y === 'number' ? x - y : toText(x).localeCompare(toText(y))
        return desc ? -c : c
      })
    }
    return { rows: rows.slice(page * size, page * size + size), total: rows.length }
  })
  const pages = $derived(Math.max(1, Math.ceil(view.total / size)))

  function toggleSort(f: Field) {
    if (sort !== f.name) { sort = f.name; desc = false }
    else if (!desc) desc = true
    else sort = undefined
    page = 0
  }

  async function open(row: Row) {
    if (rt.editing || !p.onRowClick?.length) return
    await runActions(p.onRowClick, rt, scope(), { row })
  }

  async function removeSelected() {
    if (!src.table || !selected.length) return
    if (!(await rt.confirm(`Delete ${selected.length} row${selected.length > 1 ? 's' : ''}?`, 'This cannot be undone.'))) return
    try {
      await api.deleteRows(rt.appId, src.table.id, selected)
      rt.toast('success', `Deleted ${selected.length} row${selected.length > 1 ? 's' : ''}`)
      selected = []
      rt.reload()
    } catch (e: any) { rt.toast('error', e.message) }
  }

  const allOnPage = $derived(view.rows.length > 0 && view.rows.every((r) => selected.includes(r._id)))
  const density = $derived(p.density === 'compact' ? 'py-1.5' : 'py-3')
  const clickable = $derived(!!p.onRowClick?.length)
  function exportCSV() {
    if (src.table) window.open(api.exportURL(rt.appId, src.table.id, resolveFilter(p.filter, scope())), '_blank')
  }
</script>

<div class="flex flex-col gap-3">
  {#if p.title || p.search || p.allowDelete || p.allowExport}
    <div class="flex flex-wrap items-center gap-2">
      {#if p.title}<h3 class="mr-auto text-base font-semibold">{toText(p.title)}</h3>{/if}
      {#if p.search}
        <div class={['relative', p.title ? '' : 'mr-auto']}>
          <SearchIcon class="text-muted-foreground absolute top-1/2 left-2.5 size-4 -translate-y-1/2" />
          <Input class="h-9 w-56 pl-8" placeholder={p.searchPlaceholder || 'Search…'} bind:value={search} />
        </div>
      {/if}
      {#if p.allowDelete && selected.length}
        <Button variant="destructive" size="sm" onclick={removeSelected}><TrashIcon />Delete {selected.length}</Button>
      {/if}
      {#if p.allowExport && src.table && !src.fromParent}
        <Button variant="outline" size="sm" onclick={exportCSV}><DownloadIcon />Export</Button>
      {/if}
    </div>
  {/if}

  <div class="bg-card overflow-hidden rounded-lg border">
    <Table.Root>
      <Table.Header class="bg-muted/50">
        <Table.Row class="hover:bg-transparent">
          {#if p.allowDelete}
            <Table.Head class="w-10">
              <Checkbox checked={allOnPage} onCheckedChange={(v) => (selected = v ? [...new Set([...selected, ...view.rows.map((r) => r._id)])] : selected.filter((id) => !view.rows.some((r) => r._id === id)))} aria-label="Select all" />
            </Table.Head>
          {/if}
          {#each columns as c (c.id)}
            {@const numeric = ['number', 'autonumber'].includes(c.type === 'formula' ? c.resultType ?? '' : c.type)}
            <Table.Head class={numeric ? 'text-right' : ''}>
              {#if p.sortable !== false}
                <button type="button" class={['hover:text-foreground inline-flex items-center gap-1', numeric ? 'flex-row-reverse' : '']} onclick={() => toggleSort(c)}>
                  {c.name}
                  {#if sort === c.name}{#if desc}<ArrowDownIcon class="size-3.5" />{:else}<ArrowUpIcon class="size-3.5" />{/if}
                  {:else}<ChevronsUpDownIcon class="size-3.5 opacity-40" />{/if}
                </button>
              {:else}{c.name}{/if}
            </Table.Head>
          {/each}
        </Table.Row>
      </Table.Header>
      <Table.Body>
        {#if src.loading && !view.rows.length}
          {#each Array(Math.min(size, 5)) as _}
            <Table.Row>
              {#if p.allowDelete}<Table.Cell></Table.Cell>{/if}
              {#each columns as c (c.id)}<Table.Cell class={density}><Skeleton class="h-4 w-3/4" /></Table.Cell>{/each}
            </Table.Row>
          {/each}
        {:else if src.error}
          <Table.Row><Table.Cell colspan={columns.length + 1} class="text-destructive py-8 text-center">{src.error}</Table.Cell></Table.Row>
        {:else if !src.table && rt.editing}
          <Table.Row><Table.Cell class="text-muted-foreground py-8 text-center">Choose a table in the settings, or put this table inside a data provider.</Table.Cell></Table.Row>
        {:else if view.rows.length === 0}
          <Table.Row><Table.Cell colspan={columns.length + 1} class="text-muted-foreground py-10 text-center">{toText(p.emptyText) || 'No rows'}</Table.Cell></Table.Row>
        {:else}
          {#each view.rows as row, i (row._id)}
            <Table.Row class={[clickable ? 'cursor-pointer' : '', p.striped && i % 2 ? 'bg-muted/30' : '']} data-state={selected.includes(row._id) ? 'selected' : undefined} onclick={() => open(row)}>
              {#if p.allowDelete}
                <Table.Cell class={density} onclick={(e) => e.stopPropagation()}>
                  <Checkbox checked={selected.includes(row._id)} onCheckedChange={(v) => (selected = v ? [...selected, row._id] : selected.filter((x) => x !== row._id))} aria-label="Select row" />
                </Table.Cell>
              {/if}
              {#each columns as c (c.id)}
                {@const numeric = ['number', 'autonumber'].includes(c.type === 'formula' ? c.resultType ?? '' : c.type)}
                <Table.Cell class={[density, numeric ? 'text-right tabular-nums' : '', 'max-w-80 truncate']}><CellValue field={c} value={row[c.name]} /></Table.Cell>
              {/each}
            </Table.Row>
          {/each}
        {/if}
      </Table.Body>
    </Table.Root>
  </div>

  {#if p.showFooter !== false && view.total > 0}
    <div class="text-muted-foreground flex items-center justify-between gap-2 text-sm">
      <span>{view.total.toLocaleString()} row{view.total === 1 ? '' : 's'}{selected.length ? ` · ${selected.length} selected` : ''}</span>
      {#if pages > 1}
        <div class="flex items-center gap-2">
          <span class="tabular-nums">Page {page + 1} of {pages}</span>
          <Button variant="outline" size="icon" class="size-8" disabled={page === 0} onclick={() => page--} aria-label="Previous page"><ChevronLeftIcon /></Button>
          <Button variant="outline" size="icon" class="size-8" disabled={page + 1 >= pages} onclick={() => page++} aria-label="Next page"><ChevronRightIcon /></Button>
        </div>
      {/if}
    </div>
  {/if}
</div>
