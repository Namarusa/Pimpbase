<script lang="ts">
  // AssistantSheet is the builder's AI assistant: it drafts tables,
  // screens, queries and flows for review, and answers questions about the
  // app's data with the access of a chosen role.
  import * as Sheet from '$lib/components/ui/sheet'
  import * as Tabs from '$lib/components/ui/tabs'
  import * as ToggleGroup from '$lib/components/ui/toggle-group'
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Textarea } from '$lib/components/ui/textarea'
  import { Label } from '$lib/components/ui/label'
  import { Spinner } from '$lib/components/ui/spinner'
  import SparklesIcon from '@lucide/svelte/icons/sparkles'
  import CheckIcon from '@lucide/svelte/icons/check'
  import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw'
  import SearchIcon from '@lucide/svelte/icons/search'
  import { toast } from 'svelte-sonner'
  import { ai, api, ApiError, type AIAnswer, type AIDraft, type AIStatus, type App, type DraftKind, type Member, type RoleDef, type Table } from '../api'
  import { navigate } from '../router.svelte'
  import { catalog } from './catalog'
  import { applyDraft } from './apply'
  import { markdown } from './markdown'
  import { reload } from './state.svelte'
  import DraftReview from './DraftReview.svelte'
  import NoKey from './NoKey.svelte'

  let { open = $bindable(false), app, kind: startKind = 'table', target }: {
    open: boolean; app: App; kind?: DraftKind; target?: { kind: DraftKind; id: string }
  } = $props()
  // targetName names the screen, query or flow open in the builder.
  let targetName = $state('')

  let status = $state<AIStatus | null>(null)
  let tables = $state<Table[]>([])
  let roles = $state<RoleDef[]>([])
  let members = $state<Member[]>([])
  let tab = $state('build')
  let kind = $state<DraftKind>('table')
  let change = $state('new')
  let prompt = $state('')
  let busy = $state(false)
  let error = $state('')
  let draft = $state<AIDraft | null>(null)
  let question = $state('')
  let as = $state('')
  let answer = $state<AIAnswer | null>(null)

  const kinds: { value: DraftKind; label: string; example: string }[] = [
    { value: 'table', label: 'Table', example: 'Track invoices for each customer: number, issue date, due date, amount, status (draft, sent, paid) and the PDF.' },
    { value: 'screen', label: 'Screen', example: 'A page listing open invoices with a search box, totals at the top and a button to add an invoice.' },
    { value: 'query', label: 'Query', example: 'Get the orders of a customer from the shop API, by customer id, newest first.' },
    { value: 'flow', label: 'Flow', example: 'Every weekday at 8:00, find invoices that are overdue and email their customer a reminder.' },
  ]

  $effect(() => {
    if (!open) return
    kind = startKind
    change = target && target.kind === startKind ? 'current' : 'new'
    load()
  })

  async function load() {
    try {
      const [st, ts, rs, ms] = await Promise.all([ai.status(), api.tables(app.id), api.roles(app.id), api.members(app.id)])
      status = st; tables = ts; roles = rs; members = ms
      targetName = ''
      if (target?.kind === 'screen') targetName = (await api.screens(app.id)).find((x) => x.id === target.id)?.name ?? ''
      if (target?.kind === 'flow') targetName = (await api.flows(app.id)).find((x) => x.id === target.id)?.name ?? ''
      if (target?.kind === 'query') targetName = (await api.queries(app.id)).find((x) => x.id === target.id)?.name ?? ''
    } catch (e: any) { error = e.message }
  }

  function fail(e: any) {
    if (e instanceof ApiError && e.body?.code === 'ai_not_configured' && status) status.configured = false
    else error = e.message
  }

  async function makeDraft() {
    busy = true; error = ''; draft = null
    try {
      draft = await ai.draft(app.id, kind, prompt, catalog(kind, tables), change === 'current' && targetName && target?.kind === kind ? target.id : '')
    } catch (e: any) { fail(e) } finally { busy = false }
  }

  async function apply() {
    if (!draft) return
    busy = true; error = ''
    try {
      const where = await applyDraft(app, draft)
      toast.success('Applied')
      draft = null; prompt = ''; open = false
      reload.n++
      navigate(where)
    } catch (e: any) { error = e.message } finally { busy = false }
  }

  async function ask() {
    busy = true; error = ''; answer = null
    try { answer = await ai.ask(app.id, question, as) } catch (e: any) { fail(e) } finally { busy = false }
  }

  const asLabel = $derived.by(() => {
    if (!as) return 'You (every table and row)'
    if (as.startsWith('user:')) { const m = members.find((x) => 'user:' + x.userId === as); return m ? `${m.name} (${m.email})` : as }
    return `Someone with the ${roles.find((r) => r.id === as)?.name ?? as} role`
  })
  const callText = (c: AIAnswer['calls'][number]) => {
    const i = c.input ?? {}
    if (c.tool === 'summarize') return `${i.op}${i.field ? ' of ' + i.field : ''} in ${i.table}${i.groupBy ? ' by ' + i.groupBy : ''}${i.filter ? ' where ' + i.filter : ''}`
    return `rows of ${i.table}${i.filter ? ' where ' + i.filter : ''}${i.sort ? ' by ' + i.sort : ''}`
  }
</script>

<Sheet.Root bind:open>
  <Sheet.Content class="w-full gap-0 overflow-y-auto sm:max-w-xl">
    <Sheet.Header class="border-b">
      <Sheet.Title class="flex items-center gap-2"><SparklesIcon class="size-4 text-violet-500" />Assistant</Sheet.Title>
      <Sheet.Description>Describe what you need. Drafts are shown for review and change nothing until you apply them.</Sheet.Description>
      {#if status?.configured}<p class="text-muted-foreground text-xs" data-testid="ai-using">Using {status.providers.find((p) => p.id === status?.provider)?.name ?? status.provider} · <span class="font-mono">{status.model}</span></p>{/if}
    </Sheet.Header>
    <div class="flex flex-col gap-4 p-4">
      {#if !status}
        <div class="text-muted-foreground flex items-center gap-2 text-sm"><Spinner />Loading</div>
      {:else if !status.configured}
        <NoKey onNavigate={() => (open = false)} />
      {:else}
        <Tabs.Root bind:value={tab}>
          <Tabs.List class="w-full">
            <Tabs.Trigger value="build">Build</Tabs.Trigger>
            <Tabs.Trigger value="ask">Ask about data</Tabs.Trigger>
          </Tabs.List>

          <Tabs.Content value="build" class="mt-3 flex flex-col gap-3">
            {#if !draft}
              <ToggleGroup.Root type="single" variant="outline" size="sm" value={kind} onValueChange={(v) => { if (v) { kind = v as DraftKind; change = target?.kind === v ? change : 'new' } }} class="w-full">
                {#each kinds as k (k.value)}<ToggleGroup.Item value={k.value} class="flex-1">{k.label}</ToggleGroup.Item>{/each}
              </ToggleGroup.Root>
              {#if target && targetName && target.kind === kind && kind !== 'table'}
                <Select.Root type="single" bind:value={change}>
                  <Select.Trigger size="sm" class="w-full">{change === 'current' ? `Change “${targetName}”` : `A new ${kind}`}</Select.Trigger>
                  <Select.Content>
                    <Select.Item value="current" label={`Change “${targetName}”`}>Change “{targetName}”</Select.Item>
                    <Select.Item value="new" label={`A new ${kind}`}>A new {kind}</Select.Item>
                  </Select.Content>
                </Select.Root>
              {/if}
              <div class="flex flex-col gap-1.5">
                <Label for="ai-prompt">Describe it</Label>
                <Textarea id="ai-prompt" rows={5} bind:value={prompt} placeholder={kinds.find((k) => k.value === kind)?.example}
                  onkeydown={(e) => { if (e.key === 'Enter' && (e.metaKey || e.ctrlKey) && prompt.trim()) makeDraft() }} />
                <p class="text-muted-foreground text-xs">The assistant sees your app's tables, screens, queries, flows and roles, never row data or secret values.</p>
              </div>
              <Button onclick={makeDraft} disabled={busy || !prompt.trim()} class="self-start">
                {#if busy}<Spinner />Drafting…{:else}<SparklesIcon />Draft it{/if}
              </Button>
              {#if busy}<p class="text-muted-foreground text-xs">Drafts are checked against your app before you see them; this can take up to a minute.</p>{/if}
            {:else}
              <DraftReview {draft} {tables} />
              <div class="flex flex-wrap gap-2 border-t pt-3">
                <Button onclick={apply} disabled={busy}>{#if busy}<Spinner />{:else}<CheckIcon />{/if}Apply</Button>
                <Button variant="outline" onclick={makeDraft} disabled={busy}><RotateCcwIcon />Draft again</Button>
                <Button variant="ghost" onclick={() => (draft = null)} disabled={busy}>Discard</Button>
              </div>
            {/if}
          </Tabs.Content>

          <Tabs.Content value="ask" class="mt-3 flex flex-col gap-3">
            <div class="flex flex-col gap-1.5">
              <Label>Answer with the access of</Label>
              <Select.Root type="single" value={as || 'builder'} onValueChange={(v) => (as = v === 'builder' ? '' : v)}>
                <Select.Trigger size="sm" class="w-full">{asLabel}</Select.Trigger>
                <Select.Content>
                  <Select.Item value="builder" label="You">You (every table and row)</Select.Item>
                  <Select.Group><Select.GroupHeading>Members</Select.GroupHeading>
                    {#each members as m (m.userId)}<Select.Item value={'user:' + m.userId} label={m.name}>{m.name} <span class="text-muted-foreground text-xs">{roles.find((r) => r.id === m.role)?.name ?? m.role}</span></Select.Item>{/each}
                  </Select.Group>
                  <Select.Group><Select.GroupHeading>Roles</Select.GroupHeading>
                    {#each roles as r (r.id)}<Select.Item value={r.id} label={r.name}>Someone with the {r.name} role</Select.Item>{/each}
                  </Select.Group>
                </Select.Content>
              </Select.Root>
              <p class="text-muted-foreground text-xs">{as ? 'Table permissions, row rules and hidden links apply, as they would for them.' : 'As a builder you see every table and row. Pick a member or a role to check what they would see.'}</p>
            </div>
            <div class="flex gap-2">
              <Textarea rows={2} bind:value={question} placeholder="Which customers have overdue invoices, and how much do they owe?"
                onkeydown={(e) => { if (e.key === 'Enter' && !e.shiftKey && question.trim()) { e.preventDefault(); ask() } }} />
              <Button onclick={ask} disabled={busy || !question.trim()} class="self-end" aria-label="Ask">{#if busy}<Spinner />{:else}<SearchIcon />{/if}</Button>
            </div>
            {#if answer}
              <div class="ai-answer bg-muted/40 rounded-lg border p-3 text-sm">{@html markdown(answer.answer)}</div>
              {#if answer.calls.length}
                <details class="text-muted-foreground text-xs">
                  <summary class="cursor-pointer">How this was found ({answer.calls.length} {answer.calls.length === 1 ? 'lookup' : 'lookups'})</summary>
                  <ul class="mt-1.5 space-y-1 pl-4">{#each answer.calls as c, i (i)}<li class="font-mono">{callText(c)}{#if c.error}<span class="text-destructive"> · {c.error}</span>{/if}</li>{/each}</ul>
                </details>
              {/if}
            {/if}
          </Tabs.Content>
        </Tabs.Root>
        {#if error}<p class="text-destructive text-sm">{error}</p>{/if}
      {/if}
    </div>
  </Sheet.Content>
</Sheet.Root>

<style>
  .ai-answer :global(p + p), .ai-answer :global(p + ul), .ai-answer :global(ul + p), .ai-answer :global(table + p), .ai-answer :global(p + table) { margin-top: 0.5rem }
  .ai-answer :global(ul) { list-style: disc; padding-left: 1.25rem }
  .ai-answer :global(ol) { list-style: decimal; padding-left: 1.25rem }
  .ai-answer :global(table) { width: 100%; border-collapse: collapse; font-size: 0.8125rem }
  .ai-answer :global(th), .ai-answer :global(td) { border-bottom: 1px solid var(--border); padding: 0.25rem 0.5rem; text-align: left }
  .ai-answer :global(th) { font-weight: 600 }
  .ai-answer :global(code) { font-family: var(--font-mono, monospace); font-size: 0.8em }
</style>
