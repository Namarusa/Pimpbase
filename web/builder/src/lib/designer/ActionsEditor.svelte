<script lang="ts">
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Switch } from '$lib/components/ui/switch'
  import { Label } from '$lib/components/ui/label'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import ArrowUpIcon from '@lucide/svelte/icons/arrow-up'
  import BindingInput from './BindingInput.svelte'
  import FieldSelect from './FieldSelect.svelte'
  import NavigateEditor from './NavigateEditor.svelte'
  import { actionInfo, actionTypes, describe } from '../screen/actions'
  import { registry } from '../screen/registry'
  import { getDesigner } from './state.svelte'
  import { all } from './tree'
  import type { BindingGroup } from './context'
  import type { Action } from '../screen/types'

  // ActionsEditor edits the list of actions a button, row click or form
  // runs, in order.
  let { value, onChange, groups }: { value: Action[]; onChange: (v: Action[]) => void; groups: BindingGroup[] } = $props()
  const d = getDesigner()
  const list = $derived(Array.isArray(value) ? value : [])
  const nodes = $derived(d.root ? all(d.root) : [])
  const dialogs = $derived(nodes.filter((n) => n.type === 'dialog' || n.type === 'side_panel'))
  const dataNodes = $derived(nodes.filter((n) => ['data_provider', 'row_provider', 'table', 'list', 'cards', 'chart', 'stat', 'repeater', 'form'].includes(n.type)))
  const none = '\u0000none'

  const set = (i: number, key: string, v: any) => onChange(list.map((a, j) => (j === i ? { ...a, [key]: v } : a)))
  const add = (type: string) => {
    const a: Action = { type }
    if (type === 'toast') { a.kind = 'success'; a.message = 'Done' }
    if ((type === 'save_row' || type === 'delete_row' || type === 'export_csv') && d.tables[0]) a.table = d.tables[0].id
    if (type === 'save_row') a.values = []
    onChange([...list, a])
  }
  const label = (id: string) => {
    const n = nodes.find((x) => x.id === id)
    return n ? `${n.name || registry[n.type]?.label}${n.props.title ? ` · ${n.props.title}` : ''}` : ''
  }
</script>

<div class="flex flex-col gap-2">
  {#each list as a, i (i)}
    {@const info = actionInfo(a.type)}
    <div class="bg-muted/40 rounded-md border">
      <div class="flex items-center gap-1 border-b px-2 py-1.5">
        <span class="text-muted-foreground w-4 text-xs tabular-nums">{i + 1}</span>
        <span class="flex-1 truncate text-xs font-medium">{describe(a, d)}</span>
        {#if i > 0}<Button variant="ghost" size="icon" class="size-6" aria-label="Move up" onclick={() => { const n = [...list]; [n[i - 1], n[i]] = [n[i], n[i - 1]]; onChange(n) }}><ArrowUpIcon class="size-3" /></Button>{/if}
        <Button variant="ghost" size="icon" class="size-6" aria-label="Remove action" onclick={() => onChange(list.filter((_, j) => j !== i))}><XIcon class="size-3" /></Button>
      </div>
      <div class="flex flex-col gap-2 p-2">
        {#each info?.fields ?? [] as f (f.key)}
          {#if f.kind === 'boolean'}
            <div class="flex items-center gap-2"><Switch id={`a${i}${f.key}`} checked={!!a[f.key]} onCheckedChange={(v) => set(i, f.key, v)} /><Label for={`a${i}${f.key}`} class="text-xs font-normal">{f.label}</Label></div>
          {:else}
            <div class="flex flex-col gap-1">
              <Label class="text-muted-foreground text-xs font-normal">{f.label}</Label>
              {#if f.kind === 'screen'}
                <NavigateEditor action={a} screens={d.screens} {groups} onChange={(na) => onChange(list.map((x, j) => (j === i ? na : x)))} />
              {:else if f.kind === 'text'}
                <BindingInput value={a[f.key] ?? ''} onChange={(v) => set(i, f.key, v)} {groups} placeholder={f.placeholder} />
              {:else if f.kind === 'select'}
                <Select.Root type="single" value={a[f.key] ?? ''} onValueChange={(v) => set(i, f.key, v)}>
                  <Select.Trigger size="sm" class="w-full">{f.options?.find((o) => o.value === a[f.key])?.label ?? 'Choose'}</Select.Trigger>
                  <Select.Content>{#each f.options ?? [] as o (o.value)}<Select.Item value={o.value} label={o.label}>{o.label}</Select.Item>{/each}</Select.Content>
                </Select.Root>
              {:else if f.kind === 'table'}
                <Select.Root type="single" value={a.table ?? ''} onValueChange={(v) => set(i, 'table', v)}>
                  <Select.Trigger size="sm" class="w-full">{d.table(a.table)?.name ?? 'Choose a table'}</Select.Trigger>
                  <Select.Content>{#each d.tables as t (t.id)}<Select.Item value={t.id} label={t.name}>{t.name}</Select.Item>{/each}</Select.Content>
                </Select.Root>
              {:else if f.kind === 'dialog' || f.kind === 'component'}
                {@const opts = f.kind === 'dialog' ? dialogs : dataNodes}
                <Select.Root type="single" value={a[f.key] ?? ''} onValueChange={(v) => set(i, f.key, v === none ? '' : v)}>
                  <Select.Trigger size="sm" class="w-full">{a[f.key] ? label(a[f.key]) : f.kind === 'dialog' ? (a.type === 'close_dialog' ? 'All dialogs' : 'Choose a dialog') : 'Everything'}</Select.Trigger>
                  <Select.Content>
                    {#if f.kind === 'component' || a.type === 'close_dialog'}<Select.Item value={none} label="All">{f.kind === 'dialog' ? 'All dialogs' : 'Everything'}</Select.Item>{/if}
                    {#each opts as n (n.id)}<Select.Item value={n.id} label={label(n.id)}>{label(n.id)}</Select.Item>{/each}
                    {#if f.kind === 'dialog' && dialogs.length === 0}<div class="text-muted-foreground px-2 py-1.5 text-xs">Add a Dialog or Side panel to this screen first.</div>{/if}
                  </Select.Content>
                </Select.Root>
              {:else if f.kind === 'query'}
                <Select.Root type="single" value={a.query ?? ''} onValueChange={(v) => onChange(list.map((x, j) => (j === i ? { ...x, query: v, params: {} } : x)))}>
                  <Select.Trigger size="sm" class="w-full">{d.queries.find((q) => q.id === a.query)?.name ?? 'Choose a query'}</Select.Trigger>
                  <Select.Content>
                    {#each d.queries as q (q.id)}<Select.Item value={q.id} label={q.name}>{q.name}<span class="text-muted-foreground ml-auto font-mono text-[10px]">{q.method}</span></Select.Item>{/each}
                    {#if !d.queries.length}<div class="text-muted-foreground px-2 py-1.5 text-xs">Add a query in the APIs tab first.</div>{/if}
                  </Select.Content>
                </Select.Root>
              {:else if f.kind === 'flow'}
                {@const manual = d.flows.filter((x) => x.trigger.type === 'manual')}
                <Select.Root type="single" value={a.flow ?? ''} onValueChange={(v) => onChange(list.map((x, j) => (j === i ? { ...x, flow: v, params: {} } : x)))}>
                  <Select.Trigger size="sm" class="w-full">{d.flows.find((x) => x.id === a.flow)?.name ?? 'Choose a flow'}</Select.Trigger>
                  <Select.Content>
                    {#each manual as fl (fl.id)}<Select.Item value={fl.id} label={fl.name}>{fl.name}{#if !fl.enabled}<span class="text-muted-foreground ml-auto text-[10px]">off</span>{/if}</Select.Item>{/each}
                    {#if !manual.length}<div class="text-muted-foreground px-2 py-1.5 text-xs">Add a flow started by a button in the Flows tab first.</div>{/if}
                  </Select.Content>
                </Select.Root>
              {:else if f.kind === 'flowparams'}
                {@const fl = d.flows.find((x) => x.id === a.flow)}
                {#each fl?.trigger.parameters ?? [] as fp (fp.name)}
                  <div class="flex items-center gap-1">
                    <span class="w-2/5 shrink-0 truncate font-mono text-xs">{fp.name}</span>
                    <div class="min-w-0 flex-1"><BindingInput value={a.params?.[fp.name] ?? ''} {groups} placeholder={fp.default || 'default'} onChange={(v) => set(i, 'params', { ...(a.params ?? {}), [fp.name]: v })} /></div>
                  </div>
                {:else}
                  <span class="text-muted-foreground text-xs">{fl ? 'This flow takes no parameters.' : 'Choose a flow first.'}</span>
                {/each}
              {:else if f.kind === 'params'}
                {@const q = d.queries.find((x) => x.id === a.query)}
                {#each q?.parameters ?? [] as qp (qp.name)}
                  <div class="flex items-center gap-1">
                    <span class="w-2/5 shrink-0 truncate font-mono text-xs">{qp.name}</span>
                    <div class="min-w-0 flex-1"><BindingInput value={a.params?.[qp.name] ?? ''} {groups} placeholder={qp.default || 'default'} onChange={(v) => set(i, 'params', { ...(a.params ?? {}), [qp.name]: v })} /></div>
                  </div>
                {:else}
                  <span class="text-muted-foreground text-xs">{q ? 'This query takes no parameters.' : 'Choose a query first.'}</span>
                {/each}
              {:else if f.kind === 'values'}
                {@const t = d.table(a.table)}
                {@const vals = (a.values ?? []) as { field: string; value: string }[]}
                {#each vals as v, k (k)}
                  <div class="flex items-center gap-1">
                    <div class="w-2/5 shrink-0"><FieldSelect value={v.field} fields={t?.fields.filter((x) => x.type !== 'formula' && x.type !== 'autonumber') ?? []} allowNone={false}
                      onChange={(fv) => set(i, 'values', vals.map((x, m) => (m === k ? { ...x, field: fv } : x)))} /></div>
                    <div class="min-w-0 flex-1"><BindingInput value={v.value} {groups} placeholder="value" onChange={(nv) => set(i, 'values', vals.map((x, m) => (m === k ? { ...x, value: nv } : x)))} /></div>
                    <Button variant="ghost" size="icon" class="size-6 shrink-0" aria-label="Remove value" onclick={() => set(i, 'values', vals.filter((_, m) => m !== k))}><XIcon class="size-3" /></Button>
                  </div>
                {/each}
                <Button variant="outline" size="sm" class="h-7 self-start text-xs" onclick={() => set(i, 'values', [...vals, { field: '', value: '' }])}><PlusIcon />Add value</Button>
              {/if}
            </div>
          {/if}
        {/each}
      </div>
    </div>
  {/each}
  <DropdownMenu.Root>
    <DropdownMenu.Trigger>
      {#snippet child({ props })}<Button {...props} variant="outline" size="sm" class="h-8 w-full border-dashed"><PlusIcon />Add action</Button>{/snippet}
    </DropdownMenu.Trigger>
    <DropdownMenu.Content class="w-56">
      {#each actionTypes as t (t.type)}<DropdownMenu.Item onclick={() => add(t.type)}>{t.label}</DropdownMenu.Item>{/each}
    </DropdownMenu.Content>
  </DropdownMenu.Root>
</div>
