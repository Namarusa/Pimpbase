<script lang="ts">
  import * as Avatar from '$lib/components/ui/avatar'
  import { Badge } from '$lib/components/ui/badge'
  import { Skeleton } from '$lib/components/ui/skeleton'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
  import { getRuntime, getScope, shareValues } from '../context.svelte'
  import { RowSource } from '../rows.svelte'
  import { runActions } from '../actions'
  import { toText } from '../bindings'
  import type { Row } from '../../api'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const scope = getScope()
  const src = new RowSource(rt, node.id, () => ({ props: p, scope: scope() }))
  shareValues(node.id, () => ({ rows: src.rows, total: src.total, loading: src.loading }))
  const clickable = $derived(!!p.onRowClick?.length)
  const title = (r: Row) => toText(r[p.titleField]) || (src.table?.primaryField ? toText(r[src.table.fields.find((f) => f.id === src.table!.primaryField)?.name ?? '']) : '') || `Row ${r._id}`
  const initials = (s: string) => s.split(/\s+/).filter(Boolean).slice(0, 2).map((w) => w[0]?.toUpperCase()).join('')

  async function open(row: Row) {
    if (!rt.editing && clickable) await runActions(p.onRowClick, rt, scope(), { row })
  }
</script>

<div class={['bg-card overflow-hidden rounded-lg', p.bordered === false ? '' : 'border']}>
  {#if p.title}<div class="border-b px-4 py-3 text-sm font-semibold">{toText(p.title)}</div>{/if}
  {#if src.loading && !src.rows.length}
    {#each Array(3) as _}<div class="flex items-center gap-3 px-4 py-3"><Skeleton class="size-9 rounded-full" /><div class="flex flex-1 flex-col gap-2"><Skeleton class="h-4 w-1/3" /><Skeleton class="h-3 w-1/2" /></div></div>{/each}
  {:else if src.error}
    <p class="text-destructive p-4 text-sm">{src.error}</p>
  {:else if src.rows.length === 0}
    <p class="text-muted-foreground p-8 text-center text-sm">{toText(p.emptyText) || 'Nothing here yet'}</p>
  {:else}
    <ul class="divide-y">
      {#each src.rows as row (row._id)}
        <li>
          <svelte:element this={clickable ? 'button' : 'div'} type={clickable ? 'button' : undefined} role={clickable ? undefined : 'listitem'} onclick={() => open(row)}
            class={['flex w-full items-center gap-3 px-4 py-3 text-left', clickable ? 'hover:bg-muted/50 cursor-pointer transition-colors' : '']}>
            {#if p.showAvatar}
              <Avatar.Root class="size-9">
                {#if p.imageField && toText(row[p.imageField])}<Avatar.Image src={toText(row[p.imageField])} alt="" />{/if}
                <Avatar.Fallback class="bg-primary/10 text-primary text-xs font-medium">{initials(title(row))}</Avatar.Fallback>
              </Avatar.Root>
            {/if}
            <div class="min-w-0 flex-1">
              <div class="truncate text-sm font-medium">{title(row)}</div>
              {#if p.subtitleField}<div class="text-muted-foreground truncate text-sm">{toText(row[p.subtitleField])}</div>{/if}
            </div>
            {#if p.badgeField && toText(row[p.badgeField])}<Badge variant="secondary">{toText(row[p.badgeField])}</Badge>{/if}
            {#if p.metaField}<span class="text-muted-foreground shrink-0 text-sm tabular-nums">{toText(row[p.metaField])}</span>{/if}
            {#if clickable}<ChevronRightIcon class="text-muted-foreground size-4 shrink-0" />{/if}
          </svelte:element>
        </li>
      {/each}
    </ul>
  {/if}
</div>
