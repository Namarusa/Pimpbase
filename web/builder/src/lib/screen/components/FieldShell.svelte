<script lang="ts">
  import type { Snippet } from 'svelte'
  import { Label } from '$lib/components/ui/label'
  import type { FieldBinding } from '../field.svelte'

  let { f, help, children, inline = false }: { f: FieldBinding; help?: string; children: Snippet; inline?: boolean } = $props()
</script>

<div class="flex flex-col gap-2" data-invalid={!!f.error || undefined}>
  {#if inline}
    <div class="flex items-center gap-3">
      {@render children()}
      <Label for={f.id} class="font-normal">{f.label}{#if f.required}<span class="text-destructive">*</span>{/if}</Label>
    </div>
  {:else}
    <Label for={f.id} class={f.error ? 'text-destructive' : ''}>{f.label}{#if f.required}<span class="text-destructive">*</span>{/if}</Label>
    {@render children()}
  {/if}
  {#if f.error}
    <p class="text-destructive text-sm">{f.error}</p>
  {:else if help}
    <p class="text-muted-foreground text-sm">{help}</p>
  {/if}
</div>
