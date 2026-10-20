<script lang="ts">
  import { linkLabel } from '../../format'
  import * as Popover from '$lib/components/ui/popover'
  import * as Command from '$lib/components/ui/command'
  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import CheckIcon from '@lucide/svelte/icons/check'
  import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down'
  import XIcon from '@lucide/svelte/icons/x'
  import { api, type LinkRef } from '../../api'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import { getRuntime } from '../context.svelte'
  import type { Node } from '../types'

  // LinkField picks rows of the table a link field points at.
  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const f = new FieldBinding(node, () => p, 'link')
  const target = $derived(rt.table(f.field?.linkTable))
  const many = $derived(!!f.field?.linkMany)
  const primary = $derived(target?.fields.find((x) => x.id === target.primaryField))
  const chosen = $derived<LinkRef[]>(Array.isArray(f.value) ? f.value : f.value ? [f.value] : [])
  let open = $state(false)
  let query = $state('')
  let results = $state<LinkRef[]>([])

  $effect(() => {
    if (!open || !target) return
    const q = query.trim()
    const t = target, pf = primary
    const h = setTimeout(async () => {
      try {
        const filter = q && pf ? `CONTAINS(LOWER(TEXT([${pf.name}])), LOWER("${q.replaceAll('"', '""')}"))` : ''
        const page = await api.rows(rt.appId, t.id, { filter, limit: 30 })
        results = page.rows.map((r) => ({ _id: r._id, label: pf ? String(r[pf.name] ?? '') || `#${r._id}` : `#${r._id}` }))
      } catch { results = [] }
    }, 150)
    return () => clearTimeout(h)
  })

  function toggle(r: LinkRef) {
    if (!many) { f.value = chosen[0]?._id === r._id ? null : r; open = false }
    else f.value = chosen.some((c) => c._id === r._id) ? chosen.filter((c) => c._id !== r._id) : [...chosen, r]
    f.touch()
  }
</script>

<FieldShell {f} help={p.help}>
  {#if !target}
    <p class="text-muted-foreground rounded-md border border-dashed p-3 text-sm">Choose a link field of the form’s table.</p>
  {:else}
    <Popover.Root bind:open>
      <Popover.Trigger id={f.id} disabled={f.disabled}>
        {#snippet child({ props })}
          <Button {...props} variant="outline" role="combobox" aria-invalid={!!f.error} class="h-auto min-h-9 w-full justify-between font-normal">
            <span class="flex flex-wrap gap-1">
              {#each chosen as c (c._id)}
                {#if many}<Badge variant="secondary">{linkLabel(c)}</Badge>{:else}{linkLabel(c)}{/if}
              {:else}<span class="text-muted-foreground">{p.placeholder || `Choose from ${target.name}`}</span>{/each}
            </span>
            <ChevronsUpDownIcon class="opacity-50" />
          </Button>
        {/snippet}
      </Popover.Trigger>
      <Popover.Content class="w-(--bits-popover-anchor-width) min-w-64 p-0" align="start">
        <Command.Root shouldFilter={false}>
          <Command.Input placeholder={`Search ${target.name}…`} bind:value={query} />
          <Command.List>
            <Command.Empty>No rows found.</Command.Empty>
            {#each results as r (r._id)}
              <Command.Item value={String(r._id)} onSelect={() => toggle(r)}>
                <CheckIcon class={chosen.some((c) => c._id === r._id) ? 'opacity-100' : 'opacity-0'} />{r.label}
              </Command.Item>
            {/each}
          </Command.List>
        </Command.Root>
      </Popover.Content>
    </Popover.Root>
    {#if many && chosen.length}
      <div class="flex flex-wrap gap-1">
        {#each chosen as c (c._id)}
          <Badge variant="outline" class="gap-1 pr-1">{linkLabel(c)}<button type="button" class="hover:text-destructive" onclick={() => toggle(c)} aria-label={`Remove ${linkLabel(c)}`}><XIcon class="size-3" /></button></Badge>
        {/each}
      </div>
    {/if}
  {/if}
</FieldShell>
