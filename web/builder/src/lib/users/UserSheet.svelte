<script lang="ts">
  // UserSheet edits one person: name, workspace role, app roles and
  // password.
  import { untrack } from 'svelte'
  import * as Sheet from '$lib/components/ui/sheet'
  import * as Select from '$lib/components/ui/select'
  import * as Avatar from '$lib/components/ui/avatar'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Separator } from '$lib/components/ui/separator'
  import { toast } from 'svelte-sonner'
  import ShieldIcon from '@lucide/svelte/icons/shield-check'
  import KeyRoundIcon from '@lucide/svelte/icons/key-round'
  import { api, type App, type RoleDef, type UserRow } from '../api'
  import { session } from '../session.svelte'
  import RoleSelect from '../access/RoleSelect.svelte'
  import { workspaceRoles } from './workspace'

  let { open = $bindable(false), user, apps, onChanged }: { open: boolean; user: UserRow | null; apps: App[]; onChanged: () => void } = $props()
  let name = $state('')
  let password = $state('')
  let appRoles = $state<Record<string, RoleDef[]>>({})

  // Reset the form when the sheet opens or shows someone else.
  $effect(() => {
    if (!open || !user?.id) return
    untrack(() => {
      name = user!.name
      password = ''
    })
  })
  $effect(() => {
    if (!open) return
    for (const a of apps) if (!appRoles[a.id]) api.roles(a.id).then((r) => (appRoles[a.id] = r)).catch(() => {})
  })

  const self = $derived(user?.id === session.user?.id)
  const builder = $derived(['owner', 'admin', 'builder'].includes(user?.role ?? ''))
  const grant = (appId: string) => user?.apps.find((g) => g.appId === appId)

  async function patch(p: Parameters<typeof api.updateUser>[1], done: string) {
    try {
      await api.updateUser(user!.id, p)
      toast.success(done)
      onChanged()
    } catch (e: any) { toast.error(e.message) }
  }
  async function setApp(appId: string, role: string) {
    try {
      await api.setMember(appId, user!.id, role)
      user!.apps = role === 'NONE' ? user!.apps.filter((g) => g.appId !== appId) : [...user!.apps.filter((g) => g.appId !== appId), { appId, role, source: 'manual' }]
      onChanged()
    } catch (e: any) { toast.error(e.message) }
  }
  function generate() {
    const words = ['amber', 'birch', 'cobalt', 'delta', 'ember', 'fjord', 'granite', 'harbor', 'indigo', 'juniper', 'krypton', 'lagoon', 'meadow', 'nimbus', 'orchid', 'pepper']
    const pick = () => words[crypto.getRandomValues(new Uint32Array(1))[0] % words.length]
    password = `${pick()}-${pick()}-${pick()}-${crypto.getRandomValues(new Uint32Array(1))[0] % 100}`
  }
</script>

<Sheet.Root bind:open>
  <Sheet.Content class="w-full gap-0 overflow-y-auto sm:max-w-lg">
    {#if user}
      <Sheet.Header class="border-b">
        <div class="flex items-center gap-3">
          <Avatar.Root class="size-11"><Avatar.Fallback class="bg-primary/10 text-primary font-medium">{(user.name || user.email).split(/\s+/).slice(0, 2).map((w) => w[0]?.toUpperCase()).join('')}</Avatar.Fallback></Avatar.Root>
          <div class="min-w-0">
            <Sheet.Title class="truncate">{user.name}</Sheet.Title>
            <Sheet.Description class="truncate">{user.email}</Sheet.Description>
          </div>
        </div>
        <div class="flex flex-wrap gap-1.5 pt-1">
          {#each user.signIn as m (m)}<Badge variant="outline">{#if m === 'password'}Password{:else}<KeyRoundIcon />{m}{/if}</Badge>{/each}
          {#if user.disabled}<Badge variant="destructive">Switched off</Badge>{/if}
          {#each user.groups ?? [] as g (g)}<Badge variant="secondary">{g}</Badge>{/each}
        </div>
      </Sheet.Header>
      <div class="flex flex-col gap-5 p-4">
        <div class="grid gap-3 sm:grid-cols-[1fr_10rem]">
          <div class="flex flex-col gap-1.5"><Label for="u-name">Name</Label>
            <Input id="u-name" bind:value={name} onblur={() => name !== user!.name && patch({ name }, 'Name saved')} /></div>
          <div class="flex flex-col gap-1.5"><Label>Workspace role</Label>
            <Select.Root type="single" value={user.role} disabled={self} onValueChange={(v) => patch({ role: v }, `${user!.name} is now ${v}`)}>
              <Select.Trigger class="w-full">{workspaceRoles.find((r) => r.value === user!.role)?.label}</Select.Trigger>
              <Select.Content>{#each workspaceRoles as r (r.value)}<Select.Item value={r.value} label={r.label}><div><div>{r.label}</div><div class="text-muted-foreground text-xs">{r.help}</div></div></Select.Item>{/each}</Select.Content>
            </Select.Root></div>
        </div>

        <Separator />
        <div class="flex flex-col gap-2">
          <h3 class="text-sm font-semibold">Apps</h3>
          {#if builder}
            <p class="text-muted-foreground flex items-center gap-1.5 text-sm"><ShieldIcon class="size-4" />Builders and admins have full access to every app.</p>
          {:else}
            {#each apps as a (a.id)}
              {@const g = grant(a.id)}
              <div class="flex items-center gap-3">
                <div class="min-w-0 flex-1"><div class="truncate text-sm font-medium">{a.name}</div>
                  {#if g?.source === 'sso'}<div class="text-muted-foreground text-xs">Given by single sign-on</div>{/if}</div>
                <RoleSelect class="w-48" mode="assign" none publicRole={false} roles={appRoles[a.id] ?? []} value={g?.role ?? 'NONE'} onChange={(v) => setApp(a.id, v)} />
              </div>
            {:else}<p class="text-muted-foreground text-sm">There are no apps yet.</p>{/each}
          {/if}
        </div>

        <Separator />
        <div class="flex flex-col gap-2">
          <h3 class="text-sm font-semibold">{user.hasPassword ? 'Reset password' : 'Set a password'}</h3>
          <p class="text-muted-foreground text-xs">{user.hasPassword ? 'Signs them out everywhere. Share the new password with them safely.' : 'They sign in with single sign-on today; a password lets them sign in without it too.'}</p>
          <div class="flex gap-2">
            <Input class="font-mono text-xs" bind:value={password} placeholder="At least 10 characters" autocomplete="off" />
            <Button variant="outline" onclick={generate}>Generate</Button>
            <Button disabled={password.length < 10} onclick={() => patch({ password }, 'Password changed').then(() => (password = ''))}>Save</Button>
          </div>
        </div>

        {#if !self}
          <Separator />
          <div class="flex items-center justify-between gap-3">
            <div><h3 class="text-sm font-semibold">{user.disabled ? 'Switched off' : 'Switch off'}</h3>
              <p class="text-muted-foreground text-xs">{user.disabled ? 'They cannot sign in and their API keys do not work.' : 'Signs them out and stops their API keys, keeping their account.'}</p></div>
            <Button variant={user.disabled ? 'default' : 'outline'} onclick={() => patch({ disabled: !user!.disabled }, user!.disabled ? 'Switched on' : 'Switched off')}>{user.disabled ? 'Switch on' : 'Switch off'}</Button>
          </div>
        {/if}
      </div>
    {/if}
  </Sheet.Content>
</Sheet.Root>
