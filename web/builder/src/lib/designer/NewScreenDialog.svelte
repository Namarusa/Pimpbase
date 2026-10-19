<script lang="ts">
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Select from '$lib/components/ui/select'
  import * as RadioGroup from '$lib/components/ui/radio-group'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import { api } from '../api'
  import { getDesigner } from './state.svelte'
  import { blankScreen, dashboardScreen, detailScreen, listScreen, slug } from './templates'
  import type { Screen } from '../screen/types'

  let { open = $bindable(false), onCreated }: { open: boolean; onCreated: (s: Screen) => void } = $props()
  const d = getDesigner()
  let kind = $state<'blank' | 'crud' | 'dashboard'>('crud')
  let name = $state('')
  let route = $state('')
  let tableId = $state('')
  let busy = $state(false)
  let error = $state('')

  $effect(() => {
    if (open) {
      error = ''
      name = ''
      route = ''
      kind = d.tables.length ? 'crud' : 'blank'
      tableId = d.tables[0]?.id ?? ''
    }
  })

  async function create(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    busy = true
    try {
      const t = d.table(tableId)
      const specs = kind === 'blank' || !t
        ? [blankScreen(name || 'New screen', route || `/${slug(name || 'new-screen')}`)]
        : kind === 'crud' ? [listScreen(t), detailScreen(t)] : [dashboardScreen(t)]
      let first: Screen | null = null
      for (const s of specs) {
        let r = s.route
        // A taken route gets a number, so templates can be added twice.
        for (let i = 2; d.screens.some((x) => x.route === r); i++) r = `${s.route.replace(/\/:id$/, '')}-${i}${s.route.endsWith('/:id') ? '/:id' : ''}`
        const created = await api.createScreen(d.app.id, { name: s.name, route: r, definition: s.definition })
        d.screens = [...d.screens, created]
        first ??= created
      }
      open = false
      if (first) onCreated(first)
    } catch (err: any) {
      error = err.message
    } finally {
      busy = false
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header>
      <Dialog.Title>New screen</Dialog.Title>
      <Dialog.Description>Start from a table to get working screens at once, or from a blank page.</Dialog.Description>
    </Dialog.Header>
    <form id="new-screen" class="flex flex-col gap-4" onsubmit={create}>
      <RadioGroup.Root bind:value={kind} class="grid gap-2">
        {#each [
          { v: 'crud', t: 'List and details', s: 'A searchable table with an add dialog, and a page to view and edit each row.' },
          { v: 'dashboard', t: 'Dashboard', s: 'Key numbers, charts and the latest rows of a table.' },
          { v: 'blank', t: 'Blank', s: 'An empty page to build yourself.' },
        ] as o (o.v)}
          <Label for={`k-${o.v}`} class={['hover:bg-muted/50 flex cursor-pointer items-start gap-3 rounded-lg border p-3 font-normal', kind === o.v ? 'border-primary bg-primary/5' : '', o.v !== 'blank' && !d.tables.length ? 'pointer-events-none opacity-50' : '']}>
            <RadioGroup.Item value={o.v} id={`k-${o.v}`} class="mt-0.5" disabled={o.v !== 'blank' && !d.tables.length} />
            <span class="flex flex-col gap-0.5"><span class="text-sm font-medium">{o.t}</span><span class="text-muted-foreground text-xs">{o.s}</span></span>
          </Label>
        {/each}
      </RadioGroup.Root>
      {#if kind === 'blank'}
        <div class="flex flex-col gap-2"><Label for="ns-name">Name</Label><Input id="ns-name" bind:value={name} placeholder="Overview" required /></div>
        <div class="flex flex-col gap-2"><Label for="ns-route">Route</Label><Input id="ns-route" class="font-mono" bind:value={route} placeholder={`/${slug(name || 'overview')}`} /></div>
      {:else}
        <div class="flex flex-col gap-2">
          <Label>Table</Label>
          <Select.Root type="single" bind:value={tableId}>
            <Select.Trigger class="w-full">{d.table(tableId)?.name ?? 'Choose a table'}</Select.Trigger>
            <Select.Content>{#each d.tables as t (t.id)}<Select.Item value={t.id} label={t.name}>{t.name}</Select.Item>{/each}</Select.Content>
          </Select.Root>
        </div>
      {/if}
      {#if !d.tables.length}<p class="text-muted-foreground text-xs">Add a table in Data to start screens from your data.</p>{/if}
      {#if error}<p class="text-destructive text-sm">{error}</p>{/if}
    </form>
    <Dialog.Footer>
      <Button variant="outline" onclick={() => (open = false)}>Cancel</Button>
      <Button type="submit" form="new-screen" disabled={busy}>{#if busy}<Spinner />{/if}Create</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
