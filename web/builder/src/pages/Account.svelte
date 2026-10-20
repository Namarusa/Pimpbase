<script lang="ts">
  // Account is the signed-in person's profile, password and API keys.
  import * as Card from '$lib/components/ui/card'
  import * as Table from '$lib/components/ui/table'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Select from '$lib/components/ui/select'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Spinner } from '$lib/components/ui/spinner'
  import { toast } from 'svelte-sonner'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import CopyIcon from '@lucide/svelte/icons/copy'
  import TrashIcon from '@lucide/svelte/icons/trash-2'
  import KeyIcon from '@lucide/svelte/icons/key'
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert'
  import { api, type ApiKey, type App, type RoleDef } from '../lib/api'
  import { session } from '../lib/session.svelte'
  import { formatDate, timeAgo } from '../lib/format'
  import RoleSelect from '../lib/access/RoleSelect.svelte'
  import { roleName } from '../lib/access/roles'

  let keys = $state<ApiKey[]>([])
  let apps = $state<App[]>([])
  let roles = $state<Record<string, RoleDef[]>>({})
  let name = $state(session.user?.name ?? '')
  let password = $state('')
  let creating = $state(false)
  let form = $state({ name: '', appId: '', role: '', expires: '90' })
  let made = $state<ApiKey | null>(null)
  let error = $state('')
  let busy = $state(false)

  async function load() {
    try { [keys, apps] = await Promise.all([api.keys(), api.apps()]) } catch (e: any) { toast.error(e.message) }
  }
  load()
  $effect(() => {
    const id = form.appId
    if (id && !roles[id]) api.roles(id).then((r) => (roles[id] = r)).catch(() => {})
  })

  const appName = (id: string) => apps.find((a) => a.id === id)?.name ?? 'a removed app'
  const expiries = [{ value: '7', label: '7 days' }, { value: '30', label: '30 days' }, { value: '90', label: '90 days' }, { value: '365', label: '1 year' }, { value: '0', label: 'Never' }]
  const expired = (k: ApiKey) => !!k.expiresAt && new Date(k.expiresAt).getTime() < Date.now()

  async function saveName() {
    try { session.user = { ...session.user!, ...(await api.updateUser(session.user!.id, { name })) }; toast.success('Name saved') } catch (e: any) { toast.error(e.message) }
  }
  async function savePassword(e: SubmitEvent) {
    e.preventDefault()
    try {
      await api.updateUser(session.user!.id, { password })
      password = ''
      toast.success('Password changed; sign in again on your other devices')
      session.user = await api.me().catch(() => null)
    } catch (err: any) { toast.error(err.message) }
  }
  async function create(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    busy = true
    try {
      made = await api.createKey({ name: form.name, appId: form.appId || undefined, role: form.appId ? form.role || undefined : undefined, expiresInDays: Number(form.expires) })
      creating = false
      await load()
    } catch (err: any) { error = err.message } finally { busy = false }
  }
  async function revoke(k: ApiKey) {
    if (!confirm(`Revoke ${k.name}? Anything using it stops working at once.`)) return
    await api.deleteKey(k.id)
    await load()
  }
</script>

<div class="mx-auto flex max-w-4xl flex-col gap-5 px-4 py-6">
  <div>
    <h1 class="text-xl font-semibold">Your account</h1>
    <p class="text-muted-foreground text-sm">{session.user?.email} · {session.user?.role}</p>
  </div>
  <div class="grid gap-4 md:grid-cols-2">
    <Card.Root class="gap-3 py-4">
      <Card.Header class="px-5"><Card.Title class="text-sm">Profile</Card.Title></Card.Header>
      <Card.Content class="flex flex-col gap-1.5 px-5"><Label for="me-name">Name</Label>
        <div class="flex gap-2"><Input id="me-name" bind:value={name} /><Button variant="outline" onclick={saveName} disabled={name === session.user?.name}>Save</Button></div>
        {#if session.user?.groups?.length}<div class="flex flex-wrap gap-1 pt-2"><span class="text-muted-foreground text-xs">Groups from single sign-on:</span>{#each session.user.groups as g (g)}<Badge variant="secondary">{g}</Badge>{/each}</div>{/if}
      </Card.Content>
    </Card.Root>
    <Card.Root class="gap-3 py-4">
      <Card.Header class="px-5"><Card.Title class="text-sm">{session.user?.hasPassword ? 'Change password' : 'Set a password'}</Card.Title></Card.Header>
      <Card.Content class="px-5">
        <form class="flex gap-2" onsubmit={savePassword}>
          <Input type="password" bind:value={password} minlength={10} required placeholder="At least 10 characters" autocomplete="new-password" />
          <Button type="submit" variant="outline">Save</Button>
        </form>
      </Card.Content>
    </Card.Root>
  </div>

  <Card.Root class="gap-0 py-0">
    <Card.Header class="flex flex-row items-center gap-3 border-b px-5 py-4 [.border-b]:pb-4">
      <div class="flex-1">
        <Card.Title class="text-sm">API keys</Card.Title>
        <Card.Description class="text-xs">Send a key as <code>Authorization: Bearer …</code>. A key acts as you; limit it to one app and role for scripts and integrations.</Card.Description>
      </div>
      <Button size="sm" onclick={() => { form = { name: '', appId: '', role: '', expires: '90' }; error = ''; creating = true }}><PlusIcon />New key</Button>
    </Card.Header>
    {#if made?.key}
      <div class="flex flex-col gap-2 border-b bg-emerald-500/10 px-5 py-3">
        <div class="flex items-center gap-2 text-sm font-medium"><TriangleAlertIcon class="size-4" />Copy {made.name} now. It is not shown again.</div>
        <div class="flex gap-2"><Input readonly value={made.key} class="bg-background font-mono text-xs" />
          <Button variant="outline" onclick={() => { navigator.clipboard?.writeText(made!.key!); toast.success('Copied') }}><CopyIcon />Copy</Button>
          <Button variant="ghost" onclick={() => (made = null)}>Done</Button></div>
      </div>
    {/if}
    <Table.Root>
      <Table.Header><Table.Row><Table.Head class="pl-5">Name</Table.Head><Table.Head>Works for</Table.Head><Table.Head>Expires</Table.Head><Table.Head>Last used</Table.Head><Table.Head class="w-10 pr-5"></Table.Head></Table.Row></Table.Header>
      <Table.Body>
        {#each keys as k (k.id)}
          <Table.Row>
            <Table.Cell class="pl-5"><div class="flex items-center gap-2 font-medium"><KeyIcon class="text-muted-foreground size-4" />{k.name}</div>
              <div class="text-muted-foreground text-xs">Made {formatDate(k.createdAt)}</div></Table.Cell>
            <Table.Cell>{#if k.appId}<Badge variant="secondary">{appName(k.appId)}{k.role ? ` · ${roleName(roles[k.appId] ?? [], k.role)}` : ''}</Badge>{:else}<span class="text-sm">Everything you can do</span>{/if}</Table.Cell>
            <Table.Cell class="text-xs">{#if expired(k)}<Badge variant="destructive">Expired</Badge>{:else}{k.expiresAt ? formatDate(k.expiresAt) : 'Never'}{/if}</Table.Cell>
            <Table.Cell class="text-muted-foreground text-xs">{k.lastUsedAt ? timeAgo(k.lastUsedAt) : 'Never'}</Table.Cell>
            <Table.Cell class="pr-5"><Button variant="ghost" size="icon" class="hover:text-destructive size-8" aria-label="Revoke" onclick={() => revoke(k)}><TrashIcon /></Button></Table.Cell>
          </Table.Row>
        {:else}
          <Table.Row><Table.Cell colspan={5} class="text-muted-foreground py-8 text-center text-sm">No API keys yet.</Table.Cell></Table.Row>
        {/each}
      </Table.Body>
    </Table.Root>
  </Card.Root>
</div>

<Dialog.Root bind:open={creating}>
  <Dialog.Content class="sm:max-w-md">
    <form class="flex flex-col gap-4" onsubmit={create}>
      <Dialog.Header><Dialog.Title>New API key</Dialog.Title><Dialog.Description>Give each script or integration its own key, so you can revoke one without the others.</Dialog.Description></Dialog.Header>
      {#if error}<p class="text-destructive bg-destructive/10 rounded-md px-3 py-2 text-sm">{error}</p>{/if}
      <div class="flex flex-col gap-1.5"><Label for="k-name">Name</Label><Input id="k-name" bind:value={form.name} required placeholder="Nightly sync" /></div>
      <div class="grid gap-3 sm:grid-cols-2">
        <div class="flex flex-col gap-1.5"><Label>Works for</Label>
          <Select.Root type="single" value={form.appId || 'all'} onValueChange={(v) => { form.appId = v === 'all' ? '' : v; form.role = '' }}>
            <Select.Trigger class="w-full">{form.appId ? appName(form.appId) : 'Everything you can do'}</Select.Trigger>
            <Select.Content><Select.Item value="all" label="Everything">Everything you can do</Select.Item>
              {#each apps as a (a.id)}<Select.Item value={a.id} label={a.name}>Only {a.name}</Select.Item>{/each}</Select.Content>
          </Select.Root></div>
        <div class="flex flex-col gap-1.5"><Label>Expires</Label>
          <Select.Root type="single" bind:value={form.expires}>
            <Select.Trigger class="w-full">{expiries.find((x) => x.value === form.expires)?.label}</Select.Trigger>
            <Select.Content>{#each expiries as x (x.value)}<Select.Item value={x.value} label={x.label}>{x.label}</Select.Item>{/each}</Select.Content>
          </Select.Root></div>
      </div>
      {#if form.appId}
        <div class="flex flex-col gap-1.5"><Label>Acts with the role</Label>
          <RoleSelect mode="assign" none noneLabel="Your own role" roles={roles[form.appId] ?? []} value={form.role || 'NONE'} onChange={(v) => (form.role = v === 'NONE' ? '' : v)} />
          <p class="text-muted-foreground text-xs">{form.role ? 'The key sees only what this role sees, and stops working if you lose the role.' : 'No role picked: the key acts with your own role in the app.'} App keys cannot change designs or users.</p></div>
      {/if}
      <Dialog.Footer>
        <Button type="button" variant="outline" onclick={() => (creating = false)}>Cancel</Button>
        <Button type="submit" disabled={busy}>{#if busy}<Spinner />{/if}Create key</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
