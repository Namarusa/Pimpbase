<script lang="ts">
  import { toast } from 'svelte-sonner'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as Empty from '$lib/components/ui/empty'
  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Spinner } from '$lib/components/ui/spinner'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import EllipsisIcon from '@lucide/svelte/icons/ellipsis'
  import MailIcon from '@lucide/svelte/icons/mail'
  import SaveIcon from '@lucide/svelte/icons/save'
  import SendIcon from '@lucide/svelte/icons/send'
  import WorkflowIcon from '@lucide/svelte/icons/workflow'
  import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw'
  import { api, type App, type EmailTemplate, type EmailVersion, type RenderedEmail } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import AppNav from '../components/AppNav.svelte'
  import ConfirmHost from '../lib/screen/ConfirmHost.svelte'
  import EmailEditor from '../lib/emails/EmailEditor.svelte'
  import EmailPreview from '../lib/emails/EmailPreview.svelte'
  import { starters } from '../lib/emails/blocks'
  import { timeAgo } from '../lib/format'

  // Emails holds an app's email templates: a list, the chosen template's
  // editor, and a live preview filled with the fields' samples. Saving
  // makes a new version; flows send the newest. Templates open at
  // /apps/<app>/emails/<id>.
  let { app: slug, item }: { app: string; item?: string } = $props()
  let app = $state<App | null>(null)
  let templates = $state<EmailTemplate[]>([])
  let error = $state('')
  let ready = $state(false)
  let confirmHost = $state<ConfirmHost>()

  async function load() {
    try {
      const res = await api.app(slug)
      if (!res.builder) { error = 'Only builders can manage email templates.'; return }
      app = res.app
      templates = await api.emailTemplates(app.id)
      ready = true
    } catch (e: any) { error = e.message }
  }
  load()

  const go = (id?: string) => navigate(`/apps/${slug}/emails${id ? '/' + id : ''}`)
  const current = $derived(templates.find((t) => t.id === item) ?? null)
  $effect(() => { if (ready && !item && templates[0]) go(templates[0].id) })

  // The draft is what the editor changes; it is saved on request.
  let draft = $state<EmailTemplate | null>(null)
  let dirty = $state(false)
  let selected = $state('')
  let saving = $state(false)
  let loadedId = ''
  $effect(() => {
    if (!current || current.id === loadedId) return
    loadedId = current.id
    draft = structuredClone($state.snapshot(current)) as EmailTemplate
    dirty = false
    selected = ''
    viewing = null
    versions = []
    loadVersions()
  })
  const changed = () => { dirty = true; schedulePreview() }

  // Live preview, shortly after each change.
  let rendered = $state<RenderedEmail | null>(null)
  let previewError = $state('')
  let rendering = $state(false)
  let timer: ReturnType<typeof setTimeout> | undefined
  function schedulePreview() {
    clearTimeout(timer)
    timer = setTimeout(renderPreview, 250)
  }
  async function renderPreview() {
    if (!app) return
    const t = viewing ?? draft
    if (!t) return
    rendering = true
    try {
      rendered = await api.previewEmail(app.id, $state.snapshot(t) as EmailTemplate, { mark: !viewing })
      previewError = ''
    } catch (e: any) { previewError = e.message } finally { rendering = false }
  }
  $effect(() => { if (draft?.id) renderPreview() })

  async function save() {
    if (!app || !draft || saving) return
    saving = true
    try {
      const t = await api.updateEmailTemplate(app.id, draft.id, $state.snapshot(draft) as EmailTemplate)
      templates = templates.map((x) => (x.id === t.id ? t : x)).sort((a, b) => a.name.localeCompare(b.name))
      draft.version = t.version
      draft.updatedAt = t.updatedAt
      dirty = false
      toast.success(`Saved as version ${t.version}`)
      loadVersions()
    } catch (e: any) { toast.error(e.message) } finally { saving = false }
  }
  async function discard() {
    if (!current) return
    draft = structuredClone($state.snapshot(current)) as EmailTemplate
    dirty = false
    renderPreview()
  }
  async function sendTest() {
    if (!app || !draft) return
    try {
      const r = await api.previewEmail(app.id, $state.snapshot(draft) as EmailTemplate, { send: true })
      toast.success(`Test sent to ${r.sent?.to}`, { description: r.sent?.receipt.note })
    } catch (e: any) { toast.error(e.message) }
  }
  async function open(id: string) {
    if (id === item) return
    if (dirty && !(await confirmHost?.ask('Leave without saving?', 'Your changes to this template are not saved.'))) return
    dirty = false
    go(id)
  }

  // Versions.
  let versions = $state<EmailVersion[]>([])
  let viewing = $state<EmailTemplate | null>(null)
  async function loadVersions() {
    if (!app || !draft) return
    try { versions = await api.emailVersions(app.id, draft.id) } catch { versions = [] }
  }
  async function view(v: number) {
    if (!app || !draft) return
    if (v === draft.version && !dirty) { viewing = null; renderPreview(); return }
    viewing = await api.emailVersion(app.id, draft.id, v)
    renderPreview()
  }
  async function restore(v: number) {
    if (!app || !draft) return
    if (dirty && !(await confirmHost?.ask(`Restore version ${v}?`, 'Your unsaved changes are dropped.'))) return
    try {
      const t = await api.restoreEmailVersion(app.id, draft.id, v)
      templates = templates.map((x) => (x.id === t.id ? t : x))
      draft = structuredClone(t)
      dirty = false
      viewing = null
      toast.success(`Version ${v} is back, as version ${t.version}`)
      loadVersions()
      renderPreview()
    } catch (e: any) { toast.error(e.message) }
  }

  // New templates.
  let adding = $state(false)
  let newName = $state('')
  let starter = $state('order')
  let addError = $state('')
  function startAdd() {
    let n = 'New email'
    for (let i = 2; templates.some((t) => t.name.toLowerCase() === n.toLowerCase()); i++) n = `New email ${i}`
    newName = n
    addError = ''
    adding = true
  }
  async function create(e: SubmitEvent) {
    e.preventDefault()
    if (!app) return
    try {
      const t = await api.createEmailTemplate(app.id, starters.find((s) => s.key === starter)!.make(newName))
      templates = [...templates, t].sort((a, b) => a.name.localeCompare(b.name))
      adding = false
      dirty = false
      go(t.id)
    } catch (err: any) { addError = err.message }
  }
  async function duplicate(t: EmailTemplate) {
    if (!app) return
    let n = `${t.name} copy`
    for (let i = 2; templates.some((x) => x.name.toLowerCase() === n.toLowerCase()); i++) n = `${t.name} copy ${i}`
    try {
      const c = await api.createEmailTemplate(app.id, { ...$state.snapshot(t), name: n })
      templates = [...templates, c].sort((a, b) => a.name.localeCompare(b.name))
      go(c.id)
    } catch (e: any) { toast.error(e.message) }
  }
  async function remove(t: EmailTemplate) {
    if (!app || !(await confirmHost?.ask(`Delete the template “${t.name}”?`, 'Its saved versions go too.'))) return
    try {
      await api.deleteEmailTemplate(app.id, t.id)
      templates = templates.filter((x) => x.id !== t.id)
      if (item === t.id) { dirty = false; go() }
    } catch (e: any) { toast.error(e.message) }
  }

  function keydown(e: KeyboardEvent) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') { e.preventDefault(); save() }
  }
  function beforeunload(e: BeforeUnloadEvent) { if (dirty) e.preventDefault() }
</script>

<svelte:window onkeydown={keydown} onbeforeunload={beforeunload} />

<div class="flex h-full flex-col">
  {#if app}<AppNav {app} active="emails" />{/if}
  {#if error}
    <p class="text-destructive p-6 text-sm">{error}</p>
  {:else if ready && app}
    <div class="flex min-h-0 flex-1">
      <aside class="bg-card flex w-64 shrink-0 flex-col border-r">
        <div class="flex items-center justify-between px-3 pt-3 pb-1">
          <span class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">Email templates</span>
          <Button variant="ghost" size="icon" class="size-7" onclick={startAdd} aria-label="New template"><PlusIcon /></Button>
        </div>
        <div class="flex flex-1 flex-col gap-0.5 overflow-y-auto px-2 pb-3">
          {#each templates as t (t.id)}
            <div class={['group flex items-center rounded-md', item === t.id ? 'bg-primary/10 text-primary' : 'hover:bg-muted']}>
              <button type="button" class="flex min-w-0 flex-1 items-center gap-2.5 px-2 py-1.5 text-left" onclick={() => open(t.id)}>
                <MailIcon class="size-4 shrink-0 opacity-70" />
                <span class="flex min-w-0 flex-col">
                  <span class="truncate text-sm font-medium">{t.name}</span>
                  <span class="text-muted-foreground truncate text-[11px]">{(t.usedBy ?? []).length ? `Sent by ${(t.usedBy ?? []).length} flow step${(t.usedBy ?? []).length === 1 ? '' : 's'}` : 'Not used by a flow yet'}</span>
                </span>
              </button>
              <DropdownMenu.Root>
                <DropdownMenu.Trigger>
                  {#snippet child({ props })}<Button {...props} variant="ghost" size="icon" class="mr-1 size-6 opacity-0 group-hover:opacity-100 data-[state=open]:opacity-100" aria-label="Template actions"><EllipsisIcon /></Button>{/snippet}
                </DropdownMenu.Trigger>
                <DropdownMenu.Content align="start">
                  <DropdownMenu.Item onclick={() => duplicate(t)}>Duplicate</DropdownMenu.Item>
                  <DropdownMenu.Separator />
                  <DropdownMenu.Item variant="destructive" onclick={() => remove(t)}>Delete</DropdownMenu.Item>
                </DropdownMenu.Content>
              </DropdownMenu.Root>
            </div>
          {/each}
        </div>
      </aside>

      {#if draft && current}
        <section class="flex min-w-0 flex-1 flex-col">
          <div class="bg-card flex h-12 shrink-0 items-center gap-3 border-b px-4">
            <span class="truncate text-sm font-semibold">{draft.name}</span>
            <Badge variant="outline" class="h-5 px-1.5 text-[10px]">v{draft.version}</Badge>
            {#if dirty}<span class="text-xs text-amber-600 dark:text-amber-400">Unsaved changes</span>{/if}
            <div class="ml-auto flex items-center gap-2">
              <Button variant="ghost" size="sm" onclick={sendTest}><SendIcon />Send me a test</Button>
              {#if dirty}<Button variant="outline" size="sm" onclick={discard}>Discard</Button>{/if}
              <Button size="sm" onclick={save} disabled={!dirty || saving}>{#if saving}<Spinner />{:else}<SaveIcon />{/if}Save</Button>
            </div>
          </div>
          <div class="flex min-h-0 flex-1">
            <div class="bb-email-canvas min-w-0 flex-1 overflow-y-auto">
              <div class="mx-auto flex max-w-3xl flex-col gap-3 p-6">
                {#if viewing}
                  <div class="bg-card flex items-center gap-3 rounded-lg border px-3 py-2 text-sm">
                    <span>Viewing version {viewing.version}, saved {timeAgo(viewing.updatedAt ?? '')} by {viewing.updatedBy || 'someone'}.</span>
                    <Button size="sm" variant="outline" class="ml-auto h-7" onclick={() => restore(viewing!.version)}><RotateCcwIcon />Restore</Button>
                    <Button size="sm" variant="ghost" class="h-7" onclick={() => { viewing = null; renderPreview() }}>Back to the draft</Button>
                  </div>
                {/if}
                <EmailPreview html={rendered?.html} text={rendered?.text} subject={rendered?.subject} preheader={rendered?.preheader}
                  from={app.name} to="the sample recipient" warnings={rendered?.warnings ?? []} error={previewError} loading={rendering}
                  bind:selected selectable={!viewing && draft.mode === 'blocks'} badge={viewing ? `version ${viewing.version}` : 'sample data'} />
                {#if (current.usedBy ?? []).length}
                  <div class="bg-card rounded-lg border px-3 py-2">
                    <div class="text-muted-foreground mb-1 text-[11px] font-semibold tracking-wide uppercase">Sent by</div>
                    <div class="flex flex-wrap gap-1.5">
                      {#each current.usedBy ?? [] as u (u.flowId + u.step)}
                        <Button variant="outline" size="sm" class="h-7 text-xs" href={`/apps/${slug}/flows/${u.flowId}`} onclick={(e) => { e.preventDefault(); navigate(`/apps/${slug}/flows/${u.flowId}`) }}><WorkflowIcon />{u.flowName} · {u.step}</Button>
                      {/each}
                    </div>
                  </div>
                {/if}
              </div>
            </div>
            <aside class="bg-card w-[400px] shrink-0 border-l">
              <EmailEditor {draft} onChange={changed} bind:selected app={app ? { name: app.name, slug: app.slug } : null}>
                {#snippet history()}
                  <div class="flex flex-col gap-2">
                    <p class="text-muted-foreground text-xs">Each save keeps a version (the last 50). Flows send the newest. Pick one to preview it.</p>
                    <ol class="flex flex-col gap-1">
                      {#each versions as v (v.version)}
                        <li>
                          <button type="button" onclick={() => view(v.version)}
                            class={['flex w-full items-center gap-2 rounded-md border px-2.5 py-2 text-left', (viewing?.version ?? (dirty ? -1 : draft?.version)) === v.version ? 'border-primary bg-primary/5' : 'hover:bg-muted']}>
                            <Badge variant={v.version === draft?.version ? 'default' : 'secondary'} class="h-5 px-1.5 font-mono text-[10px]">v{v.version}</Badge>
                            <span class="flex min-w-0 flex-1 flex-col">
                              <span class="truncate text-xs font-medium">{v.subject || '(no subject)'}</span>
                              <span class="text-muted-foreground text-[11px]">{timeAgo(v.savedAt)} · {v.savedBy || 'someone'}</span>
                            </span>
                            {#if v.version === draft?.version}<span class="text-muted-foreground text-[10px]">current</span>{/if}
                          </button>
                        </li>
                      {/each}
                    </ol>
                  </div>
                {/snippet}
              </EmailEditor>
            </aside>
          </div>
        </section>
      {:else}
        <Empty.Root class="flex-1">
          <Empty.Header>
            <Empty.Media variant="icon"><MailIcon /></Empty.Media>
            <Empty.Title>Emails your flows send</Empty.Title>
            <Empty.Description>Design an email once, with fields such as a customer or order lines, then send it from any flow with a Send email step.</Empty.Description>
          </Empty.Header>
          <Empty.Content><Button onclick={startAdd}><PlusIcon />New template</Button></Empty.Content>
        </Empty.Root>
      {/if}
    </div>
  {:else}
    <p class="text-muted-foreground p-6 text-sm">Loading…</p>
  {/if}
</div>

<Dialog.Root bind:open={adding}>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header><Dialog.Title>New email template</Dialog.Title><Dialog.Description>Start from an example and make it yours.</Dialog.Description></Dialog.Header>
    <form id="new-tpl" class="flex flex-col gap-4" onsubmit={create}>
      <div class="flex flex-col gap-1.5"><Label for="tpl-new-name">Name</Label><Input id="tpl-new-name" bind:value={newName} required /></div>
      <div class="grid grid-cols-2 gap-2">
        {#each starters as s (s.key)}
          <button type="button" onclick={() => (starter = s.key)}
            class={['flex flex-col items-start gap-0.5 rounded-lg border p-2.5 text-left transition-colors', starter === s.key ? 'border-primary bg-primary/5' : 'hover:bg-muted']}>
            <span class="text-sm font-medium">{s.label}</span><span class="text-muted-foreground text-xs">{s.description}</span>
          </button>
        {/each}
      </div>
      {#if addError}<p class="text-destructive text-sm">{addError}</p>{/if}
    </form>
    <Dialog.Footer><Button variant="outline" onclick={() => (adding = false)}>Cancel</Button><Button type="submit" form="new-tpl">Create</Button></Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>
<ConfirmHost bind:this={confirmHost} />

<style>
  .bb-email-canvas { background-color: var(--muted); }
  :global(.dark) .bb-email-canvas { background-color: color-mix(in oklab, var(--background) 70%, var(--muted)); }
</style>
