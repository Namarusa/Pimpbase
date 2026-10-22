<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import * as ToggleGroup from '$lib/components/ui/toggle-group'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import { Button } from '$lib/components/ui/button'
  import { Separator } from '$lib/components/ui/separator'
  import CopyIcon from '@lucide/svelte/icons/copy'
  import TrashIcon from '@lucide/svelte/icons/trash'
  import WandIcon from '@lucide/svelte/icons/wand-sparkles'
  import PlugIcon from '@lucide/svelte/icons/plug'
  import BindingInput from './BindingInput.svelte'
  import FieldSelect from './FieldSelect.svelte'
  import FieldsPicker from './FieldsPicker.svelte'
  import OptionsInput from './OptionsInput.svelte'
  import ChartColors from './ChartColors.svelte'
  import { colorItems } from '../screen/chart'
  import ActionsEditor from './ActionsEditor.svelte'
  import ScreenFormulaInput from './ScreenFormulaInput.svelte'
  import ConditionsEditor from './ConditionsEditor.svelte'
  import { conditionFormula } from '../screen/formulas'
  import { registry } from '../screen/registry'
  import { conditionOps } from '../screen/bindings'
  import { iconNames, icons } from '../screen/icons'
  import { getDesigner } from './state.svelte'
  import { bindingsFor, contextTable, withQueries } from './context'
  import { duplicate, remove, findIn } from './tree'
  import { formFields } from './templates'
  import type { Role } from '../api'
  import RoleSelect from '../access/RoleSelect.svelte'
  import RolesPicker from '../access/RolesPicker.svelte'
  import type { Node, Setting } from '../screen/types'
  import { incoming } from '../screen/navigation'

  const d = getDesigner()
  const node = $derived(d.node)
  const def = $derived(node ? registry[node.type] : undefined)
  const props = $derived(node ? { ...def?.defaults(), ...node.props } : {})
  const sources = $derived(withQueries(d.tables, d.queries))
  const table = $derived(node && d.root ? contextTable(d.root, node.id, sources) : undefined)
  // received is what other screens send here; this screen's own tree is
  // the one being edited.
  const liveScreens = $derived(d.screens.map((s) => (s.id === d.screen?.id && d.root ? { ...s, definition: d.root } : s)))
  const received = $derived(d.screen ? incoming(d.screen, liveScreens) : [])
  const groups = $derived(node && d.root && d.screen ? bindingsFor(d.root, node.id, sources, d.screen.route, false, received) : [])
  const actionGroups = $derived(node && d.root && d.screen ? bindingsFor(d.root, node.id, sources, d.screen.route, true, received) : [])
  const nodeQuery = $derived(String(props.table ?? '').startsWith('query:') ? d.queries.find((q) => q.id === String(props.table).slice(6)) : undefined)
  const order = ['Data', '', 'Validation', 'Layout', 'Style', 'Conditions']
  const sections = $derived.by(() => {
    const visible = (def?.settings ?? []).filter((s) => !s.show || s.show(props))
    return order.map((name) => ({ name, settings: visible.filter((s) => (s.section ?? '') === name) })).filter((g) => g.settings.length)
  })

  let screenName = $state('')
  let screenRoute = $state('')
  let metaError = $state('')
  $effect(() => {
    screenName = d.screen?.name ?? ''
    screenRoute = d.screen?.route ?? ''
    metaError = ''
  })

  function set(key: string, v: any) {
    const id = node?.id
    if (!id) return
    d.change((root) => {
      const n = findIn(root, id)
      if (!n) return
      n.props[key] = v
      // A form's fields follow a new table.
      if (n.type === 'form' && key === 'table' && n.children.length === 0) {
        const t = d.table(v)
        if (t) n.children = formFields(t)
      }
    }, true)
  }
  function rename(v: string) {
    const id = node?.id
    if (id) d.change((root) => { const n = findIn(root, id); if (n) n.name = v || undefined }, true)
  }
  function addFormFields() {
    const id = node?.id
    const t = d.table(props.table)
    if (!id || !t) return
    d.change((root) => {
      const n = findIn(root, id)
      if (!n) return
      const have = new Set(n.children.map((c) => c.props.field))
      n.children.push(...formFields(t).filter((f) => !have.has(f.props.field)))
    })
  }
  function del() {
    const id = node?.id
    if (!id || id === 'root') return
    d.change((root) => { remove(root, id) })
    d.selected = 'root'
  }
  function dup() {
    const id = node?.id
    if (!id || id === 'root') return
    let copy: Node | null = null
    d.change((root) => { copy = duplicate(root, id) })
    if (copy) d.selected = (copy as Node).id
  }
  async function saveMeta(p: { name?: string; route?: string; role?: Role }) {
    metaError = await d.updateMeta(p)
    if (metaError) { screenName = d.screen?.name ?? ''; screenRoute = d.screen?.route ?? '' }
  }
  const sid = (s: Setting) => `s-${node?.id}-${s.key}`
</script>

<div class="flex h-full flex-col">
  {#if !node || !def}
    <p class="text-muted-foreground p-4 text-sm">Select a component on the canvas or in the tree to change its settings.</p>
  {:else}
    <div class="flex items-center gap-2 border-b px-4 py-3">
      <span class="bg-primary/10 text-primary grid size-8 place-items-center rounded-md"><def.icon class="size-4" /></span>
      <div class="min-w-0 flex-1">
        <div class="truncate text-sm font-semibold">{node.id === 'root' ? 'Screen' : def.label}</div>
        <div class="text-muted-foreground truncate text-xs">{node.id === 'root' ? 'The page and its layout' : def.description}</div>
      </div>
      {#if node.id !== 'root'}
        <Button variant="ghost" size="icon" class="size-8" onclick={dup} title="Duplicate (Ctrl+D)"><CopyIcon /></Button>
        <Button variant="ghost" size="icon" class="hover:text-destructive size-8" onclick={del} title="Delete (Del)"><TrashIcon /></Button>
      {/if}
    </div>

    <div class="flex-1 overflow-y-auto">
      {#if node.id === 'root' && d.screen}
        <div class="flex flex-col gap-3 p-4">
          <div class="flex flex-col gap-1.5"><Label for="scr-name" class="text-xs">Screen name</Label>
            <Input id="scr-name" class="h-8" bind:value={screenName} onblur={() => screenName !== d.screen?.name && saveMeta({ name: screenName })} /></div>
          <div class="flex flex-col gap-1.5"><Label for="scr-route" class="text-xs">Route</Label>
            <Input id="scr-route" class="h-8 font-mono text-xs" bind:value={screenRoute} onblur={() => screenRoute !== d.screen?.route && saveMeta({ route: screenRoute })} />
            <p class="text-muted-foreground text-xs">Use <code>:id</code> for a part that changes, such as <code>/orders/:id</code>.</p></div>
          <div class="flex flex-col gap-1.5"><Label class="text-xs">Values this screen receives</Label>
            {#each received as r (r.path)}
              <div class="flex items-baseline gap-2 text-xs"><code class="font-mono">{`{{ ${r.path} }}`}</code><span class="text-muted-foreground ml-auto truncate">{r.from.length ? `from ${r.from.join(', ')}` : 'from the address'}</span></div>
            {:else}
              <p class="text-muted-foreground text-xs">None yet. Another screen sends values with a Go to screen action; add <code>:id</code> to the route or an extra value there.</p>
            {/each}
          </div>
          <div class="flex flex-col gap-1.5"><Label class="text-xs">Who can open it</Label>
            <RoleSelect roles={d.roles} value={d.screen.role} onChange={(v) => saveMeta({ role: v })} /></div>
          {#if metaError}<p class="text-destructive text-xs">{metaError}</p>{/if}
        </div>
        <Separator />
      {:else}
        <div class="flex flex-col gap-1.5 px-4 pt-4">
          <Label for="node-name" class="text-xs">Name in the tree</Label>
          <Input id="node-name" class="h-8" placeholder={def.label} value={node.name ?? ''} oninput={(e) => rename(e.currentTarget.value)} />
        </div>
      {/if}

      {#each sections as sec (sec.name)}
        <div class="flex flex-col gap-3 px-4 py-4">
          {#if sec.name}<div class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">{sec.name}</div>{/if}
          {#if node.type === 'form' && sec.name === 'Data' && props.table}
            <Button variant="outline" size="sm" class="h-8" onclick={addFormFields}><WandIcon />Add a field for every column</Button>
          {/if}
          {#each sec.settings as s (s.key)}
            {#if s.kind === 'boolean'}
              <div class="flex items-center justify-between gap-3">
                <Label for={sid(s)} class="text-xs font-normal">{s.label}</Label>
                <Switch id={sid(s)} checked={!!props[s.key]} onCheckedChange={(v) => set(s.key, v)} />
              </div>
            {:else}
              <div class="flex flex-col gap-1.5">
                <Label for={sid(s)} class="text-xs">{s.label}</Label>
                {#if s.kind === 'text'}
                  <BindingInput id={sid(s)} value={props[s.key] ?? ''} onChange={(v) => set(s.key, v)} {groups} placeholder={s.placeholder} />
                {:else if s.kind === 'textarea'}
                  <BindingInput id={sid(s)} multiline value={props[s.key] ?? ''} onChange={(v) => set(s.key, v)} {groups} placeholder={s.placeholder} />
                {:else if s.kind === 'filter'}
                  <BindingInput id={sid(s)} multiline mono value={props[s.key] ?? ''} onChange={(v) => set(s.key, v)} {groups} placeholder={'[Status] = "Open"'} />
                {:else if s.kind === 'number'}
                  <Input id={sid(s)} type="number" class="h-8" value={props[s.key] ?? ''} oninput={(e) => set(s.key, e.currentTarget.value === '' ? '' : Number(e.currentTarget.value))} />
                {:else if s.kind === 'segmented'}
                  <ToggleGroup.Root type="single" variant="outline" size="sm" class="w-full" value={String(props[s.key] ?? '')} onValueChange={(v) => v && set(s.key, v)}>
                    {#each s.options ?? [] as o (o.value)}<ToggleGroup.Item value={o.value} class="flex-1 px-1 text-xs">{o.label}</ToggleGroup.Item>{/each}
                  </ToggleGroup.Root>
                {:else if s.kind === 'select'}
                  <Select.Root type="single" value={props[s.key] ?? ''} onValueChange={(v) => set(s.key, v)}>
                    <Select.Trigger id={sid(s)} size="sm" class="w-full">{s.options?.find((o) => o.value === props[s.key])?.label ?? 'Choose'}</Select.Trigger>
                    <Select.Content>{#each s.options ?? [] as o (o.value)}<Select.Item value={o.value} label={o.label}>{o.label}</Select.Item>{/each}</Select.Content>
                  </Select.Root>
                {:else if s.kind === 'table'}
                  <Select.Root type="single" value={props[s.key] ?? ''} onValueChange={(v) => set(s.key, v === '\u0000' ? '' : v)}>
                    <Select.Trigger id={sid(s)} size="sm" class="w-full">{d.table(props[s.key])?.name ?? (def.tableSource ? 'From the data provider' : 'Choose a table')}</Select.Trigger>
                    <Select.Content>
                      {#if def.tableSource}<Select.Item value={'\u0000'} label="None"><span class="text-muted-foreground">From the data provider</span></Select.Item>{/if}
                      <Select.Group><Select.GroupHeading>Tables</Select.GroupHeading>
                        {#each d.tables as t (t.id)}<Select.Item value={t.id} label={t.name}>{t.name}</Select.Item>{/each}
                      </Select.Group>
                      {#if s.queries && d.queries.length}
                        <Select.Group><Select.GroupHeading>API queries</Select.GroupHeading>
                          {#each d.queries as q (q.id)}<Select.Item value={`query:${q.id}`} label={q.name}><PlugIcon class="opacity-60" />{q.name}<span class="text-muted-foreground ml-auto font-mono text-[10px]">{q.method}</span></Select.Item>{/each}
                        </Select.Group>
                      {/if}
                    </Select.Content>
                  </Select.Root>
                  {#if nodeQuery}
                    {#if !nodeQuery.fields.length}<p class="text-muted-foreground text-xs">Send this query once in the API studio so its columns can be picked here.</p>{/if}
                    {#each nodeQuery.parameters as qp (qp.name)}
                      <div class="flex flex-col gap-1 pl-2">
                        <Label class="text-muted-foreground text-xs font-normal"><span class="font-mono">{qp.name}</span> · {qp.type}</Label>
                        <BindingInput value={props.params?.[qp.name] ?? ''} {groups} placeholder={qp.default || 'default'} onChange={(v) => set('params', { ...(props.params ?? {}), [qp.name]: v })} />
                      </div>
                    {/each}
                  {/if}
                {:else if s.kind === 'field'}
                  <FieldSelect id={sid(s)} value={props[s.key] ?? ''} onChange={(v) => set(s.key, v)} fields={table?.fields ?? []} types={s.fieldTypes} />
                {:else if s.kind === 'fields'}
                  <FieldsPicker value={props[s.key] ?? []} onChange={(v) => set(s.key, v)} fields={table?.fields ?? []} types={s.fieldTypes} />
                {:else if s.kind === 'options'}
                  {#key node.id}<OptionsInput id={sid(s)} value={props[s.key] ?? []} onChange={(v) => set(s.key, v)} />{/key}
                {:else if s.kind === 'icon'}
                  {@const Icon = icons[props[s.key]]}
                  <Select.Root type="single" value={props[s.key] ?? ''} onValueChange={(v) => set(s.key, v === '\u0000' ? '' : v)}>
                    <Select.Trigger id={sid(s)} size="sm" class="w-full">{#if Icon}<Icon />{props[s.key]}{:else}<span class="text-muted-foreground">No icon</span>{/if}</Select.Trigger>
                    <Select.Content class="max-h-72">
                      <Select.Item value={'\u0000'} label="None"><span class="text-muted-foreground">No icon</span></Select.Item>
                      {#each iconNames as name (name)}{@const I = icons[name]}<Select.Item value={name} label={name}><I />{name}</Select.Item>{/each}
                    </Select.Content>
                  </Select.Root>
                {:else if s.kind === 'color'}
                  <Input id={sid(s)} type="color" class="h-8 w-16 p-1" value={props[s.key] || '#4f46e5'} oninput={(e) => set(s.key, e.currentTarget.value)} />
                {:else if s.kind === 'chartColors'}
                  <ChartColors value={props[s.key] ?? {}} onChange={(v) => set(s.key, v)} items={colorItems(props, table?.fields ?? [])} />
                {:else if s.kind === 'actions'}
                  <ActionsEditor value={props[s.key] ?? []} onChange={(v) => set(s.key, v)} groups={actionGroups} />
                {:else if s.kind === 'roles'}
                  <RolesPicker id={sid(s)} roles={d.roles} value={props[s.key] ?? []} onChange={(v) => set(s.key, v.length ? v : undefined)} />
                {:else if s.kind === 'formula'}
                  <ScreenFormulaInput id={sid(s)} value={props[s.key] ?? ''} onChange={(v) => set(s.key, v.trim() ? v : undefined)} {groups} placeholder={s.placeholder} />
                  {#if s.key === 'showWhen' && props.visibleIf?.left}
                    {@const c = props.visibleIf}
                    {@const f = conditionFormula(c)}
                    <div class="bg-muted/50 flex flex-col gap-1 rounded-md border p-2 text-xs">
                      <span>Also shown only when <code class="font-mono">{c.left}</code> {conditionOps.find((o) => o.value === c.op)?.label} {#if c.right}<code class="font-mono">{c.right}</code>{/if}</span>
                      <div class="flex gap-3">
                        {#if f}<Button variant="link" size="sm" class="h-auto p-0 text-xs" onclick={() => { set('showWhen', props.showWhen ? `(${props.showWhen}) AND ${f}` : f); set('visibleIf', undefined) }}>Turn into a formula</Button>{/if}
                        <Button variant="link" size="sm" class="h-auto p-0 text-xs" onclick={() => set('visibleIf', undefined)}>Remove</Button>
                      </div>
                    </div>
                  {/if}
                {:else if s.kind === 'conditions'}
                  <ConditionsEditor value={props[s.key] ?? []} onChange={(v) => set(s.key, v)} settings={def.settings} {props} {groups} />
                {/if}
                {#if s.help}<p class="text-muted-foreground text-xs">{s.help}</p>{/if}
              </div>
            {/if}
          {/each}
        </div>
        <Separator />
      {/each}
    </div>
  {/if}
</div>
