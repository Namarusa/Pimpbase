<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Switch } from '$lib/components/ui/switch'
  import { Textarea } from '$lib/components/ui/textarea'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import TrashIcon from '@lucide/svelte/icons/trash-2'
  import type { EmailParam } from '../api'

  // FieldsEditor lists the values a template reads, which the Send email
  // step fills, each with a sample for previews. readOnly shows the fixed
  // fields of a system email.
  let { params, onChange, readOnly = false }: { params: EmailParam[]; onChange: (p: EmailParam[]) => void; readOnly?: boolean } = $props()
  const types = [
    { value: 'text', label: 'Text' }, { value: 'number', label: 'Number' }, { value: 'boolean', label: 'Yes / no' },
    { value: 'object', label: 'Object (a row)' }, { value: 'list', label: 'List (rows)' },
  ]
  let drafts = $state<Record<number, string>>({})
  let errors = $state<Record<number, string>>({})
  const nameOk = (n: string) => /^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(n) && !['app', 'loop', 'item', 'index', 'number'].includes(n)

  const update = (i: number, p: Partial<EmailParam>) => onChange(params.map((x, k) => (k === i ? { ...x, ...p } : x)))
  const json = (v: any) => (v === undefined ? '' : JSON.stringify(v, null, 2))
  function setJSON(i: number, src: string) {
    drafts[i] = src
    try {
      const v = src.trim() ? JSON.parse(src) : undefined
      const t = params[i].type
      if (v !== undefined && t === 'list' && !Array.isArray(v)) throw new Error('a list is written [ … ]')
      if (v !== undefined && t === 'object' && (typeof v !== 'object' || Array.isArray(v) || v === null)) throw new Error('an object is written { … }')
      errors[i] = ''
      update(i, { sample: v })
    } catch (e: any) { errors[i] = e.message }
  }
  function setType(i: number, t: EmailParam['type']) {
    const samples: Record<string, any> = { text: '', number: 0, boolean: true, object: { Name: 'Sample' }, list: [{ Name: 'Sample' }] }
    delete drafts[i]
    errors[i] = ''
    update(i, { type: t, sample: samples[t] })
  }
  function add() {
    let n = 'field'
    for (let k = 2; params.some((p) => p.name === n); k++) n = `field${k}`
    onChange([...params, { name: n, type: 'text', sample: '' }])
  }
</script>

<div class="flex flex-col gap-3">
  <p class="text-muted-foreground text-xs">{readOnly ? 'Pimpbase fills these when it sends this email.' : 'Fields are what this template reads, such as a customer row or a list of order lines. A flow\'s Send email step fills them; the sample values here fill the preview.'}</p>
  {#each params as p, i (i)}
    <div class="bg-card flex flex-col gap-2 rounded-lg border p-2.5">
      <div class="flex items-center gap-1.5">
        <Input class="h-8 font-mono text-xs" value={p.name} aria-invalid={!nameOk(p.name)} disabled={readOnly} oninput={(e) => update(i, { name: e.currentTarget.value.trim() })} />
        <Select.Root type="single" value={p.type} disabled={readOnly} onValueChange={(v) => setType(i, v as EmailParam['type'])}>
          <Select.Trigger size="sm" class="w-36 shrink-0">{types.find((t) => t.value === p.type)?.label}</Select.Trigger>
          <Select.Content>{#each types as t (t.value)}<Select.Item value={t.value} label={t.label}>{t.label}</Select.Item>{/each}</Select.Content>
        </Select.Root>
        {#if !readOnly}<Button variant="ghost" size="icon" class="size-8 shrink-0" aria-label="Remove field" onclick={() => onChange(params.filter((_, k) => k !== i))}><TrashIcon class="size-3.5" /></Button>{/if}
      </div>
      {#if !nameOk(p.name)}<p class="text-destructive text-xs">Letters, digits and underscores, starting with a letter; not app or loop.</p>{/if}
      <Input class="h-7 text-xs" placeholder="What it is (shown in the Send email step)" value={p.description ?? ''} disabled={readOnly} oninput={(e) => update(i, { description: e.currentTarget.value })} />
      <div class="text-muted-foreground text-[11px] font-medium tracking-wide uppercase">Sample</div>
      {#if p.type === 'boolean'}
        <div class="flex items-center gap-2"><Switch checked={!!p.sample} onCheckedChange={(v) => update(i, { sample: v })} /><span class="text-xs">{p.sample ? 'Yes' : 'No'}</span></div>
      {:else if p.type === 'object' || p.type === 'list'}
        <Textarea rows={p.type === 'list' ? 6 : 4} class="font-mono text-xs" value={drafts[i] ?? json(p.sample)} oninput={(e) => setJSON(i, e.currentTarget.value)} />
        {#if errors[i]}<p class="text-destructive text-xs">{errors[i]}</p>{/if}
      {:else}
        <Input class="h-8 text-sm" type={p.type === 'number' ? 'number' : 'text'} value={p.sample ?? ''} oninput={(e) => update(i, { sample: p.type === 'number' ? +e.currentTarget.value : e.currentTarget.value })} />
      {/if}
    </div>
  {:else}
    <p class="text-muted-foreground rounded-lg border border-dashed p-4 text-center text-xs">No fields yet.</p>
  {/each}
  {#if !readOnly}<Button variant="outline" size="sm" class="self-start border-dashed" onclick={add}><PlusIcon />Add field</Button>{/if}
</div>
