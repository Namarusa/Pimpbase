<script lang="ts">
  import { onDestroy } from 'svelte'
  import { toast } from 'svelte-sonner'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as Empty from '$lib/components/ui/empty'
  import * as Tabs from '$lib/components/ui/tabs'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import { Textarea } from '$lib/components/ui/textarea'
  import { Spinner } from '$lib/components/ui/spinner'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import EllipsisIcon from '@lucide/svelte/icons/ellipsis'
  import CheckIcon from '@lucide/svelte/icons/check'
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert'
  import PlayIcon from '@lucide/svelte/icons/play'
  import WorkflowIcon from '@lucide/svelte/icons/workflow'
  import { api, type App, type Flow, type TriggerType } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import AppNav from '../components/AppNav.svelte'
  import ConfirmHost from '../lib/screen/ConfirmHost.svelte'
  import StepList from '../lib/flows/StepList.svelte'
  import StepSettings from '../lib/flows/StepSettings.svelte'
  import TriggerSettings from '../lib/flows/TriggerSettings.svelte'
  import RunsView from '../lib/flows/RunsView.svelte'
  import { FlowEditor, setEditor } from '../lib/flows/editor.svelte'
  import { integrations, loadIntegrations, manifestOf } from '../lib/integrations/state.svelte'
  import { describeCron, normalize, triggerInfo, triggerTypes } from '../lib/flows/steps'

  // Flows is the automation builder: the app's flows on the left, the
  // chosen flow's trigger and steps on a canvas, their settings on the
  // right, and its run history. Flows open at /apps/<app>/flows/<id>.
  let { app: slug, item }: { app: string; item?: string } = $props()
  const ed = new FlowEditor()
  setEditor(ed)
  let app = $state<App | null>(null)
  let flows = $state<Flow[]>([])
  let error = $state('')
  let ready = $state(false)
  let confirmHost = $state<ConfirmHost>()
  let saving = $state<'saved' | 'saving' | 'error'>('saved')
  let saveError = $state('')
  let tab = $state<'editor' | 'runs'>('editor')
  let shownRun = $state('')
  let runsKey = $state(0)

  async function load() {
    try {
      const res = await api.app(slug)
      if (!res.builder) { error = 'Only builders can manage flows.'; return }
      app = res.app
      ed.app = res.app
      const [list, tables, queries, variables, roles, templates] = await Promise.all([api.flows(app.id), api.tables(app.id), api.queries(app.id), api.variables(app.id), api.roles(app.id), api.emailTemplates(app.id), loadIntegrations(app.id)])
      ed.roles = roles
      ed.emailTemplates = templates
      flows = list.map(normalize)
      ed.tables = tables
      ed.queries = queries
      ed.variables = variables
      ready = true
    } catch (e: any) { error = e.message }
  }
  load()

  const go = (id?: string) => navigate(`/apps/${slug}/flows${id ? '/' + id : ''}`)
  const flow = $derived(flows.find((f) => f.id === item) ?? null)
  $effect.pre(() => {
    if (ed.flow?.id !== flow?.id) { ed.selected = 'trigger'; ed.errorStep = ''; shownRun = ''; tab = 'editor' }
    const changed = ed.flow?.id !== flow?.id
    ed.flow = flow
    if (changed) { ed.samples = { run: null, fetched: {} }; if (flow) ed.loadSamples() }
  })
  $effect(() => { if (ready && !item && flows[0]) go(flows[0].id) })

  // Edits apply at once and save shortly after the last one.
  let timer: ReturnType<typeof setTimeout> | undefined
  let pendingId = ''
  ed.onChange = () => {
    if (!ed.flow) return
    pendingId = ed.flow.id
    saving = 'saving'
    clearTimeout(timer)
    timer = setTimeout(flush, 600)
  }
  async function flush() {
    clearTimeout(timer)
    const id = pendingId
    pendingId = ''
    const f = flows.find((x) => x.id === id)
    if (!f || !app) return
    try {
      const { name, enabled, trigger, steps } = $state.snapshot(f) as Flow
      await api.updateFlow(app.id, id, { name, enabled, trigger, steps })
      ed.errorStep = ''
      if (!pendingId) saving = 'saved'
    } catch (e: any) {
      saving = 'error'
      saveError = e.message
      ed.errorStep = e.body?.step && e.body.step !== 'trigger' ? e.body.step : ''
    }
  }
  onDestroy(flush)

  // New flows.
  let adding = $state(false)
  let newName = $state('')
  let newTrigger = $state<TriggerType>('row_created')
  let addError = $state('')
  function startAdd() {
    let n = 'New flow'
    for (let i = 2; flows.some((f) => f.name.toLowerCase() === n.toLowerCase()); i++) n = `New flow ${i}`
    newName = n
    addError = ''
    adding = true
  }
  async function create(e: SubmitEvent) {
    e.preventDefault()
    if (!app) return
    await flush()
    const trigger: Flow['trigger'] = { type: newTrigger, role: 'BASIC' }
    if (newTrigger.startsWith('row_')) trigger.table = ed.tables[0]?.id
    if (newTrigger === 'integration') {
      const c = integrations.connections.find((x) => manifestOf(x.kind)?.webhook)
      trigger.connection = c?.id
      trigger.events = []
    }
    if (newTrigger === 'schedule') { trigger.cron = '0 8 * * 1-5'; trigger.timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' }
    try {
      const f = normalize(await api.createFlow(app.id, { name: newName, trigger, steps: [] }))
      flows = [...flows, f].sort((a, b) => a.name.localeCompare(b.name))
      adding = false
      go(f.id)
    } catch (err: any) { addError = err.message }
  }
  async function duplicateFlow(f: Flow) {
    if (!app) return
    await flush()
    let n = `${f.name} copy`
    for (let i = 2; flows.some((x) => x.name.toLowerCase() === n.toLowerCase()); i++) n = `${f.name} copy ${i}`
    const { trigger, steps } = $state.snapshot(f) as Flow
    try {
      const c = normalize(await api.createFlow(app.id, { name: n, trigger, steps }))
      flows = [...flows, c].sort((a, b) => a.name.localeCompare(b.name))
      go(c.id)
    } catch (e: any) { toast.error(e.message) }
  }
  async function deleteFlow(f: Flow) {
    if (!app || !(await confirmHost?.ask(`Delete the flow “${f.name}”?`, 'Its run history is deleted too. Buttons that run it stop working.'))) return
    try {
      await api.deleteFlow(app.id, f.id)
      flows = flows.filter((x) => x.id !== f.id)
      if (item === f.id) go()
    } catch (e: any) { toast.error(e.message) }
  }
  function rename(v: string) {
    if (!flow) return
    flow.name = v
    ed.changed()
  }
  function toggle(v: boolean) {
    if (!flow) return
    flow.enabled = v
    ed.changed()
    flush()
  }

  // Tests run the saved flow with sample data.
  let testing = $state(false)
  let testOpen = $state(false)
  let testData = $state('')
  let testError = $state('')
  // dryRun previews emails in the run log instead of sending them.
  let dryRun = $state(true)
  const sendsEmail = $derived.by(() => { let any = false; const w = (l: any[]) => l?.forEach((x) => { if (x.type === 'send_email' && !x.disabled) any = true; w(x.then); w(x.else); w(x.do) }); w(flow?.steps ?? []); return any })
  function openTest() {
    if (!flow) return
    testError = ''
    const t = flow.trigger
    if (t.type === 'manual') testData = JSON.stringify({ params: Object.fromEntries((t.parameters ?? []).filter((p) => p.name).map((p) => [p.name, p.type === 'number' ? Number(p.default) || 0 : p.default])) }, null, 2)
    else if (t.type === 'webhook') testData = JSON.stringify({ body: { name: 'Ada' }, query: {} }, null, 2)
    else testData = ''
    testOpen = true
  }
  async function runTest() {
    if (!app || !flow) return
    let data: Record<string, any> | undefined
    if (testData.trim()) {
      try { data = JSON.parse(testData) } catch (e: any) { testError = 'Not valid JSON: ' + e.message; return }
    }
    await flush()
    if (saving === 'error') { testError = 'Fix the flow first: ' + saveError; return }
    testing = true
    try {
      const run = await api.testFlow(app.id, flow.id, data, sendsEmail && dryRun)
      ed.useRun(run)
      testOpen = false
      shownRun = run.id
      runsKey++
      tab = 'runs'
      if (run.status === 'failed') toast.error(run.error || 'The test failed')
      else if (run.status === 'succeeded') toast.success('The test run succeeded')
    } catch (e: any) { testError = e.message } finally { testing = false }
  }

  const triggerSummary = (f: Flow) => {
    const t = f.trigger
    if (t.type.startsWith('row_')) return `${triggerInfo(t.type).label} in ${ed.table(t.table)?.name ?? '…'}`
    if (t.type === 'schedule') return `${describeCron(t.cron)} (${t.timezone})`
    if (t.type === 'integration') {
      const c = integrations.connections.find((x) => x.id === t.connection)
      return c ? `${c.name}: ${t.events?.length ? t.events.join(', ') : 'any event'}` : 'Service event'
    }
    if (t.type === 'webhook') return t.respond ? 'Webhook, answers with the result' : 'Webhook call'
    return `Button · ${(t.parameters ?? []).length} parameter${(t.parameters ?? []).length === 1 ? '' : 's'}`
  }
  const FlowTrigger = $derived(triggerInfo(flow?.trigger.type ?? 'manual'))
  const selectedStep = $derived(ed.selected !== 'trigger' ? ed.step(ed.selected) : undefined)
  function keydown(e: KeyboardEvent) {
    const t = e.target as HTMLElement
    if (t.closest('input, textarea, select, [contenteditable], [role=dialog], [role=listbox], [role=menu]')) return
    if ((e.key === 'Delete' || e.key === 'Backspace') && selectedStep) { e.preventDefault(); ed.remove(selectedStep.id) }
    else if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'd' && selectedStep) { e.preventDefault(); ed.duplicate(selectedStep.id) }
  }
</script>

<svelte:window onkeydown={keydown} />

<div class="flex h-full flex-col">
  {#if app}<AppNav {app} active="flows" />{/if}
  {#if error}
    <p class="text-destructive p-6 text-sm">{error}</p>
  {:else if ready && app}
    <div class="flex min-h-0 flex-1">
      <aside class="bg-card flex w-64 shrink-0 flex-col border-r">
        <div class="flex items-center justify-between px-3 pt-3 pb-1">
          <span class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">Flows</span>
          <Button variant="ghost" size="icon" class="size-7" onclick={startAdd} aria-label="New flow"><PlusIcon /></Button>
        </div>
        <div class="flex flex-1 flex-col gap-0.5 overflow-y-auto px-2 pb-3">
          {#each flows as f (f.id)}
            {@const ti = triggerInfo(f.trigger.type)}
            <div class={['group flex items-center rounded-md', item === f.id ? 'bg-primary/10 text-primary' : 'hover:bg-muted']}>
              <button type="button" class="flex min-w-0 flex-1 items-center gap-2.5 px-2 py-1.5 text-left" onclick={() => go(f.id)}>
                <span class="relative"><ti.icon class="size-4 opacity-70" /><span class={['ring-card absolute -right-0.5 -bottom-0.5 size-2 rounded-full ring-2', f.enabled ? 'bg-emerald-500' : 'bg-muted-foreground/40']}></span></span>
                <span class="flex min-w-0 flex-col"><span class="truncate text-sm font-medium">{f.name}</span><span class="text-muted-foreground truncate text-[11px]">{triggerSummary(f)}</span></span>
              </button>
              <DropdownMenu.Root>
                <DropdownMenu.Trigger>
                  {#snippet child({ props })}<Button {...props} variant="ghost" size="icon" class="mr-1 size-6 opacity-0 group-hover:opacity-100 data-[state=open]:opacity-100" aria-label="Flow actions"><EllipsisIcon /></Button>{/snippet}
                </DropdownMenu.Trigger>
                <DropdownMenu.Content align="start">
                  <DropdownMenu.Item onclick={() => duplicateFlow(f)}>Duplicate</DropdownMenu.Item>
                  <DropdownMenu.Separator />
                  <DropdownMenu.Item variant="destructive" onclick={() => deleteFlow(f)}>Delete</DropdownMenu.Item>
                </DropdownMenu.Content>
              </DropdownMenu.Root>
            </div>
          {/each}
        </div>
        <div class="text-muted-foreground flex h-8 items-center gap-1 border-t px-3 text-xs">
          {#if saving === 'saving'}Saving…{:else if saving === 'error'}<CircleAlertIcon class="text-destructive size-3.5 shrink-0" /><span class="text-destructive truncate" title={saveError}>Not saved: {saveError}</span>{:else}<CheckIcon class="size-3.5" />Saved{/if}
        </div>
      </aside>

      {#if flow && ed.flow}
        <section class="flex min-w-0 flex-1 flex-col">
          <div class="bg-card flex h-12 shrink-0 items-center gap-3 border-b px-4">
            <Input class="h-8 max-w-72 border-transparent px-1.5 text-sm font-semibold shadow-none hover:border-input focus-visible:border-ring" value={flow.name} oninput={(e) => rename(e.currentTarget.value)} aria-label="Flow name" />
            <Tabs.Root value={tab} onValueChange={(v) => (tab = v as 'editor' | 'runs')}>
              <Tabs.List class="h-8"><Tabs.Trigger value="editor" class="text-xs">Editor</Tabs.Trigger><Tabs.Trigger value="runs" class="text-xs">Runs</Tabs.Trigger></Tabs.List>
            </Tabs.Root>
            <div class="ml-auto flex items-center gap-3">
              <label class="flex items-center gap-2 text-sm"><Switch checked={flow.enabled} onCheckedChange={toggle} />{flow.enabled ? 'On' : 'Off'}</label>
              <Button size="sm" onclick={openTest}><PlayIcon />Test</Button>
            </div>
          </div>
          {#if tab === 'editor'}
            <div class="flex min-h-0 flex-1">
              <div class="bb-flow-canvas min-w-0 flex-1 overflow-auto" role="presentation" onclick={(e) => { if (e.target === e.currentTarget) ed.selected = 'trigger' }}>
                <div class="flex min-w-max flex-col items-center px-10 py-8">
                  <button type="button" onclick={() => (ed.selected = 'trigger')}
                    class={['bg-card flex w-76 items-start gap-3 rounded-xl border-2 p-3 text-left shadow-sm transition-all', ed.selected === 'trigger' ? 'border-primary ring-primary/20 ring-[3px]' : 'border-primary/30 hover:border-primary/60']}>
                    <span class="bg-primary text-primary-foreground grid size-9 shrink-0 place-items-center rounded-lg"><FlowTrigger.icon class="size-4.5" /></span>
                    <span class="flex min-w-0 flex-col gap-0.5">
                      <span class="text-primary text-[11px] font-semibold tracking-wider uppercase">When</span>
                      <span class="text-sm font-semibold">{FlowTrigger.label}</span>
                      <span class="text-foreground/80 truncate text-xs">{triggerSummary(flow)}</span>
                    </span>
                  </button>
                  <StepList steps={flow.steps} />
                </div>
              </div>
              <aside class="bg-card w-96 shrink-0 overflow-y-auto border-l">
                {#if selectedStep}
                  {#key selectedStep.id}<StepSettings step={selectedStep} />{/key}
                {:else}
                  <TriggerSettings />
                {/if}
              </aside>
            </div>
          {:else}
            <div class="min-h-0 flex-1"><RunsView bind:selected={shownRun} reloadKey={runsKey} /></div>
          {/if}
        </section>
      {:else}
        <Empty.Root class="flex-1">
          <Empty.Header>
            <Empty.Media variant="icon"><WorkflowIcon /></Empty.Media>
            <Empty.Title>Automate your app</Empty.Title>
            <Empty.Description>A flow starts when a row changes, on a schedule, from a webhook or from a button, then runs steps: change rows, call APIs, send emails, make PDFs.</Empty.Description>
          </Empty.Header>
          <Empty.Content><Button onclick={startAdd}><PlusIcon />New flow</Button></Empty.Content>
        </Empty.Root>
      {/if}
    </div>
  {:else}
    <p class="text-muted-foreground p-6 text-sm">Loading…</p>
  {/if}
</div>

<Dialog.Root bind:open={adding}>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header><Dialog.Title>New flow</Dialog.Title><Dialog.Description>Choose what starts it. You can change everything later.</Dialog.Description></Dialog.Header>
    <form id="new-flow" class="flex flex-col gap-4" onsubmit={create}>
      <div class="flex flex-col gap-1.5"><Label for="flow-name">Name</Label><Input id="flow-name" bind:value={newName} required /></div>
      <div class="grid grid-cols-2 gap-2">
        {#each triggerTypes as t (t.type)}
          <button type="button" onclick={() => (newTrigger = t.type)}
            class={['flex items-start gap-2.5 rounded-lg border p-2.5 text-left transition-colors', newTrigger === t.type ? 'border-primary bg-primary/5' : 'hover:bg-muted']}>
            <t.icon class={['mt-0.5 size-4 shrink-0', newTrigger === t.type ? 'text-primary' : 'text-muted-foreground']} />
            <span class="flex flex-col"><span class="text-sm font-medium">{t.label}</span><span class="text-muted-foreground text-xs">{t.description}</span></span>
          </button>
        {/each}
      </div>
      {#if addError}<p class="text-destructive text-sm">{addError}</p>{/if}
    </form>
    <Dialog.Footer><Button variant="outline" onclick={() => (adding = false)}>Cancel</Button><Button type="submit" form="new-flow">Create</Button></Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={testOpen}>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header>
      <Dialog.Title>Test {flow?.name}</Dialog.Title>
      <Dialog.Description>
        The test runs the flow now, even when it is off, with the development values of app variables. Its steps really happen: rows change{sendsEmail && dryRun ? '' : ' and emails go out'}.
      </Dialog.Description>
    </Dialog.Header>
    {#if sendsEmail}
      <div class="bg-muted/40 flex items-start gap-2.5 rounded-lg border p-3">
        <Switch id="dry-run" checked={dryRun} onCheckedChange={(v) => (dryRun = v)} class="mt-0.5" />
        <Label for="dry-run" class="flex flex-col items-start gap-0.5 text-sm font-normal">
          <span class="font-medium">Only preview emails</span>
          <span class="text-muted-foreground text-xs">Send email steps fill in their email and show it in the run log, without sending it.</span>
        </Label>
      </div>
    {/if}
    <div class="flex flex-col gap-1.5">
      <Label for="test-data">Trigger data (JSON)</Label>
      <Textarea id="test-data" rows={9} class="font-mono text-xs" bind:value={testData}
        placeholder={flow?.trigger.type.startsWith('row_') ? 'Leave empty to use the newest row of the table, or write {"row": {…}}' : 'Leave empty for sample data'} />
      {#if testError}<p class="text-destructive text-sm">{testError}</p>{/if}
    </div>
    <Dialog.Footer>
      <Button variant="outline" onclick={() => (testOpen = false)}>Cancel</Button>
      <Button onclick={runTest} disabled={testing}>{#if testing}<Spinner />{:else}<PlayIcon />{/if}Run test</Button>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
<ConfirmHost bind:this={confirmHost} />

<style>
  .bb-flow-canvas {
    background-color: var(--muted);
    background-image: radial-gradient(color-mix(in oklab, var(--foreground) 14%, transparent) 1px, transparent 1px);
    background-size: 18px 18px;
  }
  :global(.dark) .bb-flow-canvas { background-color: color-mix(in oklab, var(--background) 70%, var(--muted)); }
</style>
