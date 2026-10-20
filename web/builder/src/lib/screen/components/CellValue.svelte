<script lang="ts">
  import CheckIcon from '@lucide/svelte/icons/check'
  import PaperclipIcon from '@lucide/svelte/icons/paperclip'
  import { Badge } from '$lib/components/ui/badge'
  import { display, linkLabel } from '../../format'
  import type { Field } from '../../api'

  // CellValue shows one field's value the way its type reads best.
  let { field, value }: { field?: Field; value: any } = $props()
  const type = $derived(field?.type === 'formula' ? field.resultType : field?.type)
</script>

{#if value === null || value === undefined || value === '' || (Array.isArray(value) && value.length === 0)}
  <span class="text-muted-foreground/60">—</span>
{:else if !field}
  {String(value)}
{:else if type === 'boolean'}
  {#if value}<CheckIcon class="text-primary size-4" />{:else}<span class="text-muted-foreground/60">—</span>{/if}
{:else if field.type === 'single_select'}
  <Badge variant="secondary">{value}</Badge>
{:else if field.type === 'multi_select'}
  <span class="inline-flex flex-wrap gap-1">{#each value as v}<Badge variant="secondary">{v}</Badge>{/each}</span>
{:else if field.type === 'attachment'}
  <span class="inline-flex flex-wrap gap-1">
    {#each value as file (file.id)}
      <Badge variant="outline" href={file.url} target="_blank" rel="noopener" onclick={(e: MouseEvent) => e.stopPropagation()} class="max-w-48 gap-1"><PaperclipIcon class="size-3" /><span class="truncate">{file.name}</span></Badge>
    {/each}
  </span>
{:else if field.type === 'link'}
  <span class="inline-flex flex-wrap gap-1">{#each Array.isArray(value) ? value : [value] as v}<Badge variant="outline" class={v.hidden ? "text-muted-foreground italic" : ""}>{linkLabel(v)}</Badge>{/each}</span>
{:else}
  {display(field, value)}
{/if}
