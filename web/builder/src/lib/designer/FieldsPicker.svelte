<script lang="ts">
  import { Checkbox } from '$lib/components/ui/checkbox'
  import { Button } from '$lib/components/ui/button'
  import ArrowUpIcon from '@lucide/svelte/icons/arrow-up'
  import type { Field, FieldType } from '../api'

  // FieldsPicker chooses several fields and their order.
  let { value, onChange, fields, types }: { value: string[]; onChange: (v: string[]) => void; fields: Field[]; types?: FieldType[] } = $props()
  const list = $derived(types ? fields.filter((f) => types.includes(f.type) || (f.type === 'formula' && types.includes((f.resultType ?? '') as FieldType))) : fields)
  const chosen = $derived(Array.isArray(value) ? value : [])
  const ordered = $derived([...chosen.filter((n) => list.some((f) => f.name === n)), ...list.map((f) => f.name).filter((n) => !chosen.includes(n))])

  function toggle(name: string, on: boolean) {
    onChange(on ? [...chosen, name] : chosen.filter((n) => n !== name))
  }
  function up(name: string) {
    const i = chosen.indexOf(name)
    if (i <= 0) return
    const next = [...chosen]
    ;[next[i - 1], next[i]] = [next[i], next[i - 1]]
    onChange(next)
  }
</script>

{#if list.length === 0}
  <p class="text-muted-foreground text-xs">Choose a table first.</p>
{:else}
  <div class="flex max-h-56 flex-col overflow-y-auto rounded-md border">
    {#each ordered as name (name)}
      <div class="hover:bg-muted/60 flex items-center gap-2 px-2 py-1 text-sm">
        <Checkbox checked={chosen.includes(name)} onCheckedChange={(v) => toggle(name, !!v)} aria-label={name} />
        <span class="flex-1 truncate">{name}</span>
        {#if chosen.indexOf(name) > 0}
          <Button variant="ghost" size="icon" class="size-6" onclick={() => up(name)} aria-label={`Move ${name} up`}><ArrowUpIcon class="size-3" /></Button>
        {/if}
      </div>
    {/each}
  </div>
{/if}
