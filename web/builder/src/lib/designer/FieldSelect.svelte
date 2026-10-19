<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import type { Field, FieldType } from '../api'

  let { value, onChange, fields, types, placeholder = 'Choose a field', allowNone = true, id }: {
    value: string; onChange: (v: string) => void; fields: Field[]; types?: FieldType[]; placeholder?: string; allowNone?: boolean; id?: string
  } = $props()
  const list = $derived(types ? fields.filter((f) => types.includes(f.type) || (f.type === 'formula' && types.includes((f.resultType ?? '') as FieldType))) : fields)
  const none = '\u0000none'
</script>

<Select.Root type="single" value={value || ''} onValueChange={(v) => onChange(v === none ? '' : v)}>
  <Select.Trigger {id} size="sm" class="w-full">
    {#if value}{value}{:else}<span class="text-muted-foreground">{placeholder}</span>{/if}
  </Select.Trigger>
  <Select.Content>
    {#if allowNone}<Select.Item value={none} label="None"><span class="text-muted-foreground">None</span></Select.Item>{/if}
    {#each list as f (f.id)}<Select.Item value={f.name} label={f.name}>{f.name}<span class="text-muted-foreground ml-auto text-xs">{f.type === 'formula' ? `ƒ ${f.resultType}` : f.type.replace('_', ' ')}</span></Select.Item>{/each}
    {#if list.length === 0}<div class="text-muted-foreground px-2 py-1.5 text-xs">Choose a table first</div>{/if}
  </Select.Content>
</Select.Root>
