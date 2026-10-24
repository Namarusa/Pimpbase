<script lang="ts">
  import { untrack } from 'svelte'
  import { toast } from 'svelte-sonner'
  import * as Card from '$lib/components/ui/card'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Select from '$lib/components/ui/select'
  import { Badge } from '$lib/components/ui/badge'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import { Spinner } from '$lib/components/ui/spinner'
  import MailIcon from '@lucide/svelte/icons/mail'
  import SendIcon from '@lucide/svelte/icons/send'
  import ServerIcon from '@lucide/svelte/icons/server'
  import PencilIcon from '@lucide/svelte/icons/pencil'
  import RotateCcwIcon from '@lucide/svelte/icons/rotate-ccw'
  import { api, type EmailSettings, type EmailTemplate, type RenderedEmail, type SystemEmail } from '../api'
  import { session } from '../session.svelte'
  import EmailEditor from '../emails/EmailEditor.svelte'
  import EmailPreview from '../emails/EmailPreview.svelte'

  // EmailSettings is the workspace's mail: how it is sent (SMTP or a
  // provider's API), the sender, sign-in links, and the system emails
  // (invitations, password resets, sign-in links) people get.
  let { onChanged }: { onChanged?: (ok: boolean) => void } = $props()

  const methods: { id: EmailSettings['method']; name: string; help: string }[] = [
    { id: 'smtp', name: 'SMTP', help: 'Any mail server' },
    { id: 'sendgrid', name: 'SendGrid', help: 'API key' },
    { id: 'mailgun', name: 'Mailgun', help: 'API key and domain' },
    { id: 'postmark', name: 'Postmark', help: 'Server token' },
    { id: 'resend', name: 'Resend', help: 'API key' },
    { id: 'log', name: 'Log only', help: 'For trying things: nothing leaves' },
  ]
  const security = { starttls: 'STARTTLS (port 587)', tls: 'TLS (port 465)', none: 'None (local servers)' }

  let status = $state<EmailSettings | null>(null)
  let form = $state<EmailSettings | null>(null)
  let secret = $state('')
  let saving = $state(false)
  let testing = $state(false)
  let testTo = $state('')
  let error = $state('')

  async function load() {
    try {
      status = await api.emailSettings()
      form = structuredClone($state.snapshot(status))
      secret = ''
      onChanged?.(!!status.configured)
    } catch (e: any) { error = e.message }
  }
  load()

  async function save() {
    if (!form) return
    saving = true
    try {
      const body: Partial<EmailSettings> = { ...$state.snapshot(form), smtpPassword: '', apiKey: '' }
      if (form.method === 'smtp') body.smtpPassword = secret
      else body.apiKey = secret
      status = await api.saveEmailSettings(body)
      form = structuredClone($state.snapshot(status))
      secret = ''
      onChanged?.(!!status.configured)
      toast.success('Email settings saved')
    } catch (e: any) { toast.error(e.message) } finally { saving = false }
  }
  async function test() {
    testing = true
    try {
      const r = await api.testEmailSettings(testTo.trim() || undefined)
      toast.success(`Test email sent to ${r.to}`, { description: r.receipt.note })
    } catch (e: any) { toast.error(e.message) } finally { testing = false }
  }
  const dirty = $derived(!!form && !!status && (secret !== '' || JSON.stringify(form) !== JSON.stringify(status)))
  const usesKey = $derived(form && form.method && !['smtp', 'log'].includes(form.method))
  const keySet = $derived(status && form && status.method === form.method && status.apiKeySet)

  // System emails.
  let systems = $state<SystemEmail[]>([])
  api.systemEmails().then((s) => (systems = s)).catch(() => {})
  let editing = $state<SystemEmail | null>(null)
  let open = $state(false)
  let draft = $state<EmailTemplate | null>(null)
  let selected = $state('')
  let changed = $state(false)
  let rendered = $state<RenderedEmail | null>(null)
  let previewError = $state('')
  let rendering = $state(false)
  let busy = $state<'' | 'save' | 'reset' | 'send'>('')
  let timer: ReturnType<typeof setTimeout> | undefined

  function edit(s: SystemEmail) {
    editing = s
    draft = structuredClone($state.snapshot(s.template)) as EmailTemplate
    selected = ''
    changed = false
    rendered = null
    open = true
    preview()
  }
  function onChange() { changed = true; clearTimeout(timer); timer = setTimeout(preview, 250) }
  async function preview() {
    if (!editing || !draft) return
    rendering = true
    try {
      rendered = await api.previewSystemEmail(editing.key, $state.snapshot(draft), { mark: true })
      previewError = ''
    } catch (e: any) { previewError = e.message } finally { rendering = false }
  }
  function replace(s: SystemEmail) {
    systems = systems.map((x) => (x.key === s.key ? s : x))
    editing = s
  }
  async function saveSystem() {
    if (!editing || !draft) return
    busy = 'save'
    try {
      replace(await api.saveSystemEmail(editing.key, $state.snapshot(draft)))
      changed = false
      toast.success(`${editing.name} saved`)
    } catch (e: any) { toast.error(e.message) } finally { busy = '' }
  }
  async function resetSystem() {
    if (!editing) return
    busy = 'reset'
    try {
      const s = await api.resetSystemEmail(editing.key)
      replace(s)
      draft = structuredClone($state.snapshot(s.template)) as EmailTemplate
      changed = false
      preview()
      toast.success(`${s.name} is back to the default`)
    } catch (e: any) { toast.error(e.message) } finally { busy = '' }
  }
  async function sendSystem() {
    if (!editing || !draft) return
    busy = 'send'
    try {
      const r = await api.previewSystemEmail(editing.key, $state.snapshot(draft), { send: true })
      if (r.sent) toast.success(`Test sent to ${r.sent.to}`, { description: r.sent.receipt.note })
    } catch (e: any) { toast.error(e.message) } finally { busy = '' }
  }
  function close(v: boolean) {
    if (!v && changed && !confirm('Close without saving your changes?')) return
    open = v
  }
  const what: Record<string, string> = {
    invite: 'Sent when an admin invites someone. Its link lets them choose a password.',
    password_reset: 'Sent when someone asks to reset their password, or an admin sends them a reset link.',
    sign_in_link: 'Sent when someone asks for a sign-in link (when sign-in links are on).',
  }
  $effect(() => { if (!open) untrack(() => clearTimeout(timer)) })
</script>

{#if error}
  <p class="text-destructive text-sm">{error}</p>
{:else if form && status}
  <div class="flex flex-col gap-4">
    <Card.Root>
      <Card.Header>
        <Card.Title class="flex items-center gap-2"><MailIcon class="size-4 text-sky-500" />Email delivery
          {#if status.configured}<Badge variant="secondary" class="text-emerald-700 dark:text-emerald-400">On</Badge>{:else}<Badge variant="outline">Off: not set up</Badge>{/if}
        </Card.Title>
        <Card.Description>How the workspace sends mail: flows' Send email steps, invitations, password resets and sign-in links.</Card.Description>
      </Card.Header>
      <Card.Content class="flex flex-col gap-5">
        {#if status.server}
          <p class="text-muted-foreground flex items-center gap-1.5 text-xs"><ServerIcon class="size-3.5" />The server was started with mail delivery ({status.server}), which is used. These settings still give the sender and sign-in links.</p>
        {/if}
        <div class="grid grid-cols-2 gap-2 sm:grid-cols-3" role="radiogroup" aria-label="Delivery">
          {#each methods as m (m.id)}
            <button type="button" role="radio" aria-checked={form.method === m.id} data-method={m.id}
              class="hover:bg-muted/60 flex flex-col items-start gap-1 rounded-lg border p-3 text-left transition-colors {form.method === m.id ? 'border-primary ring-primary/30 bg-muted/40 ring-2' : ''}"
              onclick={() => { form!.method = m.id; secret = '' }}>
              <span class="flex w-full items-center gap-1.5 text-sm font-medium">{m.name}
                {#if status.method === m.id && status.configured}<Badge class="ml-auto h-5 px-1.5 text-[10px]">In use</Badge>{/if}
              </span>
              <span class="text-muted-foreground text-xs">{m.help}</span>
            </button>
          {/each}
        </div>

        {#if form.method}
          <div class="grid gap-4 sm:grid-cols-2">
            <div class="flex flex-col gap-1.5"><Label for="em-from">Sender address</Label><Input id="em-from" type="email" bind:value={form.fromEmail} placeholder="noreply@example.com" /></div>
            <div class="flex flex-col gap-1.5"><Label for="em-name">Sender name</Label><Input id="em-name" bind:value={form.fromName} placeholder="Pimpbase" /></div>
            <div class="flex flex-col gap-1.5 sm:col-span-2">
              <Label for="em-reply">Replies go to <span class="text-muted-foreground font-normal">(optional)</span></Label>
              <Input id="em-reply" type="email" bind:value={form.replyTo} placeholder="support@example.com" />
            </div>
          </div>

          {#if form.method === 'smtp'}
            <div class="grid gap-4 rounded-lg border p-4 sm:grid-cols-[1fr_7rem]">
              <div class="flex flex-col gap-1.5"><Label for="em-host">Server</Label><Input id="em-host" bind:value={form.smtpHost} placeholder="smtp.example.com" /></div>
              <div class="flex flex-col gap-1.5"><Label for="em-port">Port</Label><Input id="em-port" bind:value={form.smtpPort} placeholder="587" /></div>
              <div class="flex flex-col gap-1.5 sm:col-span-2">
                <Label>Security</Label>
                <Select.Root type="single" value={form.smtpSecurity || 'starttls'} onValueChange={(v) => (form!.smtpSecurity = v as EmailSettings['smtpSecurity'])}>
                  <Select.Trigger class="w-full">{security[(form.smtpSecurity || 'starttls') as keyof typeof security]}</Select.Trigger>
                  <Select.Content>{#each Object.entries(security) as [k, v] (k)}<Select.Item value={k}>{v}</Select.Item>{/each}</Select.Content>
                </Select.Root>
              </div>
              <div class="flex flex-col gap-1.5"><Label for="em-user">User name</Label><Input id="em-user" bind:value={form.smtpUser} autocomplete="off" /></div>
              <div class="flex flex-col gap-1.5 sm:col-span-1">
                <Label for="em-pass">Password</Label>
                <Input id="em-pass" type="password" bind:value={secret} autocomplete="new-password" placeholder={status.passwordSet ? 'Saved; type to replace' : ''} />
              </div>
            </div>
          {:else if usesKey}
            <div class="grid gap-4 rounded-lg border p-4 sm:grid-cols-2">
              <div class="flex flex-col gap-1.5 sm:col-span-2">
                <Label for="em-key">{form.method === 'postmark' ? 'Server token' : 'API key'}</Label>
                <Input id="em-key" type="password" bind:value={secret} autocomplete="off" placeholder={keySet ? 'Saved; paste a new one to replace it' : 'Paste the key'} />
              </div>
              {#if form.method === 'mailgun'}
                <div class="flex flex-col gap-1.5"><Label for="em-domain">Sending domain</Label><Input id="em-domain" bind:value={form.domain} placeholder="mg.example.com" /></div>
                <div class="flex flex-col gap-1.5">
                  <Label>Region</Label>
                  <Select.Root type="single" value={form.region || 'us'} onValueChange={(v) => (form!.region = v)}>
                    <Select.Trigger class="w-full">{form.region === 'eu' ? 'EU' : 'US'}</Select.Trigger>
                    <Select.Content><Select.Item value="us">US</Select.Item><Select.Item value="eu">EU</Select.Item></Select.Content>
                  </Select.Root>
                </div>
              {/if}
            </div>
          {:else if form.method === 'log'}
            <p class="text-muted-foreground rounded-lg border border-dashed p-3 text-xs">Emails are written to the server log and kept in flow run logs. Nobody receives them.</p>
          {/if}
        {/if}

        <label class="flex items-start justify-between gap-4 rounded-lg border p-3">
          <span><span class="block text-sm font-medium">Sign-in links</span><span class="text-muted-foreground block text-xs">Let people sign in with a link emailed to them, without their password. The link works once, for 15 minutes.</span></span>
          <Switch bind:checked={form.emailLinks} />
        </label>

        <div class="flex flex-wrap items-center gap-2">
          <Button onclick={save} disabled={saving || !dirty}>{#if saving}<Spinner />{/if}Save</Button>
          <div class="ml-auto flex items-center gap-2">
            <Input class="h-9 w-56" type="email" bind:value={testTo} placeholder={session.user?.email ?? 'Send a test to…'} aria-label="Test address" />
            <Button variant="outline" onclick={test} disabled={testing || !status.configured || dirty} title={dirty ? 'Save first' : ''}>{#if testing}<Spinner />{:else}<SendIcon />{/if}Send test email</Button>
          </div>
        </div>
      </Card.Content>
    </Card.Root>

    <Card.Root>
      <Card.Header>
        <Card.Title>System emails</Card.Title>
        <Card.Description>The emails Pimpbase sends on its own. Change their words and look; the links in them are added for you.</Card.Description>
      </Card.Header>
      <Card.Content class="flex flex-col divide-y rounded-lg border p-0 mx-6">
        {#each systems as s (s.key)}
          <div class="flex items-center gap-3 px-4 py-3" data-system={s.key}>
            <MailIcon class="text-muted-foreground size-4 shrink-0" />
            <div class="min-w-0 flex-1">
              <div class="flex items-center gap-2 text-sm font-medium">{s.name}{#if s.custom}<Badge variant="secondary" class="h-5 px-1.5 text-[10px]">Customized</Badge>{/if}</div>
              <div class="text-muted-foreground truncate text-xs">{what[s.key] ?? ''}</div>
            </div>
            <Button variant="outline" size="sm" onclick={() => edit(s)}><PencilIcon />Edit</Button>
          </div>
        {/each}
      </Card.Content>
    </Card.Root>
  </div>
{/if}

<Dialog.Root open={open} onOpenChange={close}>
  <Dialog.Content class="flex h-[90vh] flex-col gap-3 sm:max-w-6xl">
    {#if editing && draft}
      <Dialog.Header>
        <Dialog.Title class="flex items-center gap-2">{editing.name}{#if editing.custom}<Badge variant="secondary" class="h-5 px-1.5 text-[10px]">Customized</Badge>{/if}{#if changed}<span class="text-muted-foreground text-xs font-normal">Unsaved changes</span>{/if}</Dialog.Title>
        <Dialog.Description>{what[editing.key] ?? ''}</Dialog.Description>
      </Dialog.Header>
      <div class="grid min-h-0 flex-1 gap-4 lg:grid-cols-[1fr_24rem]">
        <div class="min-h-0 overflow-y-auto">
          <EmailPreview html={rendered?.html} text={rendered?.text} subject={rendered?.subject} preheader={rendered?.preheader}
            from={status?.fromName || 'Pimpbase'} to="a sample person" warnings={rendered?.warnings ?? []} error={previewError} loading={rendering}
            bind:selected selectable={draft.mode === 'blocks'} badge="sample data" />
        </div>
        <aside class="min-h-0 overflow-y-auto">
          <EmailEditor {draft} {onChange} bind:selected system />
        </aside>
      </div>
      <div class="flex flex-wrap items-center gap-2 border-t pt-3">
        <Button variant="outline" onclick={resetSystem} disabled={!!busy || !editing.custom}>{#if busy === 'reset'}<Spinner />{:else}<RotateCcwIcon />{/if}Back to the default</Button>
        <Button variant="outline" onclick={sendSystem} disabled={!!busy || !status?.configured}>{#if busy === 'send'}<Spinner />{:else}<SendIcon />{/if}Send me a test</Button>
        <Button class="ml-auto" onclick={saveSystem} disabled={!!busy || !changed}>{#if busy === 'save'}<Spinner />{/if}Save</Button>
      </div>
    {/if}
  </Dialog.Content>
</Dialog.Root>
