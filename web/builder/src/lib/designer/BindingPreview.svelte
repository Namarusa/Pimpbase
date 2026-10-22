<script lang="ts">
  import { formatPath, parsePath, walk } from '../paths'
  import { describe, preview } from '../preview'

  // BindingPreview shows under a text box what its bindings give with the
  // sample data, and which paths the data does not have.
  let { value, scope, class: className = '' }: { value: string; scope: Record<string, any> | null; class?: string } = $props()
  const p = $derived(preview(value ?? '', scope))

  // hint names the keys there are where a path goes wrong.
  function hint(path: string): string {
    const segs = parsePath(path)
    if (!segs || !scope) return ''
    for (let i = segs.length - 1; i > 0; i--) {
      const r = walk(scope, segs.slice(0, i))
      if (!r.found) continue
      const v = r.value
      if (Array.isArray(v)) return /^\d+$/.test(segs[i]) ? `the list has ${v.length} item${v.length === 1 ? '' : 's'}` : `${formatPath(segs.slice(0, i))} is a list: add a position such as .0, or loop over it`
      if (v && typeof v === 'object') {
        const keys = Object.keys(v)
        return keys.length ? `keys there: ${keys.slice(0, 8).join(', ')}${keys.length > 8 ? ', …' : ''}` : 'it is empty'
      }
      return `${formatPath(segs.slice(0, i))} is ${describe(v, 24)}, not an object`
    }
    return ''
  }
</script>

{#if p.kind !== 'none' && p.kind !== 'blocks'}
  <div class={['flex flex-col gap-0.5 text-[11px] leading-snug', className]} data-binding-preview>
    {#if p.error}
      <span class="text-destructive">{p.error}</span>
    {:else if p.kind === 'formula'}
      <span class="text-muted-foreground">Formula: worked out when the flow runs.</span>
    {:else if p.missing.length}
      {#each p.missing as m (m)}
        {@const h = hint(m)}
        <span class="text-amber-700 dark:text-amber-400"><code class="font-mono">{m}</code> is not in the sample data{h ? `; ${h}` : ''}.</span>
      {/each}
    {:else if p.kind === 'value'}
      <span class="text-muted-foreground truncate">Sample: <span class="text-foreground font-mono">{describe(p.value, 80)}</span></span>
    {:else}
      <span class="text-muted-foreground truncate">Sample: <span class="text-foreground">{p.text}</span></span>
    {/if}
  </div>
{/if}
