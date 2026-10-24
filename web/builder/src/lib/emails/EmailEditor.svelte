<script lang="ts">
  import type { Snippet } from 'svelte'
  import * as Tabs from '$lib/components/ui/tabs'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as ToggleGroup from '$lib/components/ui/toggle-group'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import { Separator } from '$lib/components/ui/separator'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import ArrowUpIcon from '@lucide/svelte/icons/arrow-up'
  import ArrowDownIcon from '@lucide/svelte/icons/arrow-down'
  import CopyIcon from '@lucide/svelte/icons/copy'
  import TrashIcon from '@lucide/svelte/icons/trash-2'
  import EyeOffIcon from '@lucide/svelte/icons/eye-off'
  import BindingInput from '../designer/BindingInput.svelte'
  import TemplateEditor from '../apis/TemplateEditor.svelte'
  import type { EmailBlockType, EmailTemplate } from '../api'
  import BlockSettings from './BlockSettings.svelte'
  import FieldsEditor from './FieldsEditor.svelte'
  import StyleEditor from './StyleEditor.svelte'
  import { blockInfo, blockTypes, emailHelp, newBlock, sampleScope, summary, templateGroups } from './blocks'

  // EmailEditor edits a template: its subject and blocks (or HTML), the
  // fields it reads, and its look. The page around it previews and saves.
  let { draft, onChange, selected = $bindable(''), app = null, system = false, history }: {
    draft: EmailTemplate; onChange: () => void; selected?: string; app?: { name: string; slug: string } | null
    system?: boolean; history?: Snippet
  } = $props()
  let tab = $state('content')
  const scope = $derived(sampleScope(draft, app))
  const groups = $derived(templateGroups(draft))
  const changed = () => onChange()

  function add(type: EmailBlockType) {
    const b = newBlock(type)
    const at = draft.blocks.findIndex((x) => x.id === selected)
    draft.blocks.splice(at < 0 ? draft.blocks.length : at + 1, 0, b)
    selected = b.id
    changed()
  }
  function move(i: number, by: number) {
    const j = i + by
    if (j < 0 || j >= draft.blocks.length) return
    const [b] = draft.blocks.splice(i, 1)
    draft.blocks.splice(j, 0, b)
    changed()
  }
  function copy(i: number) {
    const b = { ...$state.snapshot(draft.blocks[i]), id: newBlock('text').id }
    draft.blocks.splice(i + 1, 0, b)
    selected = b.id
    changed()
  }
  function remove(i: number) {
    draft.blocks.splice(i, 1)
    selected = ''
    changed()
  }
  // Clicking a block in the preview opens its settings.
  $effect(() => { if (selected) tab = 'content' })
</script>

<Tabs.Root bind:value={tab} class="flex h-full min-h-0 flex-col gap-0">
  <div class="border-b px-3 pt-2.5 pb-2">
    <Tabs.List class="w-full">
      <Tabs.Trigger value="content">Content</Tabs.Trigger>
      <Tabs.Trigger value="fields">Fields{#if draft.params.length}<span class="text-muted-foreground ml-1 text-[10px]">{draft.params.length}</span>{/if}</Tabs.Trigger>
      <Tabs.Trigger value="style" disabled={draft.mode === 'html'}>Style</Tabs.Trigger>
      {#if history}<Tabs.Trigger value="history">History</Tabs.Trigger>{/if}
    </Tabs.List>
  </div>
  <div class="min-h-0 flex-1 overflow-y-auto">
    <Tabs.Content value="content" class="flex flex-col gap-4 p-4">
      {#if !system}
        <div class="flex flex-col gap-1.5">
          <Label for="tpl-name" class="text-xs">Template name</Label>
          <Input id="tpl-name" class="h-8" value={draft.name} oninput={(e) => { draft.name = e.currentTarget.value; changed() }} />
        </div>
      {/if}
      <div class="flex flex-col gap-1.5">
        <Label class="text-xs">Subject</Label>
        <BindingInput value={draft.subject} {groups} {scope} help={emailHelp} placeholder={'Your order {{ order.Number }}'} onChange={(v) => { draft.subject = v; changed() }} />
      </div>
      <div class="flex flex-col gap-1.5">
        <Label class="text-xs">Preview line</Label>
        <BindingInput value={draft.preheader} {groups} {scope} showPreview={false} help={emailHelp} placeholder="Shown after the subject in the inbox" onChange={(v) => { draft.preheader = v; changed() }} />
      </div>
      <Separator />
      <div class="flex items-center justify-between gap-2">
        <span class="text-sm font-medium">Body</span>
        <ToggleGroup.Root type="single" size="sm" variant="outline" value={draft.mode} onValueChange={(v) => { if (v) { draft.mode = v as 'blocks' | 'html'; changed() } }}>
          <ToggleGroup.Item value="blocks" class="px-3 text-xs">Blocks</ToggleGroup.Item>
          <ToggleGroup.Item value="html" class="px-3 text-xs">HTML</ToggleGroup.Item>
        </ToggleGroup.Root>
      </div>
      {#if draft.mode === 'html'}
        <TemplateEditor value={draft.html} {groups} {scope} help={emailHelp} minRows={18} placeholder={'<h1>Hello {{ customer.Name }}</h1>'} onChange={(v) => { draft.html = v; changed() }} />
        <p class="text-muted-foreground text-xs">Write the whole email. Bound values are escaped for HTML; add | raw to keep markup. The plain-text version is made from it unless you write one below.</p>
      {:else}
        <ol class="flex flex-col gap-1.5" data-testid="block-list">
          {#each draft.blocks as b, i (b.id)}
            {@const info = blockInfo(b.type)}
            <li class={['rounded-lg border', selected === b.id ? 'border-primary ring-primary/20 bg-card ring-2' : 'bg-card']}>
              <div class="group flex items-center gap-2 py-1.5 pr-1 pl-2">
                <button type="button" class="flex min-w-0 flex-1 items-center gap-2 text-left" onclick={() => (selected = selected === b.id ? '' : b.id)}>
                  <span class="bg-muted text-muted-foreground grid size-6 shrink-0 place-items-center rounded"><info.icon class="size-3.5" /></span>
                  <span class="min-w-0 flex-1">
                    <span class="block text-xs font-medium">{info.label}</span>
                    <span class="text-muted-foreground block truncate text-xs">{summary(b)}</span>
                  </span>
                  {#if b.showWhen}<EyeOffIcon class="text-muted-foreground size-3.5 shrink-0" aria-label="Shown only sometimes" />{/if}
                </button>
                <div class={['flex shrink-0 items-center', selected === b.id ? '' : 'opacity-0 group-hover:opacity-100']}>
                  <Button variant="ghost" size="icon" class="size-6" aria-label="Move up" disabled={i === 0} onclick={() => move(i, -1)}><ArrowUpIcon class="size-3.5" /></Button>
                  <Button variant="ghost" size="icon" class="size-6" aria-label="Move down" disabled={i === draft.blocks.length - 1} onclick={() => move(i, 1)}><ArrowDownIcon class="size-3.5" /></Button>
                  <Button variant="ghost" size="icon" class="size-6" aria-label="Duplicate" onclick={() => copy(i)}><CopyIcon class="size-3.5" /></Button>
                  <Button variant="ghost" size="icon" class="hover:text-destructive size-6" aria-label="Delete" onclick={() => remove(i)}><TrashIcon class="size-3.5" /></Button>
                </div>
              </div>
              {#if selected === b.id}
                <div class="border-t p-3"><BlockSettings block={b} {groups} {scope} params={draft} onChange={changed} /></div>
              {/if}
            </li>
          {:else}
            <li class="text-muted-foreground rounded-lg border border-dashed p-6 text-center text-xs">Add a heading, some text and a button to start.</li>
          {/each}
        </ol>
        <DropdownMenu.Root>
          <DropdownMenu.Trigger>
            {#snippet child({ props })}<Button {...props} variant="outline" size="sm" class="self-start border-dashed"><PlusIcon />Add block</Button>{/snippet}
          </DropdownMenu.Trigger>
          <DropdownMenu.Content align="start" class="w-64">
            {#each blockTypes as t (t.type)}
              <DropdownMenu.Item onclick={() => add(t.type)}>
                <t.icon class="size-4" />
                <span class="flex flex-col"><span class="text-sm">{t.label}</span><span class="text-muted-foreground text-xs">{t.description}</span></span>
              </DropdownMenu.Item>
            {/each}
          </DropdownMenu.Content>
        </DropdownMenu.Root>
      {/if}
      <Separator />
      <div class="flex items-center gap-2">
        <Switch id="own-text" checked={draft.text !== ''} onCheckedChange={(v) => { draft.text = v ? 'Hello {{ customer.Name }},\n\n' : ''; changed() }} />
        <Label for="own-text" class="text-xs font-normal">Write the plain-text version myself</Label>
      </div>
      {#if draft.text !== ''}
        <TemplateEditor value={draft.text} {groups} {scope} help={emailHelp} minRows={8} onChange={(v) => { draft.text = v || ' '; changed() }} />
      {:else}
        <p class="text-muted-foreground -mt-2 text-xs">Mail apps that do not show HTML get a text version made from the blocks.</p>
      {/if}
      {#if !system}
        <div class="flex flex-col gap-1.5">
          <Label class="text-xs">Notes for builders</Label>
          <Input class="h-8 text-xs" placeholder="When this email goes out" value={draft.description} oninput={(e) => { draft.description = e.currentTarget.value; changed() }} />
        </div>
      {/if}
    </Tabs.Content>
    <Tabs.Content value="fields" class="p-4">
      <FieldsEditor params={draft.params} readOnly={system} onChange={(p) => { draft.params = p; changed() }} />
    </Tabs.Content>
    <Tabs.Content value="style" class="p-4">
      <StyleEditor style={draft.style} {groups} {scope} onChange={changed} />
    </Tabs.Content>
    {#if history}<Tabs.Content value="history" class="p-4">{@render history()}</Tabs.Content>{/if}
  </div>
</Tabs.Root>
