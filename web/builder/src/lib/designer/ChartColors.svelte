<script lang="ts">
  import XIcon from '@lucide/svelte/icons/x'

  // ChartColors sets the colour of single slices or series of a chart.
  // Items without a colour of their own take the next palette colour.
  let { value, onChange, items }: { value: Record<string, string>; onChange: (v: Record<string, string>) => void; items: string[] } = $props()
  const own = $derived(value && typeof value === 'object' ? value : {})
  // Colours set for labels the settings no longer list stay editable.
  const all = $derived([...new Set([...items, ...Object.keys(own)])])

  function set(label: string, color: string | null) {
    const next = { ...own }
    if (color) next[label] = color
    else delete next[label]
    onChange(next)
  }
</script>

{#if all.length}
  <div class="flex flex-col gap-1">
    {#each all as label (label)}
      <div class="flex h-7 items-center gap-2 text-xs">
        <label class="relative size-5 shrink-0 cursor-pointer" title={own[label] ? own[label] : 'Automatic: the next palette colour'}>
          <span class={['block size-5 rounded-full', own[label] ? 'border' : 'border-muted-foreground/50 border border-dashed']} style:background={own[label]}></span>
          <input type="color" class="absolute inset-0 size-full cursor-pointer opacity-0" value={own[label] || '#4f46e5'}
            oninput={(e) => set(label, e.currentTarget.value)} aria-label={`Colour of ${label}`} />
        </label>
        <span class="min-w-0 flex-1 truncate">{label}</span>
        {#if own[label]}
          <button type="button" class="text-muted-foreground hover:text-foreground" onclick={() => set(label, null)} aria-label={`Automatic colour for ${label}`}><XIcon class="size-3.5" /></button>
        {:else}
          <span class="text-muted-foreground">Auto</span>
        {/if}
      </div>
    {/each}
  </div>
{:else}
  <p class="text-muted-foreground text-xs">Group a pie or donut by a single-select or yes/no field to colour its slices one by one; otherwise pick a palette above.</p>
{/if}
