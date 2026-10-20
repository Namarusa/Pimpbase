<script lang="ts">
  // Access is an app's people, roles and permissions.
  import * as Tabs from '$lib/components/ui/tabs'
  import * as Card from '$lib/components/ui/card'
  import * as Table from '$lib/components/ui/table'
  import * as Avatar from '$lib/components/ui/avatar'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import { Input } from '$lib/components/ui/input'
  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { toast } from 'svelte-sonner'
  import SearchIcon from '@lucide/svelte/icons/search'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import EllipsisIcon from '@lucide/svelte/icons/ellipsis'
  import PencilIcon from '@lucide/svelte/icons/pencil'
  import TrashIcon from '@lucide/svelte/icons/trash-2'
  import ShieldIcon from '@lucide/svelte/icons/shield-check'
  import KeyRoundIcon from '@lucide/svelte/icons/key-round'
  import MonitorIcon from '@lucide/svelte/icons/monitor'
  import PlugIcon from '@lucide/svelte/icons/plug'
  import WorkflowIcon from '@lucide/svelte/icons/workflow'
  import EyeIcon from '@lucide/svelte/icons/eye'
  import { api, type App, type Flow, type Query, type RoleDef, type Table as TableT, type UserRow } from '../lib/api'
  import type { Screen } from '../lib/screen/types'
  import { navigate } from '../lib/router.svelte'
  import AppNav from '../components/AppNav.svelte'
  import RoleSelect from '../lib/access/RoleSelect.svelte'
  import RoleDot from '../lib/access/RoleDot.svelte'
  import RoleDialog from '../lib/access/RoleDialog.svelte'
  import TablePermissions from '../lib/access/TablePermissions.svelte'
  import { chain, roleName } from '../lib/access/roles'
  import { timeAgo } from '../lib/format'

  let { app: slug, tab: initialTab = 'members' }: { app: string; tab?: string } = $props()
  let app = $state<App | null>(null)
  let roles = $state<RoleDef[]>([])
  let users = $state<UserRow[]>([])
  let tables = $state<TableT[]>([])
  let screens = $state<Screen[]>([])
  let queries = $state<Query[]>([])
  let flows = $state<Flow[]>([])
  let error = $state('')
  let tab = $state(initialTab)
  let search = $state('')
  let roleOpen = $state(false)
  let editing = $state<RoleDef | null>(null)

  async function load() {
    try {
      app = (await api.app(slug)).app
      const id = app.id
      ;[roles, users, tables, screens, queries, flows] = await Promise.all([
        api.roles(id), api.users(), api.tables(id), api.screens(id), api.queries(id), api.flows(id),
      ])
    } catch (e: any) { error = e.message }
  }
  load()

  const isBuilderRole = (r: string) => ['owner', 'admin', 'builder'].includes(r)
  const grant = (u: UserRow) => u.apps.find((g) => g.appId === app?.id)
  const people = $derived(users
    .filter((u) => !search || `${u.name} ${u.email}`.toLowerCase().includes(search.toLowerCase()))
    .sort((a, b) => Number(isBuilderRole(b.role)) - Number(isBuilderRole(a.role)) || Number(!!grant(b)) - Number(!!grant(a)) || a.email.localeCompare(b.email)))
  const memberCount = (role: string) => users.filter((u) => grant(u)?.role === role).length
  const initials = (u: UserRow) => (u.name || u.email).split(/\s+/).slice(0, 2).map((w) => w[0]?.toUpperCase()).join('')

  async function setRole(u: UserRow, role: string) {
    try {
      await api.setMember(app!.id, u.id, role)
      u.apps = role === 'NONE' ? u.apps.filter((g) => g.appId !== app!.id) : [...u.apps.filter((g) => g.appId !== app!.id), { appId: app!.id, role, source: 'manual' }]
      toast.success(role === 'NONE' ? `${u.name} no longer has access` : `${u.name} is now ${roleName(roles, role)}`)
    } catch (e: any) { toast.error(e.message) }
  }

  async function deleteRole(r: RoleDef) {
    const parent = roleName(roles, r.inherits ?? 'PUBLIC')
    if (!confirm(`Remove the ${r.name} role? Its members become ${parent}; screens, tables and queries that need it become admin-only.`)) return
    try {
      await api.deleteRole(app!.id, r.id)
      await load()
      toast.success(`Removed ${r.name}`)
    } catch (e: any) { toast.error(e.message) }
  }

  async function screenRole(s: Screen, role: string) {
    try { Object.assign(s, await api.updateScreen(app!.id, s.id, { role })) } catch (e: any) { toast.error(e.message) }
  }
  async function queryRole(q: Query, role: string) {
    try { Object.assign(q, await api.updateQuery(app!.id, q.id, { role })) } catch (e: any) { toast.error(e.message) }
  }
  async function flowChange(f: Flow, p: Partial<Flow>) {
    try { Object.assign(f, await api.updateFlow(app!.id, f.id, p)) } catch (e: any) { toast.error(e.message) }
  }
  const limited = (s: Screen) => {
    let n = 0
    const walk = (node: any) => { if (node.props?.roles?.length) n++; node.children?.forEach(walk) }
    walk(s.definition)
    return n
  }
  const buttonFlows = $derived(flows.filter((f) => f.trigger.type === 'manual'))
  const unattendedFlows = $derived(flows.filter((f) => f.trigger.type === 'schedule' || f.trigger.type === 'webhook'))
  function setTab(v: string) {
    tab = v
    history.replaceState(null, '', `/apps/${slug}/access${v === 'members' ? '' : '/' + v}`)
  }
</script>

{#if app}
  <AppNav {app} active="access" />
  <div class="mx-auto flex max-w-6xl flex-col gap-5 px-4 py-6">
    <div class="flex flex-wrap items-end gap-3">
      <div class="flex-1">
        <h1 class="text-xl font-semibold">Access</h1>
        <p class="text-muted-foreground text-sm">Who uses {app.name}, the roles they hold, and what each role may see and do.</p>
      </div>
      <Button variant="outline" size="sm" href={`/app/${app.slug}`} target="_blank"><EyeIcon />Preview as a role</Button>
    </div>
    {#if error}<p class="text-destructive text-sm">{error}</p>{/if}
    <Tabs.Root value={tab} onValueChange={setTab}>
      <Tabs.List>
        <Tabs.Trigger value="members">Members <Badge variant="secondary" class="ml-1 h-5 px-1.5">{users.filter((u) => grant(u)).length}</Badge></Tabs.Trigger>
        <Tabs.Trigger value="roles">Roles <Badge variant="secondary" class="ml-1 h-5 px-1.5">{roles.length}</Badge></Tabs.Trigger>
        <Tabs.Trigger value="permissions">Permissions</Tabs.Trigger>
      </Tabs.List>

      <Tabs.Content value="members" class="mt-3">
        <Card.Root class="gap-0 py-0">
          <div class="flex flex-wrap items-center gap-3 border-b px-4 py-3">
            <div class="relative w-full max-w-xs">
              <SearchIcon class="text-muted-foreground absolute top-2.5 left-2.5 size-4" />
              <Input class="h-9 pl-8" placeholder="Search people" bind:value={search} />
            </div>
            <p class="text-muted-foreground ml-auto text-xs">Add people on the <a class="text-foreground underline" href="/users" onclick={(e) => { e.preventDefault(); navigate('/users') }}>Users</a> page or with single sign-on.</p>
          </div>
          <Table.Root>
            <Table.Header><Table.Row>
              <Table.Head class="pl-4">Person</Table.Head><Table.Head>Role in {app.name}</Table.Head><Table.Head>Given by</Table.Head><Table.Head class="pr-4 text-right">Last sign-in</Table.Head>
            </Table.Row></Table.Header>
            <Table.Body>
              {#each people as u (u.id)}
                {@const g = grant(u)}
                <Table.Row class={u.disabled ? 'opacity-60' : ''}>
                  <Table.Cell class="pl-4">
                    <div class="flex items-center gap-3">
                      <Avatar.Root class="size-8"><Avatar.Fallback class="bg-primary/10 text-primary text-xs font-medium">{initials(u)}</Avatar.Fallback></Avatar.Root>
                      <div class="min-w-0"><div class="truncate font-medium">{u.name}{#if u.disabled}<Badge variant="outline" class="ml-2">Switched off</Badge>{/if}</div><div class="text-muted-foreground truncate text-xs">{u.email}</div></div>
                    </div>
                  </Table.Cell>
                  <Table.Cell class="w-64">
                    {#if isBuilderRole(u.role)}
                      <span class="text-muted-foreground inline-flex items-center gap-1.5 text-sm"><ShieldIcon class="size-4" />Builder, full access</span>
                    {:else}
                      <RoleSelect mode="assign" none publicRole={false} {roles} value={g?.role ?? 'NONE'} onChange={(v) => setRole(u, v)} />
                    {/if}
                  </Table.Cell>
                  <Table.Cell>
                    {#if g?.source === 'sso'}<Badge variant="outline"><KeyRoundIcon />Single sign-on</Badge>
                    {:else if g}<span class="text-muted-foreground text-xs">Builder</span>{/if}
                  </Table.Cell>
                  <Table.Cell class="text-muted-foreground pr-4 text-right text-xs">{u.lastLoginAt ? timeAgo(u.lastLoginAt) : 'Never'}</Table.Cell>
                </Table.Row>
              {/each}
            </Table.Body>
          </Table.Root>
        </Card.Root>
      </Tabs.Content>

      <Tabs.Content value="roles" class="mt-3 flex flex-col gap-3">
        <div class="flex items-center gap-3">
          <p class="text-muted-foreground flex-1 text-sm">Each role holds what the role it inherits holds. Admin holds every role, custom ones included.</p>
          <Button size="sm" onclick={() => { editing = null; roleOpen = true }}><PlusIcon />New role</Button>
        </div>
        <div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {#each roles as r (r.id)}
            <Card.Root class="gap-3 py-4">
              <Card.Header class="flex flex-row items-start gap-2.5 px-4">
                <span class="mt-1.5"><RoleDot color={r.color || '#64748b'} class="size-2.5" /></span>
                <div class="min-w-0 flex-1">
                  <Card.Title class="flex items-center gap-2 text-sm">{r.name}{#if r.builtin}<Badge variant="secondary" class="h-5 px-1.5 text-[10px]">Built in</Badge>{/if}</Card.Title>
                  <div class="text-muted-foreground font-mono text-[11px]">{r.id}</div>
                </div>
                {#if !r.builtin}
                  <DropdownMenu.Root>
                    <DropdownMenu.Trigger>{#snippet child({ props })}<Button {...props} variant="ghost" size="icon" class="-mt-1 size-7" aria-label="Role actions"><EllipsisIcon /></Button>{/snippet}</DropdownMenu.Trigger>
                    <DropdownMenu.Content align="end">
                      <DropdownMenu.Item onclick={() => { editing = r; roleOpen = true }}><PencilIcon />Edit</DropdownMenu.Item>
                      <DropdownMenu.Item variant="destructive" onclick={() => deleteRole(r)}><TrashIcon />Remove</DropdownMenu.Item>
                    </DropdownMenu.Content>
                  </DropdownMenu.Root>
                {/if}
              </Card.Header>
              <Card.Content class="flex flex-col gap-2.5 px-4">
                <p class="text-muted-foreground min-h-8 text-xs">{r.description || 'No description.'}</p>
                <div class="flex flex-wrap items-center gap-1 text-xs">
                  {#each chain(roles, r.id) as c, i (c)}
                    {#if i > 0}<span class="text-muted-foreground">›</span>{/if}
                    <span class={['inline-flex items-center gap-1 rounded px-1.5 py-0.5', i === 0 ? 'bg-muted font-medium' : 'text-muted-foreground']}><RoleDot color={roles.find((x) => x.id === c)?.color || '#64748b'} />{roleName(roles, c)}</span>
                  {/each}
                  {#if r.id === 'ADMIN'}<span class="text-muted-foreground">+ every custom role</span>{/if}
                </div>
                {#if r.id !== 'PUBLIC'}<div class="text-muted-foreground text-xs">{memberCount(r.id)} {memberCount(r.id) === 1 ? 'member' : 'members'}</div>{/if}
              </Card.Content>
            </Card.Root>
          {/each}
        </div>
      </Tabs.Content>

      <Tabs.Content value="permissions" class="mt-3 flex flex-col gap-6">
        <section class="flex flex-col gap-3">
          <h2 class="text-sm font-semibold">Tables</h2>
          {#if tables.length === 0}<p class="text-muted-foreground text-sm">This app has no tables yet.</p>{/if}
          {#each tables as t (t.id)}
            <TablePermissions app={app.id} table={t} {roles} onSaved={(n) => Object.assign(t, n)} />
          {/each}
        </section>

        <div class="grid gap-4 lg:grid-cols-2">
          <Card.Root class="gap-0 py-0">
            <Card.Header class="flex flex-row items-center gap-2 border-b px-5 py-3.5 [.border-b]:pb-3.5"><MonitorIcon class="text-muted-foreground size-4" /><Card.Title class="text-sm">Screens</Card.Title></Card.Header>
            <Card.Content class="divide-y px-0">
              {#each screens as s (s.id)}
                <div class="flex items-center gap-3 px-5 py-2.5">
                  <div class="min-w-0 flex-1"><div class="truncate text-sm font-medium">{s.name}</div>
                    <div class="text-muted-foreground truncate font-mono text-[11px]">{s.route}{#if limited(s)}<span class="font-sans"> · {limited(s)} {limited(s) === 1 ? 'component' : 'components'} limited to roles</span>{/if}</div></div>
                  <RoleSelect class="w-56" {roles} value={s.role} onChange={(v) => screenRole(s, v)} />
                </div>
              {:else}<p class="text-muted-foreground px-5 py-4 text-sm">No screens yet.</p>{/each}
            </Card.Content>
          </Card.Root>
          <div class="flex flex-col gap-4">
            <Card.Root class="gap-0 py-0">
              <Card.Header class="flex flex-row items-center gap-2 border-b px-5 py-3.5 [.border-b]:pb-3.5"><PlugIcon class="text-muted-foreground size-4" /><Card.Title class="text-sm">API queries</Card.Title></Card.Header>
              <Card.Content class="divide-y px-0">
                {#each queries as q (q.id)}
                  <div class="flex items-center gap-3 px-5 py-2.5">
                    <div class="min-w-0 flex-1 truncate text-sm font-medium">{q.name}</div>
                    <RoleSelect class="w-56" {roles} value={q.role} onChange={(v) => queryRole(q, v)} />
                  </div>
                {:else}<p class="text-muted-foreground px-5 py-4 text-sm">No API queries yet.</p>{/each}
              </Card.Content>
            </Card.Root>
            <Card.Root class="gap-0 py-0">
              <Card.Header class="flex flex-row items-center gap-2 border-b px-5 py-3.5 [.border-b]:pb-3.5"><WorkflowIcon class="text-muted-foreground size-4" /><Card.Title class="text-sm">Flows run from screens</Card.Title></Card.Header>
              <Card.Content class="divide-y px-0">
                {#each buttonFlows as f (f.id)}
                  <div class="flex flex-wrap items-center gap-3 px-5 py-2.5">
                    <div class="min-w-0 flex-1 truncate text-sm font-medium">{f.name}</div>
                    <RoleSelect class="w-48" {roles} value={f.trigger.role} onChange={(v) => flowChange(f, { trigger: { ...f.trigger, role: v } })} />
                    <select class="border-input bg-background h-8 rounded-md border px-2 text-xs" value={f.runAs ?? 'flow'} onchange={(e) => flowChange(f, { runAs: e.currentTarget.value as 'flow' | 'user' })} aria-label="Runs as">
                      <option value="flow">Runs with full access</option><option value="user">Runs as the user</option>
                    </select>
                  </div>
                {:else}<p class="text-muted-foreground px-5 py-4 text-sm">No flows with a screen button trigger.</p>{/each}
              </Card.Content>
            </Card.Root>
            <Card.Root class="gap-0 py-0">
              <Card.Header class="flex flex-row items-center gap-2 border-b px-5 py-3.5 [.border-b]:pb-3.5"><WorkflowIcon class="text-muted-foreground size-4" /><Card.Title class="text-sm">Scheduled and webhook flows</Card.Title></Card.Header>
              <Card.Content class="divide-y px-0">
                {#each unattendedFlows as f (f.id)}
                  <div class="flex flex-wrap items-center gap-3 px-5 py-2.5">
                    <div class="min-w-0 flex-1 truncate text-sm font-medium">{f.name} <span class="text-muted-foreground text-xs font-normal">{f.trigger.type === 'schedule' ? 'schedule' : 'webhook'}</span></div>
                    {#if f.runAs === 'user'}<RoleSelect class="w-48" mode="assign" {roles} value={f.runRole ?? 'BASIC'} onChange={(v) => flowChange(f, { runRole: v })} />{/if}
                    <select class="border-input bg-background h-8 rounded-md border px-2 text-xs" value={f.runAs ?? 'flow'} onchange={(e) => flowChange(f, { runAs: e.currentTarget.value as 'flow' | 'user', runRole: f.runRole || 'BASIC' })} aria-label="Runs as">
                      <option value="flow">Runs with full access</option><option value="user">Runs as a role</option>
                    </select>
                  </div>
                {:else}<p class="text-muted-foreground px-5 py-4 text-sm">No flows with a schedule or webhook trigger.</p>{/each}
              </Card.Content>
            </Card.Root>
          </div>
        </div>
      </Tabs.Content>
    </Tabs.Root>
  </div>
  <RoleDialog bind:open={roleOpen} app={app.id} {roles} role={editing} onSaved={() => load()} />
{:else if error}
  <p class="text-destructive p-6 text-sm">{error}</p>
{/if}
