<script lang="ts">
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Sheet from '$lib/components/ui/sheet'
  import Children from '../Children.svelte'
  import { extendScope, getRuntime, pathTo } from '../context.svelte'
  import { gapClass } from '../styles'
  import type { Node } from '../types'

  // One component for dialogs and side panels: a side panel is a dialog
  // that slides in from the edge.
  let { node, p, panel = false }: { node: Node; p: Record<string, any>; panel?: boolean } = $props()
  const rt = getRuntime()
  extendScope(() => rt.dialogScopes[node.id] ?? {})

  const getOpen = () => !!rt.dialogs[node.id]
  const setOpen = (v: boolean) => (rt.dialogs[node.id] = v)
  const showInline = $derived(rt.editing && !!rt.selected && !!pathTo(node, rt.selected))
  const width: Record<string, string> = { sm: 'sm:max-w-sm', md: 'sm:max-w-lg', lg: 'sm:max-w-2xl', xl: 'sm:max-w-4xl' }
</script>

{#if rt.editing}
  {#if showInline}
    <div class="bg-background mx-auto my-2 w-full max-w-2xl rounded-lg border p-6 shadow-lg ring-4 ring-black/5">
      <div class="mb-4 flex flex-col gap-1.5">
        <div class="text-lg font-semibold leading-none">{p.title || (panel ? 'Side panel' : 'Dialog')}</div>
        {#if p.description}<div class="text-muted-foreground text-sm">{p.description}</div>{/if}
      </div>
      <div class={['flex flex-col', gapClass[p.gap], p.$layout]}><Children {node} empty="Add the dialog’s content here" /></div>
    </div>
  {:else}
    <div class="text-muted-foreground flex items-center gap-2 rounded-md border border-dashed px-3 py-2 text-xs">
      <span class="font-medium">{panel ? 'Side panel' : 'Dialog'}:</span> {p.title || 'Untitled'}
      <span class="ml-auto">opens with the “Open dialog” action</span>
    </div>
  {/if}
{:else if panel}
  <Sheet.Root bind:open={getOpen, setOpen}>
    <Sheet.Content side={p.side === 'left' ? 'left' : 'right'} class={['w-full overflow-y-auto sm:max-w-lg', p.$surface]}>
      <Sheet.Header>
        <Sheet.Title>{p.title}</Sheet.Title>
        {#if p.description}<Sheet.Description>{p.description}</Sheet.Description>{/if}
      </Sheet.Header>
      <div class={['@container flex flex-col px-4 pb-6', gapClass[p.gap], p.$layout]}><Children {node} /></div>
    </Sheet.Content>
  </Sheet.Root>
{:else}
  <Dialog.Root bind:open={getOpen, setOpen}>
    <Dialog.Content class={['max-h-[90vh] overflow-y-auto', width[p.size] ?? width.md, p.$surface]}>
      <Dialog.Header>
        <Dialog.Title>{p.title}</Dialog.Title>
        {#if p.description}<Dialog.Description>{p.description}</Dialog.Description>{/if}
      </Dialog.Header>
      <div class={['@container flex flex-col', gapClass[p.gap], p.$layout]}><Children {node} /></div>
    </Dialog.Content>
  </Dialog.Root>
{/if}
