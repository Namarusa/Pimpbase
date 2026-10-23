<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Textarea } from '$lib/components/ui/textarea'
  import { Separator } from '$lib/components/ui/separator'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw'
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert'
  import TemplateEditor from '../apis/TemplateEditor.svelte'
  import BindingInput from '../designer/BindingInput.svelte'
  import type { BindingGroup } from '../designer/context'
  import { ai, type AIModel, type AIProviderStatus, type AIStatus, type FlowStep } from '../api'
  import { navigate } from '../router.svelte'
  import { isAdmin } from '../session.svelte'
  import { getEditor } from './editor.svelte'
  import { flowHelp, systemOneModel, type AIField, type AIQuestion } from './steps'
  import QuestionsEditor from './QuestionsEditor.svelte'

  // AIStepSettings edits an AI step. Its model settings follow the
  // provider: each shows only what that provider's API takes.
  let { step, groups, scope }: { step: FlowStep; groups: BindingGroup[]; scope: Record<string, any> | null } = $props()
  const ed = getEditor()

  // The status is shared by every AI step of the page.
  let status = $state<AIStatus | null>(null)
  let statusError = $state('')
  $effect(() => {
    ai.status().then((s) => (status = s)).catch((e) => (statusError = e.message))
  })

  const workspace = '\u0000workspace'
  const other = '\u0000other'
  const pid = $derived(step.inputs.provider || status?.provider || '')
  const p = $derived(status?.providers.find((x) => x.id === pid))
  const o = $derived(p?.step)
  const label = $derived(p?.modelLabel ?? 'Model')
  // A provider with a key and address is usable once the step names a
  // model, even with none saved.
  const usable = (x?: AIProviderStatus) => !!x && (x.configured || x.missing === `a ${x.modelLabel.toLowerCase()}`)

  let models = $state<AIModel[]>([])
  let modelsFor = $state('')
  let modelsError = $state('')
  let loading = $state(false)
  let typing = $state(false)
  async function load(id: string) {
    loading = true
    modelsError = ''
    try {
      const r = await ai.models({ provider: id })
      if (pid === id) { models = r.models; modelsFor = id }
    } catch (e: any) {
      if (pid === id) { models = []; modelsFor = id; modelsError = e.message }
    } finally {
      loading = false
    }
  }
  $effect(() => {
    if (usable(p) && p!.canList && modelsFor !== p!.id) load(p!.id)
  })
  const known = $derived(models.length ? models.map((m) => m.id) : (p?.models ?? []))
  const model = $derived(step.inputs.model ?? '')
  $effect(() => {
    if (model && known.length && !known.includes(model) && !model.includes('{{')) typing = true
  })

  // A System One model answers questions about a state, not a prompt.
  const so = $derived(!!p && systemOneModel(p.id, model || p.model, p.systemOne ?? []))
  const questions = $derived((step.inputs.questions ?? []) as AIQuestion[])
  $effect(() => {
    if (so && !questions.length) set('questions', [{ name: 'category', type: 'choice', instructions: '', options: [{ name: '' }, { name: '' }] }])
  })

  function set(key: string, v: any) {
    if (v === '' || v === undefined || (Array.isArray(v) && !v.length && key !== 'fields')) delete step.inputs[key]
    else step.inputs[key] = v
    ed.changed()
  }

  // A new provider drops the settings it does not take.
  function pickProvider(v: string) {
    const id = v === workspace ? '' : v
    const next = status?.providers.find((x) => x.id === (id || status?.provider))?.step
    set('provider', id)
    delete step.inputs.model
    typing = false
    if (next) {
      if (!next.temperature) delete step.inputs.temperature
      if (!next.topP) delete step.inputs.topP
      if (!next.seed) delete step.inputs.seed
      if (!next.stop) delete step.inputs.stop
      if (!next.effort?.includes(step.inputs.effort)) delete step.inputs.effort
      if (!next.thinking?.includes(step.inputs.thinking)) delete step.inputs.thinking
    }
    if (id !== 'azure' && (id || status?.provider) !== 'azure') delete step.inputs.apiVersion
    models = []
    modelsFor = ''
    ed.changed()
  }

  const output = $derived(step.inputs.output ?? 'text')
  const fields = $derived((step.inputs.fields ?? []) as AIField[])
  const setField = (k: number, patch: Partial<AIField>) => set('fields', fields.map((f, m) => (m === k ? { ...f, ...patch } : f)))
  const fieldTypes = [{ value: 'text', label: 'Text' }, { value: 'number', label: 'Number' }, { value: 'boolean', label: 'Yes / no' }, { value: 'list', label: 'List of texts' }]
  const nameOk = (n: string) => !n || /^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(n)
  const effortWords: Record<string, string> = { minimal: 'Minimal', low: 'Low', medium: 'Medium', high: 'High', xhigh: 'Extra high', max: 'Max', off: 'Off' }
  const none = '\u0000none'
  const outputLabels: Record<string, string> = { text: 'Text', json: 'JSON', fields: 'Fields' }
</script>

<div class="flex flex-col gap-1.5">
  <Label class="text-xs">Provider</Label>
  <Select.Root type="single" value={step.inputs.provider || workspace} onValueChange={pickProvider}>
    <Select.Trigger size="sm" class="w-full" data-testid="ai-provider">
      {#if step.inputs.provider}{p?.name ?? step.inputs.provider}{:else}Workspace default{status ? ` (${status.providers.find((x) => x.id === status!.provider)?.name})` : ''}{/if}
    </Select.Trigger>
    <Select.Content>
      <Select.Item value={workspace} label="Workspace default">Workspace default<span class="text-muted-foreground ml-auto text-[10px]">{status?.providers.find((x) => x.id === status!.provider)?.name}</span></Select.Item>
      <Select.Separator />
      {#each status?.providers ?? [] as x (x.id)}
        <Select.Item value={x.id} label={x.name}>{x.name}<span class={['ml-auto text-[10px]', usable(x) ? 'text-emerald-600 dark:text-emerald-400' : 'text-muted-foreground']}>{usable(x) ? 'set up' : 'not set up'}</span></Select.Item>
      {/each}
    </Select.Content>
  </Select.Root>
  {#if statusError}<p class="text-destructive text-xs">{statusError}</p>{/if}
  {#if p && !usable(p)}
    <div class="flex items-start gap-2 rounded-md border border-amber-500/40 bg-amber-500/8 p-2 text-xs">
      <TriangleAlertIcon class="mt-0.5 size-3.5 shrink-0 text-amber-600" />
      <div class="flex-1">
        {p.name} still needs {p.missing}. {#if isAdmin()}<button class="underline" onclick={() => navigate('/users/ai')}>Set it up</button>{:else}A workspace admin adds it in Users → AI assistant.{/if}
      </div>
    </div>
  {/if}
</div>

{#if p}
  <div class="flex flex-col gap-1.5">
    <div class="flex items-center justify-between">
      <Label class="text-xs">{label}</Label>
      {#if usable(p) && p.canList}
        <Button variant="ghost" size="sm" class="text-muted-foreground h-6 px-1.5 text-[11px]" disabled={loading} onclick={() => load(p!.id)}><RefreshCwIcon class={['size-3', loading && 'animate-spin']} />{loading ? 'Loading' : 'Reload list'}</Button>
      {/if}
    </div>
    {#if typing || !known.length}
      <div class="flex gap-1.5">
        <Input class="h-8 font-mono text-xs" data-testid="ai-model-input" value={model} placeholder={p.model || (p.id === 'azure' ? 'my-gpt-deployment' : 'model id')} oninput={(e) => set('model', e.currentTarget.value.trim())} />
        {#if known.length}<Button variant="outline" size="sm" class="h-8 text-xs" onclick={() => { typing = false; set('model', '') }}>List</Button>{/if}
      </div>
    {:else}
      <Select.Root type="single" value={model || none} onValueChange={(v) => { if (v === other) { typing = true; return } set('model', v === none ? '' : v) }}>
        <Select.Trigger size="sm" class="w-full font-mono text-xs" data-testid="ai-model">{model || (p.model ? `Saved: ${p.model}` : `Choose a ${label.toLowerCase()}`)}</Select.Trigger>
        <Select.Content class="max-h-72">
          {#if p.model}<Select.Item value={none} label="Saved">Saved {label.toLowerCase()}<span class="text-muted-foreground ml-auto font-mono text-[10px]">{p.model}</span></Select.Item><Select.Separator />{/if}
          {#each models.length ? models : known.map((id) => ({ id }) as AIModel) as m (m.id)}
            <Select.Item value={m.id} label={m.id}><span class="font-mono text-xs">{m.id}</span>{#if m.note || (m.name && m.name !== m.id)}<span class="text-muted-foreground ml-auto truncate pl-2 text-[10px]">{m.note || m.name}</span>{/if}</Select.Item>
          {/each}
          <Select.Separator />
          <Select.Item value={other} label="Type another">Type another…</Select.Item>
        </Select.Content>
      </Select.Root>
    {/if}
    <p class={['text-xs', modelsError ? 'text-amber-600 dark:text-amber-400' : 'text-muted-foreground']}>
      {#if modelsError}{modelsError}. Type the {label.toLowerCase()} instead.{:else if !model && !p.model && usable(p)}Choose one: {p.name} has no saved {label.toLowerCase()}.{:else if !model && p.model}Empty uses the saved {label.toLowerCase()}, {p.model}.{:else if p.id === 'azure'}The deployment name from Azure AI Foundry.{:else}Listed live from {p.name}.{/if}
    </p>
  </div>
  {#if p.hasApiVersion}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">API version</Label>
      <Input class="h-8 font-mono text-xs" value={step.inputs.apiVersion ?? ''} placeholder={p.apiVersion || '2024-10-21'} oninput={(e) => set('apiVersion', e.currentTarget.value.trim())} />
      <p class="text-muted-foreground text-xs">Empty uses {p.apiVersion || 'the saved one'}; v1 uses Azure's v1 API.</p>
    </div>
  {/if}
{/if}

{#if so}
  <div class="rounded-md border border-violet-500/30 bg-violet-500/6 p-2 text-xs" data-testid="so-note">
    <span class="font-medium">{model || p?.model} is a System One model.</span>
    It does not write text: it answers typed questions about the state below, each with probabilities, in one fast call. Use a condition on <code class="font-mono">.confidence</code> to send unsure cases to a person or a larger model.
  </div>
  <div class="flex flex-col gap-1.5">
    <Label class="text-xs">State</Label>
    <TemplateEditor value={step.inputs.state ?? ''} {groups} {scope} help={flowHelp} minRows={4} placeholder={'{{ trigger.row }}'} onChange={(v) => set('state', v)} />
    <p class="text-muted-foreground text-xs">What the questions are about. A whole binding such as <code class="font-mono">{'{{ trigger.row }}'}</code> is sent as JSON; anything else as text.</p>
  </div>
  <div class="flex flex-col gap-1.5">
    <Label class="text-xs">Questions</Label>
    <QuestionsEditor {questions} step={step.name} {groups} {scope} onChange={(q) => set('questions', q)} />
    <p class="text-muted-foreground text-xs">Each question is answered on its own, in parallel. Ask one specific thing per question.</p>
  </div>
{:else}
<div class="flex flex-col gap-1.5">
  <Label class="text-xs">Instructions</Label>
  <TemplateEditor value={step.inputs.system ?? ''} {groups} {scope} help={flowHelp} minRows={3} placeholder="You sort support tickets for Acme. Be brief." onChange={(v) => set('system', v)} />
  <p class="text-muted-foreground text-xs">The system prompt: who the model is and how to answer. Optional.</p>
</div>
<div class="flex flex-col gap-1.5">
  <Label class="text-xs">Prompt</Label>
  <TemplateEditor value={step.inputs.prompt ?? ''} {groups} {scope} help={flowHelp} minRows={6} placeholder={'Sort this ticket.\n\nSubject: {{ trigger.row.Subject }}\n{{ trigger.row.Body }}'} onChange={(v) => set('prompt', v)} />
</div>

<div class="flex flex-col gap-1.5">
  <Label class="text-xs">Answer</Label>
  <Select.Root type="single" value={output} onValueChange={(v) => { set('output', v); if (v === 'fields' && !fields.length) set('fields', [{ name: '', type: 'text' }]) }}>
    <Select.Trigger size="sm" class="w-full" data-testid="ai-output">{outputLabels[output] ?? 'Text'}</Select.Trigger>
    <Select.Content>
      <Select.Item value="text" label="Text">Text<span class="text-muted-foreground ml-auto text-[10px]">steps.{step.name}.text</span></Select.Item>
      <Select.Item value="json" label="JSON">JSON<span class="text-muted-foreground ml-auto text-[10px]">any shape, in .data</span></Select.Item>
      <Select.Item value="fields" label="Fields">Fields<span class="text-muted-foreground ml-auto text-[10px]">named values, in .data</span></Select.Item>
    </Select.Content>
  </Select.Root>
  {#if output === 'fields'}
    <div class="flex flex-col gap-1.5 rounded-md border p-2">
      {#each fields as f, k (k)}
        <div class="flex items-center gap-1.5">
          <Input class="h-7 w-[38%] shrink-0 font-mono text-xs" aria-invalid={!nameOk(f.name)} value={f.name} placeholder="category" oninput={(e) => setField(k, { name: e.currentTarget.value.trim() })} />
          <Select.Root type="single" value={f.type || 'text'} onValueChange={(v) => setField(k, { type: v as AIField['type'] })}>
            <Select.Trigger size="sm" class="h-7 min-w-0 flex-1 text-xs">{fieldTypes.find((x) => x.value === (f.type || 'text'))?.label}</Select.Trigger>
            <Select.Content>{#each fieldTypes as t (t.value)}<Select.Item value={t.value} label={t.label}>{t.label}</Select.Item>{/each}</Select.Content>
          </Select.Root>
          <Button variant="ghost" size="icon" class="size-7 shrink-0" aria-label="Remove field" onclick={() => set('fields', fields.filter((_, m) => m !== k))}><XIcon class="size-3.5" /></Button>
        </div>
        <Input class="-mt-0.5 h-7 text-xs" value={f.description ?? ''} placeholder="What it holds, e.g. billing, bug or question" oninput={(e) => setField(k, { description: e.currentTarget.value })} />
      {/each}
      <Button variant="outline" size="sm" class="h-7 self-start border-dashed text-xs" onclick={() => set('fields', [...fields, { name: '', type: 'text' }])}><PlusIcon />Add field</Button>
    </div>
    <p class="text-muted-foreground text-xs">
      Later steps read <code class="font-mono">{`{{ steps.${step.name}.data.${fields.find((f) => f.name)?.name ?? 'name'} }}`}</code>.
      {#if o && !o.schema}{p?.name} has no JSON schemas, so the fields are asked for in the instructions and checked on the answer.{:else if o}{p?.name} is held to the fields by a JSON schema.{/if}
    </p>
  {:else if output === 'json'}
    <p class="text-muted-foreground text-xs">Say in the prompt what the JSON holds. The step fails when the answer is not valid JSON.</p>
  {/if}
</div>

{/if}

{#if o && !so}
  <Separator />
  <div class="flex flex-col gap-3" data-testid="ai-model-settings">
    <div>
      <h3 class="text-xs font-semibold">Model settings</h3>
      <p class="text-muted-foreground text-xs">What {p?.name} takes. Empty fields keep its defaults; each can be a binding.</p>
    </div>
    <div class="grid grid-cols-2 gap-x-2 gap-y-3">
      <div class="flex flex-col gap-1.5">
        <Label class="text-xs">Length limit</Label>
        <BindingInput value={step.inputs.maxTokens ?? ''} {groups} {scope} showPreview={false} help={flowHelp} placeholder={`${o.maxTokens} tokens`} onChange={(v) => set('maxTokens', v)} />
      </div>
      {#if o.effort?.length}
        <div class="flex flex-col gap-1.5">
          <Label class="text-xs">{o.effortLabel}</Label>
          <Select.Root type="single" value={step.inputs.effort || none} onValueChange={(v) => set('effort', v === none ? '' : v)}>
            <Select.Trigger size="sm" class="w-full" data-testid="ai-effort">{step.inputs.effort ? effortWords[step.inputs.effort] : 'Default'}</Select.Trigger>
            <Select.Content>
              <Select.Item value={none} label="Default">Default</Select.Item>
              {#each o.effort as e (e)}<Select.Item value={e} label={effortWords[e] ?? e}>{effortWords[e] ?? e}</Select.Item>{/each}
            </Select.Content>
          </Select.Root>
        </div>
      {/if}
      {#if o.thinking?.length}
        <div class="flex flex-col gap-1.5">
          <Label class="text-xs">Thinking</Label>
          <Select.Root type="single" value={step.inputs.thinking || none} onValueChange={(v) => set('thinking', v === none ? '' : v)}>
            <Select.Trigger size="sm" class="w-full">{step.inputs.thinking ? effortWords[step.inputs.thinking] : 'Default'}</Select.Trigger>
            <Select.Content>
              <Select.Item value={none} label="Default">Model default</Select.Item>
              {#each o.thinking as e (e)}<Select.Item value={e} label={effortWords[e] ?? e}>{effortWords[e] ?? e}</Select.Item>{/each}
            </Select.Content>
          </Select.Root>
        </div>
      {/if}
      {#if o.temperature}
        <div class="flex flex-col gap-1.5">
          <Label class="text-xs">Temperature</Label>
          <BindingInput value={step.inputs.temperature ?? ''} {groups} {scope} showPreview={false} help={flowHelp} placeholder="0 to 2" onChange={(v) => set('temperature', v)} />
        </div>
      {/if}
      {#if o.topP}
        <div class="flex flex-col gap-1.5">
          <Label class="text-xs">Top P</Label>
          <BindingInput value={step.inputs.topP ?? ''} {groups} {scope} showPreview={false} help={flowHelp} placeholder="0 to 1" onChange={(v) => set('topP', v)} />
        </div>
      {/if}
      {#if o.seed}
        <div class="flex flex-col gap-1.5">
          <Label class="text-xs">Seed</Label>
          <BindingInput value={step.inputs.seed ?? ''} {groups} {scope} showPreview={false} help={flowHelp} placeholder="random" onChange={(v) => set('seed', v)} />
        </div>
      {/if}
    </div>
    {#if o.effort?.length && o.effortHelp}<p class="text-muted-foreground -mt-1 text-xs">{o.effortHelp}</p>{/if}
    {#if o.thinking?.length}<p class="text-muted-foreground -mt-1 text-xs">Gemini 2.5 models get a thinking budget (off, 1k or 16k tokens); Gemini 3 a thinking level.</p>{/if}
    {#if o.stop}
      <div class="flex flex-col gap-1.5">
        <Label class="text-xs">Stop sequences</Label>
        <Textarea class="min-h-14 font-mono text-xs" value={step.inputs.stop ?? ''} placeholder={'One per line, up to 4'} oninput={(e) => set('stop', e.currentTarget.value)} />
      </div>
    {/if}
    {#if o.notes}<p class="text-muted-foreground text-xs">{o.notes}</p>{/if}
  </div>
{/if}
