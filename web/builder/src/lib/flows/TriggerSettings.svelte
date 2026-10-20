<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import * as Popover from '$lib/components/ui/popover'
  import * as Command from '$lib/components/ui/command'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import { Checkbox } from '$lib/components/ui/checkbox'
  import { Separator } from '$lib/components/ui/separator'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import CopyIcon from '@lucide/svelte/icons/copy'
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw'
  import ChevronDownIcon from '@lucide/svelte/icons/chevron-down'
  import { toast } from 'svelte-sonner'
  import { api, type FlowParam, type ParamType, type TriggerType } from '../api'
  import RoleSelect from '../access/RoleSelect.svelte'
  import { getEditor } from './editor.svelte'
  import { describeCron, triggerInfo, triggerTypes } from './steps'

  // TriggerSettings edits what starts the flow.
  const ed = getEditor()
  const flow = $derived(ed.flow!)
  const t = $derived(flow.trigger)
  const table = $derived(ed.table(t.table))
  const info = $derived(triggerInfo(t.type))
  const isRow = $derived(t.type.startsWith('row_'))
  const unattended = $derived(t.type === 'schedule' || t.type === 'webhook')
  const hookURL = $derived(`${location.origin}/api/hooks/${flow.hookToken}`)

  function setType(type: TriggerType) {
    const next = { ...$state.snapshot(t), type }
    if (type.startsWith('row_') && !next.table) next.table = ed.tables[0]?.id
    if (type === 'schedule' && !next.cron) { next.cron = '0 8 * * 1-5'; next.timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC' }
    flow.trigger = next
    if ((type === 'schedule' || type === 'webhook') && flow.runAs === 'user' && !flow.runRole) flow.runRole = 'BASIC'
    ed.changed()
  }
  function setRunAs(v: 'flow' | 'user') {
    flow.runAs = v
    if (v === 'user' && !flow.runRole) flow.runRole = 'BASIC'
    ed.changed()
  }
  function set<K extends keyof typeof t>(k: K, v: (typeof t)[K]) {
    flow.trigger[k] = v
    ed.changed()
  }

  // Schedules: common shapes edit with fields, anything else as cron.
  type Mode = 'minutes' | 'hourly' | 'daily' | 'weekdays' | 'weekly' | 'monthly' | 'custom'
  const modes: { value: Mode; label: string }[] = [
    { value: 'minutes', label: 'Every few minutes' }, { value: 'hourly', label: 'Every hour' }, { value: 'daily', label: 'Every day' },
    { value: 'weekdays', label: 'Every weekday' }, { value: 'weekly', label: 'On days of the week' }, { value: 'monthly', label: 'Every month' },
    { value: 'custom', label: 'Custom (cron)' },
  ]
  const dayNames = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat']
  function parse(cron = '') {
    const p = cron.trim().split(/\s+/)
    const out = { mode: 'custom' as Mode, every: 15, minute: 0, time: '08:00', days: [1] as number[], dom: 1 }
    if (p.length !== 5) return out
    const [m, h, dom, mon, dow] = p
    const n = (s: string) => /^\d+$/.test(s)
    if (mon !== '*') return out
    if (/^\*\/\d+$/.test(m) && h === '*' && dom === '*' && dow === '*') return { ...out, mode: 'minutes' as Mode, every: +m.slice(2) }
    if (n(m) && h === '*' && dom === '*' && dow === '*') return { ...out, mode: 'hourly' as Mode, minute: +m }
    if (!n(m) || !n(h)) return out
    const time = `${h.padStart(2, '0')}:${m.padStart(2, '0')}`
    if (dom === '*' && dow === '*') return { ...out, mode: 'daily' as Mode, time }
    if (dom === '*' && (dow === '1-5' || dow === 'mon-fri')) return { ...out, mode: 'weekdays' as Mode, time }
    if (dom === '*' && /^[0-6](,[0-6])*$/.test(dow)) return { ...out, mode: 'weekly' as Mode, time, days: dow.split(',').map(Number) }
    if (n(dom) && dow === '*') return { ...out, mode: 'monthly' as Mode, time, dom: +dom }
    return out
  }
  let sched = $state(parse(''))
  let customMode = $state(false)
  $effect(() => { const p = parse(t.cron); sched = customMode ? { ...p, mode: 'custom' } : p })
  function build(s: typeof sched): string {
    const [h, m] = (s.time || '08:00').split(':').map((x) => String(+x))
    switch (s.mode) {
      case 'minutes': return `*/${Math.max(1, Math.min(59, s.every || 15))} * * * *`
      case 'hourly': return `${Math.max(0, Math.min(59, s.minute || 0))} * * * *`
      case 'daily': return `${m} ${h} * * *`
      case 'weekdays': return `${m} ${h} * * 1-5`
      case 'weekly': return `${m} ${h} * * ${(s.days.length ? [...s.days].sort() : [1]).join(',')}`
      case 'monthly': return `${m} ${h} ${Math.max(1, Math.min(31, s.dom || 1))} * *`
    }
    return t.cron ?? ''
  }
  function setSched(p: Partial<typeof sched>) {
    const next = { ...sched, ...p }
    customMode = next.mode === 'custom'
    sched = next
    if (next.mode !== 'custom') set('cron', build(next))
  }

  let next = $state<string[]>([])
  let nextError = $state('')
  let timer: ReturnType<typeof setTimeout> | undefined
  $effect(() => {
    if (t.type !== 'schedule' || !t.cron) return
    const cron = t.cron, tz = t.timezone || 'UTC'
    clearTimeout(timer)
    timer = setTimeout(async () => {
      try { next = (await api.nextRuns(cron, tz)).next; nextError = '' } catch (e: any) { next = []; nextError = e.message }
    }, 300)
  })
  const fmt = (iso: string) => {
    const [d, time] = iso.split('T')
    const day = new Date(d + 'T12:00:00Z').toLocaleDateString(undefined, { weekday: 'short', day: 'numeric', month: 'short', timeZone: 'UTC' })
    return `${day}, ${time.slice(0, 5)}`
  }
  const zones = (() => { try { return (Intl as any).supportedValuesOf('timeZone') as string[] } catch { return ['UTC', 'Europe/Paris', 'Europe/London', 'America/New_York'] } })()
  let zoneOpen = $state(false)

  // Button parameters.
  const params = $derived((t.parameters ?? []) as FlowParam[])
  const setParam = (i: number, p: Partial<FlowParam>) => set('parameters', params.map((x, j) => (j === i ? { ...x, ...p } : x)))
  const paramTypes: ParamType[] = ['text', 'number', 'boolean', 'json']

  async function rotate() {
    if (!ed.flow) return
    try {
      const f = await api.newHook(ed.flow.appId, ed.flow.id)
      ed.flow.hookToken = f.hookToken
      toast.success('The webhook has a new address; the old one stopped working.')
    } catch (e: any) { toast.error(e.message) }
  }
  const copy = (s: string) => { navigator.clipboard?.writeText(s); toast.success('Copied') }
</script>

<div class="flex flex-col gap-4 p-4">
  <div class="flex items-center gap-3">
    <span class="bg-primary/10 text-primary grid size-9 place-items-center rounded-lg"><info.icon class="size-4.5" /></span>
    <div><h2 class="text-sm font-semibold">Trigger</h2><p class="text-muted-foreground text-xs">What starts this flow</p></div>
  </div>
  <div class="grid grid-cols-2 gap-1.5">
    {#each triggerTypes as tt (tt.type)}
      <button type="button" onclick={() => setType(tt.type)}
        class={['flex items-center gap-2 rounded-lg border px-2.5 py-2 text-left text-xs font-medium transition-colors', t.type === tt.type ? 'border-primary bg-primary/5 text-primary' : 'hover:bg-muted']}>
        <tt.icon class="size-3.5 shrink-0" />{tt.label}
      </button>
    {/each}
  </div>
  <p class="text-muted-foreground -mt-2 text-xs">{info.description}.</p>
  <Separator />

  {#if isRow}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Table</Label>
      <Select.Root type="single" value={t.table ?? ''} onValueChange={(v) => { flow.trigger.table = v; flow.trigger.fields = []; ed.changed() }}>
        <Select.Trigger size="sm" class="w-full">{table?.name ?? 'Choose a table'}</Select.Trigger>
        <Select.Content>{#each ed.tables as tb (tb.id)}<Select.Item value={tb.id} label={tb.name}>{tb.name}</Select.Item>{/each}</Select.Content>
      </Select.Root>
    </div>
    {#if t.type === 'row_updated' && table}
      <div class="flex flex-col gap-1.5">
        <Label class="text-xs">Only when one of these changes</Label>
        <div class="flex flex-col gap-1 rounded-md border p-2">
          {#each table.fields.filter((f) => f.type !== 'formula' || f.formulaMode === 'static') as f (f.id)}
            {@const on = (t.fields ?? []).includes(f.name)}
            <label class="flex items-center gap-2 text-xs"><Checkbox checked={on} onCheckedChange={(v) => set('fields', v ? [...(t.fields ?? []), f.name] : (t.fields ?? []).filter((x) => x !== f.name))} />{f.name}</label>
          {/each}
        </div>
        <p class="text-muted-foreground text-xs">{(t.fields ?? []).length ? 'Changes to other fields are ignored.' : 'Nothing ticked: any change starts the flow.'}</p>
      </div>
    {/if}
    <p class="text-muted-foreground text-xs">Steps read the row as <code class="font-mono">{'{{ trigger.row.Name }}'}</code>{t.type === 'row_updated' ? ', and how it was as {{ trigger.old.Name }}' : ''}. Rows changed by CSV imports do not start flows.</p>
  {:else if t.type === 'schedule'}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Repeat</Label>
      <Select.Root type="single" value={sched.mode} onValueChange={(v) => setSched({ mode: v as Mode })}>
        <Select.Trigger size="sm" class="w-full">{modes.find((m) => m.value === sched.mode)?.label}</Select.Trigger>
        <Select.Content>{#each modes as m (m.value)}<Select.Item value={m.value} label={m.label}>{m.label}</Select.Item>{/each}</Select.Content>
      </Select.Root>
    </div>
    {#if sched.mode === 'minutes'}
      <div class="flex items-center gap-2 text-xs">Every <Input type="number" min="1" max="59" class="h-8 w-20" value={sched.every} oninput={(e) => setSched({ every: +e.currentTarget.value })} /> minutes</div>
    {:else if sched.mode === 'hourly'}
      <div class="flex items-center gap-2 text-xs">At minute <Input type="number" min="0" max="59" class="h-8 w-20" value={sched.minute} oninput={(e) => setSched({ minute: +e.currentTarget.value })} /> of every hour</div>
    {:else if sched.mode !== 'custom'}
      {#if sched.mode === 'weekly'}
        <div class="flex gap-1">
          {#each dayNames as dn, di (dn)}
            {@const on = sched.days.includes(di)}
            <button type="button" class={['h-8 flex-1 rounded-md border text-xs font-medium', on ? 'border-primary bg-primary text-primary-foreground' : 'hover:bg-muted']}
              onclick={() => setSched({ days: on ? sched.days.filter((x) => x !== di) : [...sched.days, di] })}>{dn}</button>
          {/each}
        </div>
      {/if}
      <div class="flex items-center gap-2 text-xs">
        {#if sched.mode === 'monthly'}On day <Input type="number" min="1" max="31" class="h-8 w-16" value={sched.dom} oninput={(e) => setSched({ dom: +e.currentTarget.value })} />{/if}
        at <Input type="time" class="h-8 w-28" value={sched.time} oninput={(e) => setSched({ time: e.currentTarget.value })} />
      </div>
    {:else}
      <div class="flex flex-col gap-1.5">
        <Input class="h-8 font-mono text-xs" value={t.cron ?? ''} placeholder="30 8 * * 1-5" oninput={(e) => set('cron', e.currentTarget.value)} />
        <p class="text-muted-foreground text-xs">Minute, hour, day of month, month, day of week. <code class="font-mono">*/15</code> repeats, <code class="font-mono">1-5</code> is Monday to Friday.</p>
      </div>
    {/if}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Time zone</Label>
      <Popover.Root bind:open={zoneOpen}>
        <Popover.Trigger>
          {#snippet child({ props })}<Button {...props} variant="outline" size="sm" class="w-full justify-between font-normal">{t.timezone || 'UTC'}<ChevronDownIcon class="opacity-50" /></Button>{/snippet}
        </Popover.Trigger>
        <Popover.Content class="w-72 p-0" align="start">
          <Command.Root>
            <Command.Input placeholder="Find a time zone…" />
            <Command.List class="max-h-64">
              <Command.Empty>No such zone.</Command.Empty>
              {#each zones as z (z)}<Command.Item value={z} onSelect={() => { set('timezone', z); zoneOpen = false }}>{z}</Command.Item>{/each}
            </Command.List>
          </Command.Root>
        </Popover.Content>
      </Popover.Root>
    </div>
    <div class="bg-muted/50 rounded-md border p-2.5 text-xs">
      <div class="font-medium">{describeCron(t.cron)}</div>
      {#if nextError}<div class="text-destructive mt-1">{nextError}</div>
      {:else if next.length}<div class="text-muted-foreground mt-1">Next: {next.slice(0, 3).map(fmt).join(' · ')}</div>{/if}
    </div>
  {:else if t.type === 'webhook'}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Address</Label>
      <div class="flex gap-1.5">
        <Input readonly class="h-8 font-mono text-[11px]" value={hookURL} onfocus={(e) => e.currentTarget.select()} />
        <Button variant="outline" size="icon" class="size-8 shrink-0" aria-label="Copy the address" onclick={() => copy(hookURL)}><CopyIcon /></Button>
      </div>
      <p class="text-muted-foreground text-xs">Anyone with this address can start the flow: POST JSON or a form to it. Keep it secret.</p>
      <Button variant="ghost" size="sm" class="h-7 self-start text-xs" onclick={rotate}><RefreshCwIcon />New address</Button>
    </div>
    <div class="flex items-start gap-2">
      <Switch id="respond" checked={!!t.respond} onCheckedChange={(v) => set('respond', v)} />
      <Label for="respond" class="flex flex-col items-start gap-0.5 text-xs font-normal">Answer with the flow’s result<span class="text-muted-foreground">Waits up to 30 seconds for a Return result step; otherwise answers at once.</span></Label>
    </div>
    <pre class="bg-muted/60 overflow-x-auto rounded-md border p-2.5 font-mono text-[11px] leading-5">curl -X POST {hookURL} \
  -H 'Content-Type: application/json' \
  -d '&#123;"name": "Ada"&#125;'</pre>
    <p class="text-muted-foreground text-xs">Steps read the JSON as <code class="font-mono">{'{{ trigger.body.name }}'}</code> and the query string as <code class="font-mono">{'{{ trigger.query.x }}'}</code>.</p>
  {:else}
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Who may run it</Label>
      <RoleSelect roles={ed.roles} value={t.role} onChange={(v) => set('role', v)} />
    </div>
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Parameters</Label>
      {#each params as p, i (i)}
        <div class="flex items-center gap-1.5">
          <Input class="h-8 min-w-0 flex-1 font-mono text-xs" value={p.name} placeholder="name" oninput={(e) => setParam(i, { name: e.currentTarget.value })} aria-label="Parameter name" />
          <Select.Root type="single" value={p.type} onValueChange={(v) => setParam(i, { type: v as ParamType })}>
            <Select.Trigger size="sm" class="w-24">{p.type}</Select.Trigger>
            <Select.Content>{#each paramTypes as pt (pt)}<Select.Item value={pt} label={pt}>{pt}</Select.Item>{/each}</Select.Content>
          </Select.Root>
          <Input class="h-8 w-20 text-xs" value={p.default} placeholder="default" oninput={(e) => setParam(i, { default: e.currentTarget.value })} aria-label="Default" />
          <Button variant="ghost" size="icon" class="size-7 shrink-0" aria-label="Remove parameter" onclick={() => set('parameters', params.filter((_, j) => j !== i))}><XIcon class="size-3.5" /></Button>
        </div>
      {/each}
      <Button variant="outline" size="sm" class="h-7 self-start border-dashed text-xs" onclick={() => set('parameters', [...params, { name: '', type: 'text', default: '' }])}><PlusIcon />Add parameter</Button>
      <p class="text-muted-foreground text-xs">Buttons pass them with the <strong>Run flow</strong> action; steps read <code class="font-mono">{'{{ trigger.params.name }}'}</code>. What a Return result step gives back reaches later actions as <code class="font-mono">{'{{ result }}'}</code>.</p>
    </div>
  {/if}
  <Separator />
  <div class="flex flex-col gap-1.5">
    <Label class="text-xs">Steps run with</Label>
    <Select.Root type="single" value={flow.runAs ?? 'flow'} onValueChange={(v) => setRunAs(v as 'flow' | 'user')}>
      <Select.Trigger size="sm" class="w-full">{(flow.runAs ?? 'flow') === 'flow' ? 'Full access to the app' : unattended ? 'The permissions of a role' : 'The permissions of who started it'}</Select.Trigger>
      <Select.Content>
        <Select.Item value="flow" label="Full access"><div><div>Full access to the app</div><div class="text-muted-foreground text-xs">Row steps and queries may use every table and row</div></div></Select.Item>
        <Select.Item value="user" label="As the user"><div><div>{unattended ? 'The permissions of a role' : 'The permissions of who started it'}</div><div class="text-muted-foreground text-xs">Table permissions, row rules and query roles apply{unattended ? '; nobody starts a ' + (t.type === 'schedule' ? 'schedule' : 'webhook') + ', so runs act as the role you choose' : ''}</div></div></Select.Item>
      </Select.Content>
    </Select.Root>
    {#if flow.runAs === 'user' && unattended}
      <Label class="mt-1 text-xs" for="run-role">Runs act as</Label>
      <RoleSelect id="run-role" mode="assign" roles={ed.roles} value={flow.runRole ?? ''} onChange={(v) => { flow.runRole = v; ed.changed() }} />
      <p class="text-muted-foreground text-xs">Each run has exactly this role's access, the way a member with it would. Test runs act as it too.</p>
    {:else if flow.runAs === 'user' && isRow}
      <p class="text-muted-foreground text-xs">Runs act as whoever changed the row.</p>
    {/if}
  </div>
</div>
