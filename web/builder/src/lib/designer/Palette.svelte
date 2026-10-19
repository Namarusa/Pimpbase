<script lang="ts">
  import * as Command from '$lib/components/ui/command'
  import { categories, componentList } from '../screen/registry'

  // Palette lists the components to add, searchable and grouped.
  let { onPick }: { onPick: (type: string) => void } = $props()
  const hidden = new Set(['tab', 'accordion_item'])
</script>

<Command.Root class="max-h-[70vh]">
  <Command.Input placeholder="Search components…" />
  <Command.List class="max-h-[60vh]">
    <Command.Empty>No component matches.</Command.Empty>
    {#each categories as cat}
      <Command.Group heading={cat}>
        {#each componentList.filter((c) => c.category === cat && !hidden.has(c.type)) as c (c.type)}
          <Command.Item value={`${c.label} ${c.description}`} onSelect={() => onPick(c.type)} class="items-start gap-3 py-2">
            <span class="bg-muted grid size-8 shrink-0 place-items-center rounded-md"><c.icon class="size-4" /></span>
            <span class="flex min-w-0 flex-col">
              <span class="font-medium">{c.label}</span>
              <span class="text-muted-foreground line-clamp-2 text-xs">{c.description}</span>
            </span>
          </Command.Item>
        {/each}
      </Command.Group>
    {/each}
  </Command.List>
</Command.Root>
