<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import BindingInput from '../designer/BindingInput.svelte'
  import TemplateEditor from '../apis/TemplateEditor.svelte'
  import type { EmailBlock, EmailColumn } from '../api'
  import type { BindingGroup } from '../designer/context'
  import { parsePath, walk } from '../paths'
  import { emailHelp, templateGroups } from './blocks'

  // BlockSettings edits one block of an email template.
  let { block, groups, scope, params, onChange }: {
    block: EmailBlock; groups: BindingGroup[]; scope: Record<string, any>; params: any; onChange: () => void
  } = $props()

  const set = (k: keyof EmailBlock, v: any) => { (block as any)[k] = v; onChange() }
  const aligns = [{ value: 'left', label: 'Left' }, { value: 'center', label: 'Center' }, { value: 'right', label: 'Right' }]

  // The first item of a table's list, for the column pickers.
  const firstItem = $derived.by(() => {
    const src = (block.items ?? '').trim().replace(/^\{\{\s*/, '').replace(/\s*\}\}$/, '')
    const segs = src ? parsePath(src) : null
    if (!segs) return {}
    const r = walk(scope, segs)
    return Array.isArray(r.value) ? (r.value[0] ?? {}) : {}
  })
  const itemScope = $derived({ ...scope, loop: { item: firstItem, number: 1, index: 0 } })
  const itemGroups = $derived(templateGroups(params, firstItem))
  const columns = $derived(block.columns ?? [])
  const setCol = (i: number, p: Partial<EmailColumn>) => set('columns', columns.map((c, k) => (k === i ? { ...c, ...p } : c)))
</script>

<div class="flex flex-col gap-3">
  {#if block.type === 'heading' || block.type === 'button'}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">{block.type === 'button' ? 'Label' : 'Text'}</Label>
      <BindingInput value={block.text ?? ''} {groups} {scope} help={emailHelp} onChange={(v) => set('text', v)} />
    </div>
  {/if}
  {#if block.type === 'text'}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Text</Label>
      <TemplateEditor value={block.text ?? ''} {groups} {scope} help={emailHelp} minRows={5} onChange={(v) => set('text', v)} />
      <p class="text-muted-foreground text-xs">A blank line starts a paragraph. **bold**, *italic*, [label](https://…), and lines starting with "- " for a list.</p>
    </div>
  {/if}
  {#if block.type === 'heading'}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Size</Label>
      <Select.Root type="single" value={String(block.level ?? 1)} onValueChange={(v) => set('level', +v)}>
        <Select.Trigger size="sm" class="w-full">{({ 1: 'Large', 2: 'Medium', 3: 'Small' } as Record<string, string>)[String(block.level ?? 1)]}</Select.Trigger>
        <Select.Content><Select.Item value="1" label="Large">Large</Select.Item><Select.Item value="2" label="Medium">Medium</Select.Item><Select.Item value="3" label="Small">Small</Select.Item></Select.Content>
      </Select.Root>
    </div>
  {/if}
  {#if block.type === 'button' || block.type === 'image'}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">{block.type === 'button' ? 'Goes to' : 'Image address'}</Label>
      <BindingInput value={block.url ?? ''} mono {groups} {scope} help={emailHelp} placeholder="https://" onChange={(v) => set('url', v)} />
    </div>
  {/if}
  {#if block.type === 'button'}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Color</Label>
      <div class="flex items-center gap-2">
        <input type="color" class="size-8 shrink-0 cursor-pointer rounded border" value={block.color || '#2563eb'} oninput={(e) => set('color', e.currentTarget.value)} aria-label="Button color" />
        <Input class="h-8 font-mono text-xs" placeholder="Brand color" value={block.color ?? ''} oninput={(e) => set('color', e.currentTarget.value)} />
      </div>
    </div>
  {/if}
  {#if block.type === 'image'}
    <div class="grid grid-cols-2 gap-2">
      <div class="flex flex-col gap-1.5"><Label class="text-xs">Alt text</Label><BindingInput value={block.alt ?? ''} {groups} {scope} showPreview={false} help={emailHelp} onChange={(v) => set('alt', v)} /></div>
      <div class="flex flex-col gap-1.5"><Label class="text-xs">Width (px)</Label><Input class="h-8" type="number" min="0" max="520" placeholder="Full" value={block.width || ''} oninput={(e) => set('width', +e.currentTarget.value || 0)} /></div>
    </div>
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Link (optional)</Label>
      <BindingInput value={block.link ?? ''} mono {groups} {scope} help={emailHelp} placeholder="https://" onChange={(v) => set('link', v)} />
    </div>
  {/if}
  {#if block.type === 'table'}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">One row for each item of</Label>
      <BindingInput value={block.items ?? ''} mono {groups} {scope} help={emailHelp} placeholder={'{{ lines }}'} onChange={(v) => set('items', v)} />
    </div>
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Columns</Label>
      {#each columns as c, i (i)}
        <div class="bg-muted/40 flex flex-col gap-1.5 rounded-md border p-2">
          <div class="flex items-center gap-1.5">
            <Input class="h-7 text-xs" placeholder="Heading" value={c.label} oninput={(e) => setCol(i, { label: e.currentTarget.value })} />
            <Select.Root type="single" value={c.align || 'left'} onValueChange={(v) => setCol(i, { align: v === 'left' ? '' : v })}>
              <Select.Trigger size="sm" class="h-7 w-24 shrink-0 text-xs">{aligns.find((a) => a.value === (c.align || 'left'))?.label}</Select.Trigger>
              <Select.Content>{#each aligns as a (a.value)}<Select.Item value={a.value} label={a.label}>{a.label}</Select.Item>{/each}</Select.Content>
            </Select.Root>
            <Button variant="ghost" size="icon" class="size-7 shrink-0" aria-label="Remove column" onclick={() => set('columns', columns.filter((_, k) => k !== i))}><XIcon class="size-3.5" /></Button>
          </div>
          <BindingInput value={c.value} mono groups={itemGroups} scope={itemScope} help={emailHelp} placeholder={'{{ loop.item.Name }}'} onChange={(v) => setCol(i, { value: v })} />
        </div>
      {/each}
      <Button variant="outline" size="sm" class="h-7 self-start border-dashed text-xs" onclick={() => set('columns', [...columns, { label: '', value: '' }])} disabled={columns.length >= 8}><PlusIcon />Add column</Button>
    </div>
  {/if}
  {#if block.type === 'spacer'}
    <div class="flex flex-col gap-1.5"><Label class="text-xs">Height (px)</Label><Input class="h-8" type="number" min="4" max="200" value={block.height ?? 16} oninput={(e) => set('height', +e.currentTarget.value || 16)} /></div>
  {/if}
  {#if block.type === 'html'}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">HTML</Label>
      <TemplateEditor value={block.text ?? ''} {groups} {scope} help={emailHelp} minRows={8} onChange={(v) => set('text', v)} />
      <p class="text-muted-foreground text-xs">Bound values are escaped; add | raw to put HTML in as it is. Mail apps ignore scripts and most CSS outside style attributes.</p>
    </div>
  {/if}
  {#if ['heading', 'text', 'button', 'image'].includes(block.type)}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Align</Label>
      <Select.Root type="single" value={block.align || 'left'} onValueChange={(v) => set('align', v === 'left' ? '' : v)}>
        <Select.Trigger size="sm" class="w-full">{aligns.find((a) => a.value === (block.align || 'left'))?.label}</Select.Trigger>
        <Select.Content>{#each aligns as a (a.value)}<Select.Item value={a.value} label={a.label}>{a.label}</Select.Item>{/each}</Select.Content>
      </Select.Root>
    </div>
  {/if}
  <div class="flex flex-col gap-1.5">
    <Label class="text-xs">Show only when</Label>
    <BindingInput value={block.showWhen ?? ''} mono {groups} {scope} help={emailHelp} placeholder={'{{ order.Paid }} (always shown when empty)'} onChange={(v) => set('showWhen', v)} />
  </div>
</div>
