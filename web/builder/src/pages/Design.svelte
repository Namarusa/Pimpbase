<script lang="ts">
  import { onDestroy } from 'svelte'
  import { toast } from 'svelte-sonner'
  import * as Popover from '$lib/components/ui/popover'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as ToggleGroup from '$lib/components/ui/toggle-group'
  import * as Tooltip from '$lib/components/ui/tooltip'
  import * as Empty from '$lib/components/ui/empty'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Separator } from '$lib/components/ui/separator'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import Undo2Icon from '@lucide/svelte/icons/undo-2'
  import Redo2Icon from '@lucide/svelte/icons/redo-2'
  import MonitorIcon from '@lucide/svelte/icons/monitor'
  import TabletIcon from '@lucide/svelte/icons/tablet'
  import SmartphoneIcon from '@lucide/svelte/icons/smartphone'
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link'
  import EllipsisIcon from '@lucide/svelte/icons/ellipsis'
  import FileIcon from '@lucide/svelte/icons/file'
  import LayoutDashboardIcon from '@lucide/svelte/icons/layout-dashboard'
  import CheckIcon from '@lucide/svelte/icons/check'
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert'
  import { api } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import { session } from '../lib/session.svelte'
  import AppNav from '../components/AppNav.svelte'
  import { Designer, setDesigner } from '../lib/designer/state.svelte'
  import TreeItem from '../lib/designer/TreeItem.svelte'
  import Palette from '../lib/designer/Palette.svelte'
  import SettingsPanel from '../lib/designer/SettingsPanel.svelte'
  import NewScreenDialog from '../lib/designer/NewScreenDialog.svelte'
  import ThemeMenu from '../lib/designer/ThemeMenu.svelte'
  import { duplicate, findIn, insertAt, parentOf, place, remove, shift } from '../lib/designer/tree'
  import { formFields } from '../lib/designer/templates'
  import { newNode, registry } from '../lib/screen/registry'
  import { Runtime } from '../lib/screen/context.svelte'
  import { themeVars, styleString } from '../lib/screen/theme'
  import ScreenHost from '../lib/screen/ScreenHost.svelte'
  import ConfirmHost from '../lib/screen/ConfirmHost.svelte'
  import AppFrame from '../lib/screen/AppFrame.svelte'
  import type { Node, Screen } from '../lib/screen/types'

  let { app: slug, screenId }: { app: string; screenId?: string } = $props()
  const d = new Designer()
  setDesigner(d)
  let ready = $state(false)
  let error = $state('')
  let paletteOpen = $state(false)
  let newScreen = $state(false)
  let confirmHost = $state<ConfirmHost>()
  const drag = $state<{ id: string | null }>({ id: null })
  let params = $state<Record<string, string>>({})

  const rt = new Runtime({
    appId: '', appSlug: slug, tables: [], editing: true, env: 'development',
    navigate: () => {},
    toast: (kind, m) => (kind === 'error' ? toast.error(m) : toast.success(m)),
    confirm: (t, m) => confirmHost?.ask(t, m) ?? Promise.resolve(true),
    select: (id) => (d.selected = id),
  })
  $effect(() => { rt.selected = d.selected })
  $effect(() => { rt.hovered = d.hovered })

  async function load() {
    try {
      const res = await api.app(slug)
      if (!res.builder) { error = 'Only builders can design screens.'; return }
      d.app = res.app
      const [tables, screens, queries] = await Promise.all([api.tables(res.app.id), api.screens(res.app.id), api.queries(res.app.id)])
      d.tables = tables
      d.screens = screens
      d.queries = queries
      rt.queries = queries
      rt.appId = res.app.id
      rt.appSlug = res.app.slug
      rt.tables = tables
      ready = true
    } catch (e: any) { error = e.message }
  }
  load()

  // Open the screen in the address, or the first one.
  $effect(() => {
    if (!ready) return
    const want = d.screens.find((s) => s.id === screenId) ?? d.screens[0] ?? null
    if (want && want.id !== screenId) { navigate(`/apps/${slug}/design/${want.id}`); return }
    if (want?.id !== d.screen?.id) {
      d.open(want)
      if (want) previewParams(want)
    }
  })
  onDestroy(() => d.flush())

  // Bring the selected component into view on the canvas.
  $effect(() => {
    const id = d.selected
    if (!id || id === 'root') return
    requestAnimationFrame(() => document.querySelector(`.bb-canvas [data-node="${CSS.escape(id)}"]`)?.scrollIntoView({ block: 'nearest', behavior: 'smooth' }))
  })

  // Routes with parameters preview with a real row, so the canvas shows
  // data.
  async function previewParams(s: Screen) {
    params = {}
    const names = s.route.split('/').filter((x) => x.startsWith(':')).map((x) => x.slice(1))
    if (!names.length) return
    const holder = [s.definition, ...flat(s.definition)].find((n) => (n.type === 'row_provider' || (n.type === 'form' && n.props.mode === 'update')) && n.props.table)
    let first = ''
    if (holder) {
      try { first = String((await api.rows(d.app.id, holder.props.table, { limit: 1 })).rows[0]?._id ?? '') } catch { /* no rows to preview */ }
    }
    params = Object.fromEntries(names.map((n) => [n, n === 'id' ? first : '']))
  }
  const flat = (n: Node): Node[] => n.children.flatMap((c) => [c, ...flat(c)])

  function open(s: Screen) {
    navigate(`/apps/${slug}/design/${s.id}`)
  }

  // add places a new component next to or inside the selection, with
  // defaults that show real data straight away.
  function add(type: string) {
    paletteOpen = false
    if (!d.root) return
    const node = newNode(type)
    const def = registry[type]
    const t = d.tables[0]
    const where = place(d.root, d.selected, type)
    if (!where) { toast.error(`A ${def.label} cannot go there.`); return }
    const inProvider = [where.parent, ...ancestors(where.parent.id)].some((n) => ['data_provider', 'row_provider', 'repeater'].includes(n.type))
    if (t && def.settings.some((s) => s.key === 'table') && !(def.tableSource && inProvider)) node.props.table = t.id
    const tbl = d.table(node.props.table)
    if (tbl) {
      const primary = tbl.fields.find((f) => f.id === tbl.primaryField)?.name ?? tbl.fields[0]?.name ?? ''
      if (type === 'form') node.children = formFields(tbl)
      if (type === 'chart') node.props.labelField = (tbl.fields.find((f) => f.type === 'single_select') ?? tbl.fields.find((f) => f.type === 'text'))?.name ?? ''
      if (type === 'list' || type === 'cards') { node.props.titleField = primary; node.props.subtitleField = tbl.fields.find((f) => f.name !== primary && f.type === 'text')?.name ?? '' }
    }
    d.change((root) => {
      const target = findIn(root, where.parent.id)
      if (target) insertAt(target, node, where.index)
    })
    d.selected = node.id
  }
  const ancestors = (id: string): Node[] => {
    const out: Node[] = []
    let p = d.root ? parentOf(d.root, id) : null
    while (p) { out.push(p); p = d.root ? parentOf(d.root, p.id) : null }
    return out
  }

  async function deleteScreen(s: Screen) {
    if (!(await confirmHost?.ask(`Delete the screen “${s.name}”?`, 'Its components are deleted too. The data stays.'))) return
    try {
      await api.deleteScreen(d.app.id, s.id)
      d.screens = d.screens.filter((x) => x.id !== s.id)
      if (d.screen?.id === s.id) { d.open(null); navigate(`/apps/${slug}/design`) }
    } catch (e: any) { toast.error(e.message) }
  }

  async function duplicateScreen(s: Screen) {
    d.flush()
    try {
      let route = `${s.route}-copy`
      for (let i = 2; d.screens.some((x) => x.route === route); i++) route = `${s.route}-copy-${i}`
      const src = d.screens.find((x) => x.id === s.id) ?? s
      const copy = await api.createScreen(d.app.id, { name: `${s.name} copy`, route: route.replace(/\/:(\w+)-copy/, '-copy/:$1'), role: s.role, definition: src.definition })
      d.screens = [...d.screens, copy]
      open(copy)
    } catch (e: any) { toast.error(e.message) }
  }

  function keydown(e: KeyboardEvent) {
    const t = e.target as HTMLElement
    if (t.closest('input, textarea, select, [contenteditable], [role=dialog], [role=listbox], [role=menu]')) return
    const mod = e.ctrlKey || e.metaKey
    if (mod && e.key.toLowerCase() === 'z') { e.preventDefault(); if (e.shiftKey) d.redo(); else d.undo() }
    else if (mod && e.key.toLowerCase() === 'y') { e.preventDefault(); d.redo() }
    else if (mod && e.key.toLowerCase() === 'd' && d.selected && d.selected !== 'root') {
      e.preventDefault()
      const id = d.selected
      let copy: Node | null = null
      d.change((root) => { copy = duplicate(root, id) })
      if (copy) d.selected = (copy as Node).id
    } else if ((e.key === 'Delete' || e.key === 'Backspace') && d.selected && d.selected !== 'root') {
      e.preventDefault()
      const id = d.selected
      const parent = d.root ? parentOf(d.root, id) : null
      d.change((root) => { remove(root, id) })
      d.selected = parent?.id ?? 'root'
    } else if (e.key === 'Escape' && d.selected && d.root) {
      d.selected = parentOf(d.root, d.selected)?.id ?? 'root'
    } else if (e.altKey && (e.key === 'ArrowUp' || e.key === 'ArrowDown') && d.selected) {
      e.preventDefault()
      const id = d.selected
      d.change((root) => shift(root, id, e.key === 'ArrowUp' ? -1 : 1))
    }
  }

  const theme = $derived(d.app?.design?.theme === 'dark' ? 'dark' : d.app?.design?.theme === 'light' ? 'theme-light' : '')
  const width = $derived(d.device === 'phone' ? 'w-[390px]' : d.device === 'tablet' ? 'w-[820px]' : 'w-full')
  const paramNames = $derived(d.screen ? d.screen.route.split('/').filter((x) => x.startsWith(':')).map((x) => x.slice(1)) : [])
  const appUrl = $derived(d.screen ? `/app/${slug}${d.screen.route.replace(/:(\w+)/g, (_, n) => params[n] || '1')}` : `/app/${slug}`)
</script>

<svelte:window onkeydown={keydown} />

<div class="flex h-full flex-col">
  {#if d.app}<AppNav app={d.app} active="design" />{/if}
  {#if error}
    <p class="text-destructive p-6 text-sm">{error}</p>
  {:else if ready}
    <div class="flex min-h-0 flex-1">
      <!-- Screens and the component tree -->
      <aside class="bg-card flex w-68 shrink-0 flex-col border-r">
        <div class="flex items-center justify-between px-3 pt-3 pb-1">
          <span class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">Screens</span>
          <Button variant="ghost" size="icon" class="size-7" onclick={() => (newScreen = true)} aria-label="New screen"><PlusIcon /></Button>
        </div>
        <div class="flex max-h-[40%] flex-col gap-0.5 overflow-y-auto px-2 pb-2">
          {#each d.screens as s (s.id)}
            <div class={['group flex items-center rounded-md', d.screen?.id === s.id ? 'bg-primary/10 text-primary' : 'hover:bg-muted']}>
              <button type="button" class="flex min-w-0 flex-1 items-center gap-2 px-2 py-1.5 text-left" onclick={() => open(s)}>
                {#if s.route.includes('dashboard')}<LayoutDashboardIcon class="size-3.5 shrink-0 opacity-70" />{:else}<FileIcon class="size-3.5 shrink-0 opacity-70" />{/if}
                <span class="min-w-0 flex-1">
                  <span class="block truncate text-sm font-medium">{s.name}</span>
                  <span class="text-muted-foreground block truncate font-mono text-[11px]">{s.route}</span>
                </span>
              </button>
              <DropdownMenu.Root>
                <DropdownMenu.Trigger>
                  {#snippet child({ props })}<Button {...props} variant="ghost" size="icon" class="mr-1 size-6 opacity-0 group-hover:opacity-100 data-[state=open]:opacity-100" aria-label="Screen actions"><EllipsisIcon /></Button>{/snippet}
                </DropdownMenu.Trigger>
                <DropdownMenu.Content align="start">
                  <DropdownMenu.Item onclick={() => window.open(`/app/${slug}${s.route.replace(/:(\w+)/g, '1')}`, '_blank')}>Open in the app</DropdownMenu.Item>
                  <DropdownMenu.Item onclick={() => duplicateScreen(s)}>Duplicate</DropdownMenu.Item>
                  <DropdownMenu.Separator />
                  <DropdownMenu.Item variant="destructive" onclick={() => deleteScreen(s)}>Delete</DropdownMenu.Item>
                </DropdownMenu.Content>
              </DropdownMenu.Root>
            </div>
          {/each}
        </div>
        <Separator />
        {#if d.root}
          <div class="flex items-center justify-between px-3 pt-3 pb-1">
            <span class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">Components</span>
          </div>
          <div role="tree" class="flex-1 overflow-y-auto px-2 pb-4">
            <TreeItem node={d.root} {drag} />
          </div>
        {/if}
      </aside>

      <!-- Canvas -->
      <section class="flex min-w-0 flex-1 flex-col">
        <div class="bg-card flex h-12 shrink-0 items-center gap-2 border-b px-3">
          <Popover.Root bind:open={paletteOpen}>
            <Popover.Trigger disabled={!d.root}>
              {#snippet child({ props })}<Button {...props} size="sm"><PlusIcon />Add component</Button>{/snippet}
            </Popover.Trigger>
            <Popover.Content class="w-96 p-0" align="start"><Palette onPick={add} /></Popover.Content>
          </Popover.Root>
          <Tooltip.Provider>
            <Tooltip.Root><Tooltip.Trigger>{#snippet child({ props })}<Button {...props} variant="ghost" size="icon" class="size-8" disabled={!d.canUndo} onclick={() => d.undo()} aria-label="Undo"><Undo2Icon /></Button>{/snippet}</Tooltip.Trigger><Tooltip.Content>Undo (Ctrl+Z)</Tooltip.Content></Tooltip.Root>
            <Tooltip.Root><Tooltip.Trigger>{#snippet child({ props })}<Button {...props} variant="ghost" size="icon" class="size-8" disabled={!d.canRedo} onclick={() => d.redo()} aria-label="Redo"><Redo2Icon /></Button>{/snippet}</Tooltip.Trigger><Tooltip.Content>Redo (Ctrl+Shift+Z)</Tooltip.Content></Tooltip.Root>
          </Tooltip.Provider>
          <Separator orientation="vertical" class="mx-1 h-5!" />
          <ToggleGroup.Root type="single" size="sm" value={d.device} onValueChange={(v) => v && (d.device = v as typeof d.device)}>
            <ToggleGroup.Item value="desktop" aria-label="Desktop"><MonitorIcon /></ToggleGroup.Item>
            <ToggleGroup.Item value="tablet" aria-label="Tablet"><TabletIcon /></ToggleGroup.Item>
            <ToggleGroup.Item value="phone" aria-label="Phone"><SmartphoneIcon /></ToggleGroup.Item>
          </ToggleGroup.Root>
          {#each paramNames as pn (pn)}
            <div class="text-muted-foreground flex items-center gap-1.5 text-xs">
              <span class="font-mono">:{pn}</span>
              <Input class="h-7 w-16 text-xs" value={params[pn] ?? ''} oninput={(e) => (params = { ...params, [pn]: e.currentTarget.value })} aria-label={`Preview ${pn}`} />
            </div>
          {/each}
          <div class="ml-auto flex items-center gap-2">
            <span class="text-muted-foreground flex items-center gap-1 text-xs">
              {#if d.saving === 'saving'}Saving…{:else if d.saving === 'error'}<CircleAlertIcon class="text-destructive size-3.5" /><span class="text-destructive" title={d.saveError}>Not saved</span>{:else}<CheckIcon class="size-3.5" />Saved{/if}
            </span>
            <ThemeMenu />
            <Button variant="outline" size="sm" href={appUrl} target="_blank"><ExternalLinkIcon />Preview</Button>
          </div>
        </div>
        <!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
        <div class="bg-muted/50 flex min-h-0 flex-1 justify-center overflow-auto p-4" onclick={() => (d.selected = 'root')} onmouseleave={() => (rt.hovered = null)}>
          {#if d.screen && d.root}
            <div class={['bb-canvas flex h-full shrink-0 flex-col overflow-hidden rounded-xl border shadow-sm transition-[width]', width, theme]} style={styleString(themeVars(d.app.design))}>
              <AppFrame design={d.app.design} appName={d.app.name} screens={d.screens} current={d.screen.id} compact={d.device !== 'desktop'}
                user={session.user ? { name: session.user.name || session.user.email, email: session.user.email } : null}
                onNavigate={(r) => { const s = d.screens.find((x) => x.route === r); if (s) open(s) }}>
                {#key d.screen.id}
                  <ScreenHost {rt} root={d.root} scope={() => ({
                    user: session.user ? { id: session.user.id, name: session.user.name, email: session.user.email } : null,
                    app: { id: d.app.id, name: d.app.name, slug: d.app.slug }, url: params, query: {}, state: rt.state,
                  })} />
                {/key}
              </AppFrame>
            </div>
          {:else}
            <Empty.Root class="self-center">
              <Empty.Header>
                <Empty.Media variant="icon"><LayoutDashboardIcon /></Empty.Media>
                <Empty.Title>Design your first screen</Empty.Title>
                <Empty.Description>Start from a table to get a list, a detail page and forms, or from a blank page.</Empty.Description>
              </Empty.Header>
              <Empty.Content><Button onclick={() => (newScreen = true)}><PlusIcon />New screen</Button></Empty.Content>
            </Empty.Root>
          {/if}
        </div>
      </section>

      <!-- Settings -->
      <aside class="bg-card w-80 shrink-0 border-l">
        {#if d.screen}<SettingsPanel />{/if}
      </aside>
    </div>
  {:else}
    <p class="text-muted-foreground p-6 text-sm">Loading…</p>
  {/if}
</div>

<NewScreenDialog bind:open={newScreen} onCreated={open} />
<ConfirmHost bind:this={confirmHost} />
