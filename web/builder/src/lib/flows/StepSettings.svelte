<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import { Separator } from '$lib/components/ui/separator'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import TrashIcon from '@lucide/svelte/icons/trash'
  import CopyIcon from '@lucide/svelte/icons/copy'
  import BindingInput from '../designer/BindingInput.svelte'
  import BindingPreview from '../designer/BindingPreview.svelte'
  import TemplateEditor from '../apis/TemplateEditor.svelte'
  import KeyValueEditor from '../apis/KeyValueEditor.svelte'
  import type { FlowStep, Pair } from '../api'
  import { getEditor } from './editor.svelte'
  import LoopItemPanel from './LoopItemPanel.svelte'
  import SampleBox from './SampleBox.svelte'
  import AIStepSettings from './AIStepSettings.svelte'
  import { flowHelp, stepBindings, stepInfo, type InputSpec } from './steps'

  // StepSettings edits the selected step's name and inputs.
  let { step }: { step: FlowStep } = $props()
  const ed = getEditor()
  const info = $derived(stepInfo(step.type))
  const scope = $derived(ed.scope(step.id))
  const groups = $derived(ed.flow ? stepBindings(ed.flow, step.id, ed.tables, ed.queries, ed.variables, scope) : [])
  const table = $derived(ed.table(step.inputs.table))
  const query = $derived(ed.queries.find((q) => q.id === step.inputs.query))
  const shown = $derived((info?.inputs ?? []).filter((i) => !i.when || i.when(step.inputs)))
  const none = '\u0000none'

  let name = $state('')
  $effect(() => { name = step.name })
  const nameOk = $derived(/^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(name))

  function set(key: string, v: any) {
    step.inputs[key] = v
    ed.changed()
  }
  function rename(v: string) {
    name = v
    if (!/^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(v)) return
    const old = step.name
    step.name = v
    // Bindings that read the old name follow the rename.
    if (ed.flow && old && old !== v) {
      const re = new RegExp(`steps\\.${old}\\b`, 'g')
      const fix = (x: any): any => typeof x === 'string' ? x.replace(re, `steps.${v}`) : Array.isArray(x) ? x.map(fix) : x && typeof x === 'object' ? Object.fromEntries(Object.entries(x).map(([k, y]) => [k, fix(y)])) : x
      const visit = (list: FlowStep[]) => { for (const s of list) { s.inputs = fix(s.inputs); for (const b of [s.then, s.else, s.do]) if (b) visit(b) } }
      visit(ed.flow.steps)
    }
    ed.changed()
  }
  const val = (i: InputSpec) => step.inputs[i.key] ?? i.default ?? ''
  const values = $derived((step.inputs.values ?? []) as { field: string; value: string }[])
</script>

{#if info}
  <div class="flex flex-col gap-4 p-4">
    <div class="flex items-center gap-3">
      <span class={['grid size-9 place-items-center rounded-lg', info.tone]}><info.icon class="size-4.5" /></span>
      <div class="min-w-0 flex-1">
        <h2 class="text-sm font-semibold">{info.label}</h2>
        <p class="text-muted-foreground text-xs">{info.description}</p>
      </div>
    </div>

    <div class="flex flex-col gap-1.5">
      <Label for="step-name" class="text-xs">Name</Label>
      <Input id="step-name" class="h-8 font-mono text-xs" value={name} oninput={(e) => rename(e.currentTarget.value)} aria-invalid={!nameOk} />
      <p class={['text-xs', nameOk ? 'text-muted-foreground' : 'text-destructive']}>
        {#if nameOk}Later steps read the result as <code class="font-mono">{`{{ steps.${name}.… }}`}</code>{:else}Letters, digits and underscores, starting with a letter.{/if}
      </p>
    </div>
    <Separator />

    {#if step.type === 'ai'}<AIStepSettings {step} {groups} {scope} />{/if}
    {#each step.type === 'ai' ? [] : shown as i (i.key)}
      {#if i.kind === 'boolean'}
        <div class="flex items-center gap-2">
          <Switch id={`in-${i.key}`} checked={!!(step.inputs[i.key] ?? i.default)} onCheckedChange={(v) => set(i.key, v)} />
          <Label for={`in-${i.key}`} class="text-xs font-normal">{i.label}</Label>
        </div>
      {:else}
        <div class="flex flex-col gap-1.5">
          <Label class="text-xs">{i.label}</Label>
          {#if i.kind === 'text' || i.kind === 'formula'}
            <BindingInput value={val(i)} mono={i.kind === 'formula' || String(val(i)).includes('{{')} {groups} {scope} help={flowHelp} placeholder={i.placeholder} onChange={(v) => set(i.key, v)} />
          {:else if i.kind === 'template' || i.kind === 'long'}
            <TemplateEditor value={val(i)} {groups} {scope} help={flowHelp} minRows={i.key === 'content' ? 14 : 7} placeholder={i.placeholder} onChange={(v) => set(i.key, v)} />
          {:else if i.kind === 'select'}
            <Select.Root type="single" value={val(i)} onValueChange={(v) => set(i.key, v)}>
              <Select.Trigger size="sm" class="w-full">{i.options?.find((o) => o.value === val(i))?.label ?? 'Choose'}</Select.Trigger>
              <Select.Content>{#each i.options ?? [] as o (o.value)}<Select.Item value={o.value} label={o.label}>{o.label}</Select.Item>{/each}</Select.Content>
            </Select.Root>
          {:else if i.kind === 'table'}
            <Select.Root type="single" value={step.inputs.table ?? ''} onValueChange={(v) => { step.inputs.table = v; if (step.inputs.values) step.inputs.values = []; step.inputs.field = ''; ed.changed() }}>
              <Select.Trigger size="sm" class="w-full">{table?.name ?? 'Choose a table'}</Select.Trigger>
              <Select.Content>{#each step.type === 'generate_pdf' ? ed.tables.filter((t) => !t.external) : ed.tables as t (t.id)}<Select.Item value={t.id} label={t.name}>{t.name}{t.external ? ' · external' : ''}</Select.Item>{/each}</Select.Content>
            </Select.Root>
          {:else if i.kind === 'field' || i.kind === 'attachment_field'}
            {@const opts = i.kind === 'field' ? [{ name: '_id' }, { name: '_createdAt' }, { name: '_updatedAt' }, ...(table?.fields ?? [])] : (table?.fields ?? []).filter((f) => f.type === 'attachment')}
            <Select.Root type="single" value={step.inputs[i.key] || none} onValueChange={(v) => set(i.key, v === none ? '' : v)}>
              <Select.Trigger size="sm" class="w-full">{step.inputs[i.key] || (i.kind === 'field' ? 'Unsorted' : 'Do not attach')}</Select.Trigger>
              <Select.Content>
                <Select.Item value={none} label="None">{i.kind === 'field' ? 'Unsorted' : 'Do not attach'}</Select.Item>
                {#each opts as f (f.name)}<Select.Item value={f.name} label={f.name}>{f.name}</Select.Item>{/each}
                {#if i.kind === 'attachment_field' && !opts.length}<div class="text-muted-foreground px-2 py-1.5 text-xs">{table?.name ?? 'This table'} has no attachment field.</div>{/if}
              </Select.Content>
            </Select.Root>
          {:else if i.kind === 'query'}
            <Select.Root type="single" value={step.inputs.query ?? ''} onValueChange={(v) => { step.inputs.query = v; step.inputs.params = {}; ed.changed() }}>
              <Select.Trigger size="sm" class="w-full">{query?.name ?? 'Choose a query'}</Select.Trigger>
              <Select.Content>
                {#each ed.queries as q (q.id)}<Select.Item value={q.id} label={q.name}>{q.name}<span class="text-muted-foreground ml-auto font-mono text-[10px]">{q.method}</span></Select.Item>{/each}
                {#if !ed.queries.length}<div class="text-muted-foreground px-2 py-1.5 text-xs">Add a query in the APIs tab first.</div>{/if}
              </Select.Content>
            </Select.Root>
          {:else if i.kind === 'params'}
            {#each query?.parameters ?? [] as qp (qp.name)}
              <div class="flex items-center gap-1.5">
                <span class="w-2/5 shrink-0 truncate font-mono text-xs">{qp.name}</span>
                <div class="min-w-0 flex-1"><BindingInput value={step.inputs.params?.[qp.name] ?? ''} {groups} {scope} showPreview={false} help={flowHelp} placeholder={qp.default || 'default'} onChange={(v) => set('params', { ...(step.inputs.params ?? {}), [qp.name]: v })} /></div>
              </div>
              <BindingPreview class="-mt-1 pl-[calc(40%+0.375rem)]" value={step.inputs.params?.[qp.name] ?? ''} {scope} />
            {:else}
              <span class="text-muted-foreground text-xs">{query ? 'This query takes no parameters.' : 'Choose a query first.'}</span>
            {/each}
          {:else if i.kind === 'pairs'}
            <KeyValueEditor value={(step.inputs[i.key] ?? []) as Pair[]} {groups} {scope} addLabel="Add header" keyPlaceholder="X-Api-Key" onChange={(v) => set(i.key, v)} />
          {:else if i.kind === 'values'}
            {#each values as v, k (k)}
              <div class="flex items-center gap-1.5">
                <Select.Root type="single" value={v.field} onValueChange={(fv) => set('values', values.map((x, m) => (m === k ? { ...x, field: fv } : x)))}>
                  <Select.Trigger size="sm" class="w-2/5 shrink-0">{v.field || 'Field'}</Select.Trigger>
                  <Select.Content>{#each (table?.fields ?? []).filter((f) => f.type !== 'formula' && f.type !== 'autonumber') as f (f.id)}<Select.Item value={f.name} label={f.name}>{f.name}<span class="text-muted-foreground ml-auto text-[10px]">{f.type}</span></Select.Item>{/each}</Select.Content>
                </Select.Root>
                <div class="min-w-0 flex-1"><BindingInput value={v.value} {groups} {scope} showPreview={false} help={flowHelp} placeholder="value" onChange={(nv) => set('values', values.map((x, m) => (m === k ? { ...x, value: nv } : x)))} /></div>
                <Button variant="ghost" size="icon" class="size-7 shrink-0" aria-label="Remove value" onclick={() => set('values', values.filter((_, m) => m !== k))}><XIcon class="size-3.5" /></Button>
              </div>
              <BindingPreview class="-mt-1 pr-8 pl-[calc(40%+0.375rem)]" value={v.value} {scope} />
            {/each}
            <div class="flex gap-2">
              <Button variant="outline" size="sm" class="h-7 border-dashed text-xs" onclick={() => set('values', [...values, { field: '', value: '' }])}><PlusIcon />Add value</Button>
              {#if table && values.length === 0}
                <Button variant="ghost" size="sm" class="h-7 text-xs" onclick={() => set('values', table.fields.filter((f) => f.type !== 'formula' && f.type !== 'autonumber').map((f) => ({ field: f.name, value: '' })))}>Add every field</Button>
              {/if}
            </div>
          {/if}
          {#if i.help}<p class="text-muted-foreground text-xs">{i.help}</p>{/if}
          {#if step.type === 'loop' && i.key === 'items'}<LoopItemPanel {step} />{/if}
        </div>
      {/if}
    {/each}

    {#if step.type === 'run_query' || step.type === 'find_rows'}<SampleBox {step} />{/if}
    <Separator />
    <div class="flex items-center gap-2">
      <Switch id="step-continue" checked={!!step.continueOnError} onCheckedChange={(v) => { step.continueOnError = v || undefined; ed.changed() }} />
      <Label for="step-continue" class="text-xs font-normal">Go on when this step fails</Label>
    </div>
    <div class="flex items-center gap-2">
      <Switch id="step-off" checked={!step.disabled} onCheckedChange={(v) => { step.disabled = !v || undefined; ed.changed() }} />
      <Label for="step-off" class="text-xs font-normal">Step is switched on</Label>
    </div>
    <div class="flex gap-2 pt-1">
      <Button variant="outline" size="sm" onclick={() => ed.duplicate(step.id)}><CopyIcon />Duplicate</Button>
      <Button variant="outline" size="sm" class="text-destructive hover:text-destructive" onclick={() => ed.remove(step.id)}><TrashIcon />Delete</Button>
    </div>
  </div>
{/if}
