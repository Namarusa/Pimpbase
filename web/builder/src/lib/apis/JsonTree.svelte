<script lang="ts">
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
  import Self from './JsonTree.svelte'

  // JsonTree shows a response body. Lists can be chosen as the rows, and
  // any value's path copied as a binding.
  let { value, name, path = [], depth = 0, onPick, onRows, rowsPath }: {
    value: any; name?: string; path?: string[]; depth?: number
    onPick?: (path: string[]) => void; onRows?: (path: string[]) => void; rowsPath?: string
  } = $props()
  // Open the first levels and the way to the rows.
  let open = $state(depth < 2 || (!!rowsPath && (rowsPath + '.').startsWith(path.join('.') + '.')))
  const isList = $derived(Array.isArray(value))
  const isObj = $derived(value !== null && typeof value === 'object')
  const entries = $derived(isObj ? (isList ? value.map((v: any, i: number) => [String(i), v]) : Object.entries(value)) : [])
  const shown = $derived(entries.slice(0, 200))
  const here = $derived(path.join('.'))
  const preview = $derived(isList ? `[${value.length}]` : isObj ? `{${Object.keys(value).length}}` : '')
  const scalarClass = (v: any) => typeof v === 'number' ? 'text-sky-600 dark:text-sky-400' : typeof v === 'boolean' ? 'text-violet-600 dark:text-violet-400' : v === null ? 'text-muted-foreground italic' : 'text-emerald-700 dark:text-emerald-400'
  const show = (v: any) => (typeof v === 'string' ? `"${v.length > 200 ? v.slice(0, 200) + '…' : v}"` : String(v))
</script>

<div class="font-mono text-xs leading-6">
  <div class="group hover:bg-muted/60 flex items-center gap-1 rounded pr-1" style={`padding-left:${depth * 14}px`}>
    {#if isObj}
      <button type="button" class="text-muted-foreground grid size-4 place-items-center" onclick={() => (open = !open)} aria-label={open ? 'Collapse' : 'Expand'}>
        <ChevronRightIcon class={['size-3 transition-transform', open ? 'rotate-90' : '']} />
      </button>
    {:else}<span class="size-4"></span>{/if}
    {#if name !== undefined}
      <button type="button" class={['hover:underline', name.startsWith('@') ? 'text-amber-700 dark:text-amber-400' : 'text-foreground']} title="Copy the binding path" onclick={() => onPick?.(path)}>{name}</button><span class="text-muted-foreground">:</span>
    {/if}
    {#if isObj}
      <span class="text-muted-foreground">{preview}</span>
      {#if isList && onRows && depth > 0}
        {#if rowsPath === here}
          <span class="bg-primary/10 text-primary ml-2 rounded px-1.5 font-sans text-[10px] font-medium">rows</span>
        {:else}
          <button type="button" class="text-primary ml-2 hidden rounded px-1.5 font-sans text-[10px] font-medium hover:underline group-hover:inline" onclick={() => onRows(path)}>Use as rows</button>
        {/if}
      {/if}
    {:else}
      <span class={['min-w-0 truncate', scalarClass(value)]}>{show(value)}</span>
    {/if}
  </div>
  {#if isObj && open}
    {#each shown as [k, v] (k)}
      <Self value={v} name={k} path={[...path, k]} depth={depth + 1} {onPick} {onRows} {rowsPath} />
    {/each}
    {#if entries.length > shown.length}<div class="text-muted-foreground" style={`padding-left:${(depth + 1) * 14 + 20}px`}>… {entries.length - shown.length} more</div>{/if}
  {/if}
</div>
