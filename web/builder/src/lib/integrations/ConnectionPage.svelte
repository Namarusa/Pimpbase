<script lang="ts">
  import { toast } from 'svelte-sonner'
  import * as Tabs from '$lib/components/ui/tabs'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Textarea } from '$lib/components/ui/textarea'
  import { Spinner } from '$lib/components/ui/spinner'
  import CopyIcon from '@lucide/svelte/icons/copy'
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw'
  import PlugZapIcon from '@lucide/svelte/icons/plug-zap'
  import TrashIcon from '@lucide/svelte/icons/trash'
  import SaveIcon from '@lucide/svelte/icons/save'
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link'
  import FlaskConicalIcon from '@lucide/svelte/icons/flask-conical'
  import ShieldCheckIcon from '@lucide/svelte/icons/shield-check'
  import CreditCardIcon from '@lucide/svelte/icons/credit-card'
  import KeyRoundIcon from '@lucide/svelte/icons/key-round'
  import { ApiError, api, type App, type Connection, type IntegrationEvent } from '../api'
  import { navigate } from '../router.svelte'
  import { timeAgo } from '../format'
  import type ConfirmHost from '../screen/ConfirmHost.svelte'
  import IntegrationIcon from './IntegrationIcon.svelte'
  import TryAction from './TryAction.svelte'
  import SyncCard from './SyncCard.svelte'
  import { integrations, keyMode, manifestOf } from './state.svelte'

  // ConnectionPage edits one connection: its name and keys (live, and
  // optional ones for test runs), its webhook address and the events it
  // received, a place to try its actions, and its syncs.
  let { app, conn, confirm, onDeleted }: { app: App; conn: Connection; confirm?: ConfirmHost; onDeleted: () => void } = $props()
  const m = $derived(manifestOf(conn.kind))
  let tab = $state('keys')

  // The form holds what was typed; secrets left empty keep their value.
  let name = $state(conn.name)
  let settings = $state<Record<string, string>>({ ...conn.settings })
  let testSettings = $state<Record<string, string>>({ ...conn.testSettings })
  let secrets = $state<Record<string, string>>({})
  let testSecrets = $state<Record<string, string>>({})
  let saving = $state(false)
  let showAdvanced = $state(false)
  const dirty = $derived(name !== conn.name || Object.values(secrets).some(Boolean) || Object.values(testSecrets).some(Boolean)
    || JSON.stringify(settings) !== JSON.stringify(conn.settings) || JSON.stringify(testSettings) !== JSON.stringify(conn.testSettings))
  const creds = $derived((m?.credentials ?? []).filter((c) => showAdvanced || !c.advanced))
  const advancedCount = $derived((m?.credentials ?? []).filter((c) => c.advanced).length)

  function replace(c: Connection) {
    integrations.connections = integrations.connections.map((x) => (x.id === c.id ? c : x))
  }
  async function save() {
    if (saving) return
    saving = true
    try {
      const c = await api.updateConnection(app.id, conn.id, { name, settings, testSettings, secrets: clean(secrets), testSecrets: clean(testSecrets) })
      replace(c)
      name = c.name
      settings = { ...c.settings }
      testSettings = { ...c.testSettings }
      secrets = {}
      testSecrets = {}
      toast.success('Saved')
    } catch (e: any) { toast.error(e.message) } finally { saving = false }
  }
  const clean = (o: Record<string, string>) => Object.fromEntries(Object.entries(o).filter(([, v]) => v.trim()))
  async function forget(key: string, test: boolean) {
    try {
      const c = await api.updateConnection(app.id, conn.id, test ? { testSecrets: { [key]: '' } } : { secrets: { [key]: '' } })
      replace(c)
    } catch (e: any) { toast.error(e.message) }
  }

  let testing = $state<'' | 'live' | 'test'>('')
  let tested = $state<{ env: string; ok: boolean; text: string } | null>(null)
  async function test(env: 'live' | 'test') {
    if (dirty) await save()
    testing = env
    try {
      const out = await api.testConnection2(app.id, conn.id, env)
      const extra = Object.entries(out).filter(([k]) => k !== 'ok').map(([k, v]) => `${k}: ${typeof v === 'object' ? JSON.stringify(v) : v}`).join(' · ')
      tested = { env, ok: true, text: `${m?.name} answered${extra ? ' (' + extra + ')' : ''}.` }
    } catch (e: any) { tested = { env, ok: false, text: e.message } } finally { testing = '' }
  }
  const hasTest = $derived(Object.values(conn.secrets).some((s) => s.test) || Object.values(conn.testSettings).some(Boolean))

  async function remove() {
    if (!(await confirm?.ask(`Delete the connection “${conn.name}”?`, 'Its keys are forgotten and its webhook address stops working. Synced tables stay.'))) return
    try {
      await api.deleteConnection(app.id, conn.id)
    } catch (e: any) {
      if (!(e instanceof ApiError && e.status === 409)) { toast.error(e.message); return }
      if (!(await confirm?.ask('Flows still use it', `${e.message}. Delete it anyway? Those steps will fail until you choose another connection.`))) return
      try { await api.deleteConnection(app.id, conn.id, true) } catch (e2: any) { toast.error(e2.message); return }
    }
    integrations.connections = integrations.connections.filter((x) => x.id !== conn.id)
    onDeleted()
  }

  // Webhook.
  let events = $state<IntegrationEvent[]>([])
  let loadingEvents = $state(false)
  async function loadEvents() {
    loadingEvents = true
    try { events = await api.connectionEvents(app.id, conn.id) } catch { events = [] } finally { loadingEvents = false }
  }
  $effect(() => { if (tab === 'events') loadEvents() })

  // Syncs show the names of the tables they fill.
  let tables = $state<{ id: string; name: string }[]>([])
  async function loadTables() { try { tables = (await api.tables(app.id)).map((t) => ({ id: t.id, name: t.name })) } catch {} }
  $effect(() => { if (tab === 'syncs') loadTables() })
  async function rotate() {
    if (!(await confirm?.ask('Make a new webhook address?', `The old one stops working at once; paste the new one in ${m?.name}.`))) return
    try { replace(await api.newConnectionHook(app.id, conn.id)); toast.success('New address made') } catch (e: any) { toast.error(e.message) }
  }
  const copy = (s: string) => { navigator.clipboard?.writeText(s); toast.success('Copied') }
  const statusTone: Record<string, string> = {
    handled: 'bg-emerald-500/12 text-emerald-700 dark:text-emerald-300', failed: 'bg-destructive/12 text-destructive',
    rejected: 'bg-amber-500/15 text-amber-700 dark:text-amber-300', received: 'bg-muted text-muted-foreground',
  }
  const secretCred = $derived(m?.webhook?.verify.secret ? m.credentials.find((c) => c.key === m!.webhook!.verify.secret) : undefined)

  // Stripe: a ready-made payment flow for a Take a payment button.
  let payOpen = $state(false)
  let pay = $state({ product: 'Order', amount: 25, currency: 'eur', name: 'Take payment' })
  let payError = $state('')
  async function makePaymentFlow(e: SubmitEvent) {
    e.preventDefault()
    try {
      const f = await api.paymentFlow(app.id, conn.id, pay)
      payOpen = false
      toast.success(`Made the flow “${f.name}”`, { description: 'Add a button with the Take a payment action to a screen and choose this flow.' })
      navigate(`/apps/${app.slug}/flows/${f.id}`)
    } catch (err: any) { payError = err.message }
  }
</script>

{#if m}
  <section class="flex min-w-0 flex-1 flex-col">
    <div class="bg-card flex h-14 shrink-0 items-center gap-3 border-b px-4">
      <IntegrationIcon manifest={m} />
      <div class="min-w-0">
        <div class="truncate text-sm font-semibold">{conn.name}</div>
        <div class="text-muted-foreground text-xs">{m.name} · {m.category}</div>
      </div>
      <div class="ml-auto flex items-center gap-2">
        {#if m.docs}<Button variant="ghost" size="sm" href={m.docs} target="_blank"><ExternalLinkIcon />API docs</Button>{/if}
        {#if m.id === 'stripe'}<Button variant="outline" size="sm" onclick={() => { payError = ''; payOpen = true }}><CreditCardIcon />Make a payment flow</Button>{/if}
        <Button variant="ghost" size="icon" class="size-8" aria-label="Delete the connection" onclick={remove}><TrashIcon /></Button>
      </div>
    </div>
    <Tabs.Root bind:value={tab} class="flex min-h-0 flex-1 flex-col gap-0">
      <div class="bg-card border-b px-4">
        <Tabs.List class="h-10 bg-transparent p-0">
          <Tabs.Trigger value="keys" class="text-xs">Keys</Tabs.Trigger>
          <Tabs.Trigger value="actions" class="text-xs">Actions ({m.actions.length})</Tabs.Trigger>
          {#if m.webhook}<Tabs.Trigger value="events" class="text-xs">Events</Tabs.Trigger>{/if}
          {#if m.syncs?.length}<Tabs.Trigger value="syncs" class="text-xs">Sync to tables</Tabs.Trigger>{/if}
        </Tabs.List>
      </div>

      <Tabs.Content value="keys" class="min-h-0 flex-1 overflow-y-auto">
        <div class="mx-auto flex max-w-3xl flex-col gap-5 p-6">
          {#if m.guide}<div class="bg-muted/40 rounded-lg border p-3 text-sm whitespace-pre-line">{m.guide}</div>{/if}
          <div class="flex flex-col gap-1.5">
            <Label for="conn-name" class="text-xs">Name</Label>
            <Input id="conn-name" class="h-8 max-w-sm" bind:value={name} />
            <p class="text-muted-foreground text-xs">Flow steps pick the connection by this name.</p>
          </div>
          <div class="bg-card overflow-hidden rounded-xl border">
            <div class="text-muted-foreground grid grid-cols-[180px_1fr_1fr] gap-3 border-b px-4 py-2 text-[11px] font-semibold tracking-wide uppercase">
              <span>Setting</span><span>Live</span><span class="flex items-center gap-1"><FlaskConicalIcon class="size-3" />Test runs <span class="font-normal normal-case">(optional)</span></span>
            </div>
            {#each creds as c (c.key)}
              <div class="grid grid-cols-[180px_1fr_1fr] items-start gap-3 border-b px-4 py-3 last:border-b-0">
                <div class="flex flex-col gap-0.5">
                  <span class="flex items-center gap-1 text-sm font-medium">{#if c.secret}<KeyRoundIcon class="text-muted-foreground size-3.5" />{/if}{c.label}</span>
                  {#if c.required}<span class="text-destructive text-[10px] font-medium">required</span>{/if}
                  {#if c.help}<span class="text-muted-foreground text-xs">{c.help}</span>{/if}
                </div>
                {#each [false, true] as isTest (isTest)}
                  <div class="flex flex-col gap-1">
                    {#if c.secret}
                      {@const saved = conn.secrets[c.key]?.[isTest ? 'test' : 'live']}
                      {@const hint = conn.hints?.[c.key]?.[isTest ? 'test' : 'live']}
                      {@const bag = isTest ? testSecrets : secrets}
                      {#if c.long}
                        <Textarea class="min-h-20 font-mono text-xs" value={bag[c.key] ?? ''} placeholder={saved ? 'Saved; paste a new one to replace it' : c.placeholder}
                          oninput={(e) => (bag[c.key] = e.currentTarget.value)} />
                      {:else}
                        <Input type="password" autocomplete="off" class="h-8 font-mono text-xs" value={bag[c.key] ?? ''} placeholder={saved ? (hint || 'Saved') : (c.placeholder ?? '')}
                          oninput={(e) => (bag[c.key] = e.currentTarget.value)} />
                      {/if}
                      {#if saved}
                        <span class="text-muted-foreground flex items-center gap-1.5 text-[11px]"><ShieldCheckIcon class="size-3 text-emerald-600" />Saved{hint ? ` (${hint})` : ''}
                          <button type="button" class="hover:text-destructive underline" onclick={() => forget(c.key, isTest)}>remove</button></span>
                      {:else if isTest}
                        <span class="text-muted-foreground text-[11px]">Empty: test runs use the live value.</span>
                      {/if}
                    {:else}
                      {@const bag = isTest ? testSettings : settings}
                      <Input class="h-8 text-xs" value={bag[c.key] ?? ''} placeholder={isTest ? (settings[c.key] || c.placeholder || c.default || '') : (c.placeholder ?? c.default ?? '')}
                        oninput={(e) => (bag[c.key] = e.currentTarget.value)} />
                    {/if}
                  </div>
                {/each}
              </div>
            {/each}
          </div>
          {#if advancedCount}<button type="button" class="text-muted-foreground hover:text-foreground self-start text-xs font-medium" onclick={() => (showAdvanced = !showAdvanced)}>{showAdvanced ? 'Hide' : 'Show'} advanced settings ({advancedCount})</button>{/if}
          <div class="flex flex-wrap items-center gap-2">
            <Button size="sm" disabled={!dirty || saving} onclick={save}>{#if saving}<Spinner />{:else}<SaveIcon />{/if}Save</Button>
            <Button size="sm" variant="outline" disabled={!!testing} onclick={() => test('live')}>{#if testing === 'live'}<Spinner />{:else}<PlugZapIcon />{/if}Test the live keys</Button>
            {#if hasTest}<Button size="sm" variant="outline" disabled={!!testing} onclick={() => test('test')}>{#if testing === 'test'}<Spinner />{:else}<FlaskConicalIcon />{/if}Test the test keys</Button>{/if}
            {#if keyMode(conn, 'live') === 'test'}<Badge variant="secondary" class="text-[11px]">the live key is a test-mode key</Badge>{/if}
          </div>
          {#if tested}
            <div class={['rounded-lg border px-3 py-2 text-sm', tested.ok ? 'border-emerald-500/40 bg-emerald-500/8' : 'border-destructive/40 bg-destructive/8 text-destructive']}>
              <strong>{tested.env === 'test' ? 'Test keys' : 'Live keys'}:</strong> {tested.ok ? 'connected. ' : ''}{tested.text}
            </div>
          {/if}
          <p class="text-muted-foreground text-xs">Secrets never go back to the browser and are hidden from run logs. They are kept in the database like secret app variables, so protect its backups. Test runs from the flow editor and the Try panel use the test values; switched-on flows use the live ones.</p>
        </div>
      </Tabs.Content>

      <Tabs.Content value="actions" class="min-h-0 flex-1 overflow-y-auto">
        <TryAction {app} {conn} manifest={m} />
      </Tabs.Content>

      {#if m.webhook}
        <Tabs.Content value="events" class="min-h-0 flex-1 overflow-y-auto">
          <div class="mx-auto flex max-w-3xl flex-col gap-5 p-6">
            <div class="flex flex-col gap-1.5">
              <Label class="text-xs">Webhook address</Label>
              <div class="flex gap-1.5">
                <Input readonly class="h-8 font-mono text-[11px]" value={conn.hookUrl ?? ''} onfocus={(e) => e.currentTarget.select()} />
                <Button variant="outline" size="icon" class="size-8 shrink-0" aria-label="Copy the address" onclick={() => copy(conn.hookUrl ?? '')}><CopyIcon /></Button>
                <Button variant="ghost" size="sm" class="h-8 shrink-0 text-xs" onclick={rotate}><RefreshCwIcon />New address</Button>
              </div>
              {#if m.webhook.setup}<p class="text-muted-foreground text-xs">{m.webhook.setup}</p>{/if}
              {#if secretCred}
                {@const has = conn.secrets[secretCred.key]?.live || conn.secrets[secretCred.key]?.test}
                <p class={['flex items-center gap-1.5 text-xs', has ? 'text-emerald-700 dark:text-emerald-400' : 'text-amber-700 dark:text-amber-400']}>
                  <ShieldCheckIcon class="size-3.5" />{has ? `Events are checked with the ${secretCred.label.toLowerCase()}; others are refused.` : `Add the ${secretCred.label.toLowerCase()} under Keys: until then every event is refused.`}
                </p>
              {:else}
                <p class="text-muted-foreground flex items-center gap-1.5 text-xs"><ShieldCheckIcon class="size-3.5" />Events are checked with the connection's keys; others are refused.</p>
              {/if}
              <p class="text-muted-foreground text-xs">Each event is handled once, even when {m.name} sends it again. Start flows from it with the <strong>Service event</strong> trigger.</p>
            </div>
            <div class="flex flex-col gap-2">
              <div class="flex items-center">
                <h3 class="text-sm font-semibold">Received events</h3>
                <Button variant="ghost" size="sm" class="ml-auto h-7 text-xs" onclick={loadEvents} disabled={loadingEvents}><RefreshCwIcon class={loadingEvents ? 'animate-spin' : ''} />Refresh</Button>
              </div>
              {#if events.length}
                <div class="bg-card overflow-hidden rounded-xl border">
                  {#each events as ev (ev.id + ev.receivedAt)}
                    <div class="flex items-center gap-3 border-b px-3 py-2 last:border-b-0">
                      <span class={['rounded px-1.5 py-0.5 text-[10px] font-semibold uppercase', statusTone[ev.status]]}>{ev.status}</span>
                      <span class="min-w-0 flex-1">
                        <span class="block truncate font-mono text-xs">{ev.event || '(no type)'}</span>
                        <span class="text-muted-foreground block truncate text-[11px]">{ev.detail || ev.id}</span>
                      </span>
                      <span class="text-muted-foreground shrink-0 text-[11px]">{timeAgo(ev.receivedAt)}</span>
                    </div>
                  {/each}
                </div>
              {:else}
                <p class="text-muted-foreground rounded-lg border border-dashed p-4 text-center text-sm">{loadingEvents ? 'Loading…' : `Nothing yet. Events ${m.name} sends to the address above show here, with refused ones.`}</p>
              {/if}
              <p class="text-muted-foreground text-xs">The last 7 days are kept, up to 5000 events.</p>
            </div>
          </div>
        </Tabs.Content>
      {/if}

      {#if m.syncs?.length}
        <Tabs.Content value="syncs" class="min-h-0 flex-1 overflow-y-auto">
          <div class="mx-auto flex max-w-3xl flex-col gap-3 p-6">
            <p class="text-muted-foreground text-sm">A sync copies {m.name} data into a table of this app, and keeps it up to date: on a schedule{m.webhook ? ', and at once from events' : ''}. Rows {m.name} no longer has are removed. Screens and flows then read the table like any other.</p>
            {#each m.syncs as s (s.key)}
              <SyncCard {app} {conn} manifest={m} sync={s} {tables} onSaved={replace} onRan={loadTables} />
            {/each}
          </div>
        </Tabs.Content>
      {/if}
    </Tabs.Root>
  </section>

  <Dialog.Root bind:open={payOpen}>
    <Dialog.Content class="sm:max-w-md">
      <Dialog.Header>
        <Dialog.Title>Make a payment flow</Dialog.Title>
        <Dialog.Description>A flow that opens a Stripe Checkout page for a fixed price. Add a button with the <strong>Take a payment</strong> action to a screen to run it; the buyer comes back to the app when they have paid.</Dialog.Description>
      </Dialog.Header>
      <form id="pay-flow" class="flex flex-col gap-3" onsubmit={makePaymentFlow}>
        <div class="flex flex-col gap-1.5"><Label for="pay-product">What they pay for</Label><Input id="pay-product" bind:value={pay.product} required /></div>
        <div class="grid grid-cols-2 gap-3">
          <div class="flex flex-col gap-1.5"><Label for="pay-amount">Price</Label><Input id="pay-amount" type="number" min="0.5" step="0.01" bind:value={pay.amount} required /></div>
          <div class="flex flex-col gap-1.5"><Label for="pay-cur">Currency</Label><Input id="pay-cur" bind:value={pay.currency} maxlength={3} required /></div>
        </div>
        <div class="flex flex-col gap-1.5"><Label for="pay-name">Flow name</Label><Input id="pay-name" bind:value={pay.name} /></div>
        <p class="text-muted-foreground text-xs">The price is fixed in the flow, not in the button, so nobody can change what they pay. Mark orders as paid with a Service event flow on <code class="font-mono">checkout.session.completed</code>.</p>
        {#if payError}<p class="text-destructive text-sm">{payError}</p>{/if}
      </form>
      <Dialog.Footer><Button variant="outline" onclick={() => (payOpen = false)}>Cancel</Button><Button type="submit" form="pay-flow">Make the flow</Button></Dialog.Footer>
    </Dialog.Content>
  </Dialog.Root>
{:else}
  <p class="text-destructive p-6 text-sm">The integration “{conn.kind}” is no longer installed on this server.</p>
{/if}
