<script lang="ts">
  // Users is the workspace's people and how they sign in.
  import * as Tabs from '$lib/components/ui/tabs'
  import * as Card from '$lib/components/ui/card'
  import * as Table from '$lib/components/ui/table'
  import * as Avatar from '$lib/components/ui/avatar'
  import * as Dialog from '$lib/components/ui/dialog'
  import * as Select from '$lib/components/ui/select'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as RadioGroup from '$lib/components/ui/radio-group'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Switch } from '$lib/components/ui/switch'
  import { Spinner } from '$lib/components/ui/spinner'
  import { toast } from 'svelte-sonner'
  import SearchIcon from '@lucide/svelte/icons/search'
  import UserPlusIcon from '@lucide/svelte/icons/user-plus'
  import EllipsisIcon from '@lucide/svelte/icons/ellipsis'
  import PencilIcon from '@lucide/svelte/icons/pencil'
  import TrashIcon from '@lucide/svelte/icons/trash-2'
  import PowerIcon from '@lucide/svelte/icons/power'
  import KeyRoundIcon from '@lucide/svelte/icons/key-round'
  import CopyIcon from '@lucide/svelte/icons/copy'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import { api, type App, type Provider, type ProviderKind, type RoleDef, type UserRow } from '../lib/api'
  import { roleName } from '../lib/access/roles'
  import { session } from '../lib/session.svelte'
  import { route } from '../lib/router.svelte'
  import AISettings from '../lib/ai/AISettings.svelte'
  import { timeAgo } from '../lib/format'
  import UserSheet from '../lib/users/UserSheet.svelte'
  import ProviderSheet from '../lib/users/ProviderSheet.svelte'
  import ProviderIcon from '../lib/users/ProviderIcon.svelte'
  import { workspaceRoles } from '../lib/users/workspace'

  let users = $state<UserRow[]>([])
  let apps = $state<App[]>([])
  let appRoles = $state<Record<string, RoleDef[]>>({})
  let providers = $state<Provider[]>([])
  let redirectUrl = $state('')
  let search = $state('')
  let roleFilter = $state('all')
  let tab = $state(route.path.endsWith('/sso') ? 'sso' : route.path.endsWith('/ai') ? 'ai' : 'people')
  let editing = $state<UserRow | null>(null)
  let sheetOpen = $state(false)
  let adding = $state(false)
  let form = $state({ email: '', name: '', password: '', role: 'member', method: 'password' })
  let addError = $state('')
  let busy = $state(false)
  let providerOpen = $state(false)
  let providerEditing = $state<Provider | null>(null)
  let providerKind = $state<ProviderKind>('oidc')

  async function load() {
    try {
      ;[users, apps] = await Promise.all([api.users(), api.apps()])
      if (editing) editing = users.find((u) => u.id === editing!.id) ?? null
      const defs = await Promise.all(apps.map((a) => api.roles(a.id).catch(() => [] as RoleDef[])))
      appRoles = Object.fromEntries(apps.map((a, i) => [a.id, defs[i]]))
      const sso = await api.ssoProviders()
      providers = sso.providers
      redirectUrl = sso.redirectUrl
    } catch (e: any) { toast.error(e.message) }
  }
  load()

  const shown = $derived(users.filter((u) =>
    (roleFilter === 'all' || u.role === roleFilter) &&
    (!search || `${u.name} ${u.email} ${u.groups?.join(' ')}`.toLowerCase().includes(search.toLowerCase()))))
  const initials = (u: UserRow) => (u.name || u.email).split(/\s+/).slice(0, 2).map((w) => w[0]?.toUpperCase()).join('')
  const appName = (id: string) => apps.find((a) => a.id === id)?.name ?? '?'
  function signedInWith(name: string) {
    const n = users.filter((u) => u.signIn.includes(name)).length
    return `${n} ${n === 1 ? 'person' : 'people'}`
  }
  const appRoleName = (app: string, r: string) => roleName(appRoles[app] ?? [], r)
  const roleLabel = (r: string) => workspaceRoles.find((x) => x.value === r)?.label ?? r

  async function setRole(u: UserRow, role: string) {
    try { await api.updateUser(u.id, { role }); u.role = role; toast.success(`${u.name} is now ${roleLabel(role).toLowerCase()}`) } catch (e: any) { toast.error(e.message) }
  }
  async function toggle(u: UserRow) {
    try { await api.updateUser(u.id, { disabled: !u.disabled }); u.disabled = !u.disabled } catch (e: any) { toast.error(e.message) }
  }
  async function remove(u: UserRow) {
    if (!confirm(`Remove ${u.email}? They lose access to every app, and their API keys stop working.`)) return
    try { await api.deleteUser(u.id); await load() } catch (e: any) { toast.error(e.message) }
  }
  async function add(e: SubmitEvent) {
    e.preventDefault()
    addError = ''
    busy = true
    try {
      await api.createUser({ ...form, ssoOnly: form.method === 'sso' } as any)
      adding = false
      toast.success(`Added ${form.email}`)
      await load()
    } catch (err: any) { addError = err.message } finally { busy = false }
  }
  function openAdd() {
    form = { email: '', name: '', password: '', role: 'member', method: 'password' }
    addError = ''
    adding = true
  }
  function openProvider(p: Provider | null, kind: ProviderKind = 'oidc') {
    providerEditing = p
    providerKind = p?.kind ?? kind
    providerOpen = true
  }
  async function toggleProvider(p: Provider) {
    try { Object.assign(p, await api.updateProvider(p.id, { ...$state.snapshot(p), enabled: !p.enabled })) } catch (e: any) { toast.error(e.message) }
  }
  function setTab(v: string) {
    tab = v
    history.replaceState(null, '', v === 'people' ? '/users' : `/users/${v}`)
  }
</script>

<div class="mx-auto flex max-w-6xl flex-col gap-5 px-4 py-6">
  <div class="flex flex-wrap items-end gap-3">
    <div class="flex-1">
      <h1 class="text-xl font-semibold">Users</h1>
      <p class="text-muted-foreground text-sm">Owners and admins manage people, sign-in and the AI assistant. Builders design apps. Members use the apps they hold a role in.</p>
    </div>
    {#if tab === 'people'}<Button onclick={openAdd}><UserPlusIcon />Add user</Button>{/if}
  </div>
  <Tabs.Root value={tab} onValueChange={setTab}>
    <Tabs.List>
      <Tabs.Trigger value="people">People <Badge variant="secondary" class="ml-1 h-5 px-1.5">{users.length}</Badge></Tabs.Trigger>
      <Tabs.Trigger value="sso">Single sign-on <Badge variant="secondary" class="ml-1 h-5 px-1.5">{providers.filter((p) => p.enabled).length}</Badge></Tabs.Trigger>
      <Tabs.Trigger value="ai">AI assistant</Tabs.Trigger>
    </Tabs.List>

    <Tabs.Content value="people" class="mt-3">
      <Card.Root class="gap-0 py-0">
        <div class="flex flex-wrap items-center gap-3 border-b px-4 py-3">
          <div class="relative w-full max-w-xs">
            <SearchIcon class="text-muted-foreground absolute top-2.5 left-2.5 size-4" />
            <Input class="h-9 pl-8" placeholder="Search name, email or group" bind:value={search} />
          </div>
          <Select.Root type="single" bind:value={roleFilter}>
            <Select.Trigger class="w-40">{roleFilter === 'all' ? 'Every role' : roleLabel(roleFilter) + 's'}</Select.Trigger>
            <Select.Content>
              <Select.Item value="all" label="Every role">Every role</Select.Item>
              {#each workspaceRoles as r (r.value)}<Select.Item value={r.value} label={r.label}>{r.label}s</Select.Item>{/each}
            </Select.Content>
          </Select.Root>
          <span class="text-muted-foreground ml-auto text-xs">{shown.length} of {users.length}</span>
        </div>
        <Table.Root>
          <Table.Header><Table.Row>
            <Table.Head class="pl-4">Person</Table.Head><Table.Head>Workspace role</Table.Head><Table.Head>Signs in with</Table.Head>
            <Table.Head>Apps</Table.Head><Table.Head>Last sign-in</Table.Head><Table.Head class="w-10 pr-4"></Table.Head>
          </Table.Row></Table.Header>
          <Table.Body>
            {#each shown as u (u.id)}
              <Table.Row class={u.disabled ? 'opacity-60' : ''}>
                <Table.Cell class="pl-4">
                  <button class="flex items-center gap-3 text-left" onclick={() => { editing = u; sheetOpen = true }}>
                    <Avatar.Root class="size-8"><Avatar.Fallback class="bg-primary/10 text-primary text-xs font-medium">{initials(u)}</Avatar.Fallback></Avatar.Root>
                    <div class="min-w-0">
                      <div class="flex items-center gap-1.5 font-medium">{u.name}
                        {#if u.id === session.user?.id}<Badge variant="secondary" class="h-5 px-1.5 text-[10px]">You</Badge>{/if}
                        {#if u.disabled}<Badge variant="outline" class="h-5 px-1.5 text-[10px]">Switched off</Badge>{/if}</div>
                      <div class="text-muted-foreground text-xs">{u.email}</div>
                    </div>
                  </button>
                </Table.Cell>
                <Table.Cell>
                  <Select.Root type="single" value={u.role} disabled={u.id === session.user?.id} onValueChange={(v) => setRole(u, v)}>
                    <Select.Trigger size="sm" class="w-28">{roleLabel(u.role)}</Select.Trigger>
                    <Select.Content>{#each workspaceRoles as r (r.value)}<Select.Item value={r.value} label={r.label}><div><div>{r.label}</div><div class="text-muted-foreground text-xs">{r.help}</div></div></Select.Item>{/each}</Select.Content>
                  </Select.Root>
                </Table.Cell>
                <Table.Cell>
                  <div class="flex flex-wrap gap-1">
                    {#each u.signIn as m (m)}
                      {@const p = providers.find((x) => x.name === m)}
                      <Badge variant="outline" class="gap-1">{#if m === 'password'}Password{:else}<ProviderIcon kind={p?.kind ?? 'oidc'} class="size-3" />{m}{/if}</Badge>
                    {:else}<span class="text-muted-foreground text-xs">Not yet</span>{/each}
                  </div>
                </Table.Cell>
                <Table.Cell>
                  {#if ['owner', 'admin', 'builder'].includes(u.role)}<span class="text-muted-foreground text-xs">All apps</span>
                  {:else}
                    <div class="flex flex-wrap gap-1">
                      {#each u.apps.slice(0, 2) as g (g.appId)}<Badge variant="secondary" class="font-normal">{appName(g.appId)} · {g.roleName || g.role}</Badge>{/each}
                      {#if u.apps.length > 2}<Badge variant="secondary">+{u.apps.length - 2}</Badge>{/if}
                      {#if u.apps.length === 0}<span class="text-muted-foreground text-xs">None</span>{/if}
                    </div>
                  {/if}
                </Table.Cell>
                <Table.Cell class="text-muted-foreground text-xs">{u.lastLoginAt ? timeAgo(u.lastLoginAt) : 'Never'}</Table.Cell>
                <Table.Cell class="pr-4">
                  <DropdownMenu.Root>
                    <DropdownMenu.Trigger>{#snippet child({ props })}<Button {...props} variant="ghost" size="icon" class="size-8" aria-label="Actions"><EllipsisIcon /></Button>{/snippet}</DropdownMenu.Trigger>
                    <DropdownMenu.Content align="end">
                      <DropdownMenu.Item onclick={() => { editing = u; sheetOpen = true }}><PencilIcon />Edit</DropdownMenu.Item>
                      {#if u.id !== session.user?.id}
                        <DropdownMenu.Item onclick={() => toggle(u)}><PowerIcon />{u.disabled ? 'Switch on' : 'Switch off'}</DropdownMenu.Item>
                        <DropdownMenu.Separator />
                        <DropdownMenu.Item variant="destructive" onclick={() => remove(u)}><TrashIcon />Remove</DropdownMenu.Item>
                      {/if}
                    </DropdownMenu.Content>
                  </DropdownMenu.Root>
                </Table.Cell>
              </Table.Row>
            {/each}
          </Table.Body>
        </Table.Root>
      </Card.Root>
    </Tabs.Content>

    <Tabs.Content value="sso" class="mt-3 flex flex-col gap-4">
      <Card.Root class="gap-3 py-4">
        <Card.Header class="px-5">
          <Card.Title class="text-sm">Add a sign-in provider</Card.Title>
          <Card.Description>People sign in with the account they already have. Role mappings turn their groups and email domain into workspace and app roles.</Card.Description>
        </Card.Header>
        <Card.Content class="flex flex-wrap gap-2 px-5">
          <Button variant="outline" onclick={() => openProvider(null, 'google')}><ProviderIcon kind="google" />Google</Button>
          <Button variant="outline" onclick={() => openProvider(null, 'microsoft')}><ProviderIcon kind="microsoft" />Microsoft Entra ID</Button>
          <Button variant="outline" onclick={() => openProvider(null, 'oidc')}><ProviderIcon kind="oidc" />Any OpenID Connect provider</Button>
          <div class="text-muted-foreground ml-auto flex items-center gap-2 text-xs">Redirect URI <code class="bg-muted rounded px-1.5 py-0.5">{redirectUrl}</code>
            <Button variant="ghost" size="icon" class="size-7" aria-label="Copy" onclick={() => { navigator.clipboard?.writeText(redirectUrl); toast.success('Copied') }}><CopyIcon /></Button></div>
        </Card.Content>
      </Card.Root>
      {#if providers.length === 0}
        <p class="text-muted-foreground py-6 text-center text-sm">No providers yet. Everyone signs in with a password.</p>
      {/if}
      <div class="grid gap-3 md:grid-cols-2">
        {#each providers as p (p.id)}
          <Card.Root class="gap-3 py-4">
            <Card.Header class="flex flex-row items-start gap-3 px-5">
              <span class="bg-muted grid size-10 shrink-0 place-items-center rounded-lg"><ProviderIcon kind={p.kind} class="size-5" /></span>
              <div class="min-w-0 flex-1">
                <Card.Title class="text-sm">{p.name}</Card.Title>
                <Card.Description class="truncate text-xs">{p.kind === 'google' ? 'accounts.google.com' : p.kind === 'microsoft' ? `Microsoft Entra ID · ${p.tenant || 'any organisation'}` : p.issuer}</Card.Description>
              </div>
              <Switch checked={p.enabled} onCheckedChange={() => toggleProvider(p)} aria-label="Switched on" />
            </Card.Header>
            <Card.Content class="flex flex-col gap-2.5 px-5">
              <div class="flex flex-wrap gap-1.5 text-xs">
                <Badge variant="secondary">{p.mappings.length} {p.mappings.length === 1 ? 'rule' : 'rules'}</Badge>
                {#if p.autoCreate}<Badge variant="secondary">Makes accounts as {p.defaultRole}s</Badge>{:else}<Badge variant="secondary">Existing accounts only</Badge>{/if}
                {#each p.allowedDomains as d (d)}<Badge variant="outline">@{d}</Badge>{/each}
              </div>
              {#each p.mappings.slice(0, 3) as m, i (i)}
                <div class="text-muted-foreground truncate text-xs">
                  <span class="text-foreground">{m.claim === 'group' ? 'Group' : m.claim === 'email_domain' ? 'Domain' : m.claim} = {m.value}</span> →
                  {[m.workspaceRole && `workspace ${m.workspaceRole}`, ...m.apps.map((a) => `${appName(a.appId)} ${appRoleName(a.appId, a.role)}`)].filter(Boolean).join(', ')}
                </div>
              {/each}
              {#if p.mappings.length > 3}<div class="text-muted-foreground text-xs">and {p.mappings.length - 3} more</div>{/if}
              <div class="flex items-center gap-2 pt-1">
                <span class="text-muted-foreground text-xs">{signedInWith(p.name)} signed in with it</span>
                <Button variant="outline" size="sm" class="ml-auto" onclick={() => openProvider(p)}><PencilIcon />Edit</Button>
              </div>
            </Card.Content>
          </Card.Root>
        {/each}
      </div>
    </Tabs.Content>

    <Tabs.Content value="ai" class="mt-3"><AISettings /></Tabs.Content>
  </Tabs.Root>
</div>

<UserSheet bind:open={sheetOpen} user={editing} {apps} onChanged={load} />
<ProviderSheet bind:open={providerOpen} provider={providerEditing} kind={providerKind} {apps} {redirectUrl}
  onSaved={() => load()} onDeleted={() => load()} />

<Dialog.Root bind:open={adding}>
  <Dialog.Content class="sm:max-w-md">
    <form class="flex flex-col gap-4" onsubmit={add}>
      <Dialog.Header><Dialog.Title>Add user</Dialog.Title><Dialog.Description>Give them app roles after adding them, or let single sign-on rules do it.</Dialog.Description></Dialog.Header>
      {#if addError}<p class="text-destructive bg-destructive/10 rounded-md px-3 py-2 text-sm">{addError}</p>{/if}
      <div class="grid gap-3 sm:grid-cols-2">
        <div class="flex flex-col gap-1.5"><Label for="a-email">Email</Label><Input id="a-email" type="email" bind:value={form.email} required /></div>
        <div class="flex flex-col gap-1.5"><Label for="a-name">Name</Label><Input id="a-name" bind:value={form.name} /></div>
      </div>
      <div class="flex flex-col gap-1.5"><Label>Workspace role</Label>
        <Select.Root type="single" bind:value={form.role}>
          <Select.Trigger class="w-full">{roleLabel(form.role)}</Select.Trigger>
          <Select.Content>{#each workspaceRoles as r (r.value)}<Select.Item value={r.value} label={r.label}><div><div>{r.label}</div><div class="text-muted-foreground text-xs">{r.help}</div></div></Select.Item>{/each}</Select.Content>
        </Select.Root></div>
      <RadioGroup.Root bind:value={form.method} class="grid gap-2 sm:grid-cols-2">
        <Label class="has-[[data-state=checked]]:border-primary flex cursor-pointer items-start gap-2 rounded-lg border p-3 font-normal"><RadioGroup.Item value="password" class="mt-0.5" /><div><div class="font-medium">Password</div><div class="text-muted-foreground text-xs">You set a first password</div></div></Label>
        <Label class={['has-[[data-state=checked]]:border-primary flex cursor-pointer items-start gap-2 rounded-lg border p-3 font-normal', providers.length === 0 && 'opacity-50']}><RadioGroup.Item value="sso" disabled={providers.length === 0} class="mt-0.5" /><div><div class="font-medium">Single sign-on only</div><div class="text-muted-foreground text-xs">{providers.length ? 'They sign in with a provider' : 'Add a provider first'}</div></div></Label>
      </RadioGroup.Root>
      {#if form.method === 'password'}
        <div class="flex flex-col gap-1.5"><Label for="a-pw">First password</Label><Input id="a-pw" class="font-mono" bind:value={form.password} minlength={10} required autocomplete="off" /></div>
      {/if}
      <Dialog.Footer>
        <Button type="button" variant="outline" onclick={() => (adding = false)}>Cancel</Button>
        <Button type="submit" disabled={busy}>{#if busy}<Spinner />{/if}<PlusIcon />Add user</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
