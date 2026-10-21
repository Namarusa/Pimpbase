<script lang="ts">
  // ProviderSheet adds or edits a single sign-on provider and its role
  // mappings.
  import { untrack } from 'svelte'
  import * as Sheet from '$lib/components/ui/sheet'
  import * as Select from '$lib/components/ui/select'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import { Button } from '$lib/components/ui/button'
  import { Badge } from '$lib/components/ui/badge'
  import { Separator } from '$lib/components/ui/separator'
  import { Spinner } from '$lib/components/ui/spinner'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import TrashIcon from '@lucide/svelte/icons/trash-2'
  import FlaskIcon from '@lucide/svelte/icons/flask-conical'
  import { api, type App, type Mapping, type Provider, type ProviderKind, type RoleDef } from '../api'
  import RoleSelect from '../access/RoleSelect.svelte'
  import ProviderIcon from './ProviderIcon.svelte'

  let { open = $bindable(false), provider, kind, apps, redirectUrl, onSaved, onDeleted }: {
    open: boolean; provider: Provider | null; kind: ProviderKind; apps: App[]; redirectUrl: string
    onSaved: (p: Provider) => void; onDeleted: (id: string) => void
  } = $props()

  const blank = (k: ProviderKind): Provider => ({
    id: '', kind: k, name: { google: 'Google', microsoft: 'Microsoft', oidc: 'Company login' }[k], enabled: true, hasSecret: false,
    issuer: '', tenant: '', clientId: '', scopes: [], allowedDomains: [], autoCreate: true, defaultRole: 'member',
    trustEmail: false, groupsClaim: 'groups', mappings: [],
  })
  let p = $state<Provider>(blank('oidc'))
  let secret = $state('')
  let domains = $state('')
  let scopes = $state('')
  let error = $state('')
  let busy = $state(false)
  let appRoles = $state<Record<string, RoleDef[]>>({})
  let testEmail = $state('')
  let testGroups = $state('')
  let testResult = $state<{ workspace: string; apps: Record<string, string>; matched: number[] } | null>(null)

  // Reset the form each time the sheet opens, and only then.
  $effect(() => {
    if (!open) return
    const from = provider, k = kind
    untrack(() => {
      p = from ? structuredClone($state.snapshot(from)) as Provider : blank(k)
      secret = ''
      error = ''
      testResult = null
      domains = p.allowedDomains.join(', ')
      scopes = p.scopes.filter((s) => !['openid', 'email', 'profile'].includes(s)).join(' ')
    })
  })
  $effect(() => {
    if (!open) return
    for (const a of apps) if (!appRoles[a.id]) api.roles(a.id).then((r) => (appRoles[a.id] = r)).catch(() => {})
  })

  const claims = [
    { value: 'group', label: 'Group is' },
    { value: 'email_domain', label: 'Email domain is' },
    { value: 'email', label: 'Email is' },
  ]
  const workspaceRoles = [
    { value: '', label: 'Keep workspace role' },
    { value: 'member', label: 'Member' },
    { value: 'builder', label: 'Builder' },
    { value: 'admin', label: 'Admin' },
  ]
  const claimLabel = (c: string) => claims.find((x) => x.value === c)?.label ?? `Claim ${c} is`
  const appName = (id: string) => apps.find((a) => a.id === id)?.name ?? 'Removed app'

  function addMapping() {
    p.mappings.push({ claim: 'group', value: '', workspaceRole: '', apps: apps[0] ? [{ appId: apps[0].id, role: 'BASIC' }] : [] })
  }

  function body() {
    return {
      ...$state.snapshot(p),
      allowedDomains: domains.split(/[\s,]+/).filter(Boolean),
      scopes: scopes.split(/[\s,]+/).filter(Boolean),
      ...(secret ? { clientSecret: secret } : {}),
    }
  }

  async function save(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    busy = true
    try {
      const saved = provider ? await api.updateProvider(provider.id, body()) : await api.createProvider(body())
      onSaved(saved)
      open = false
    } catch (err: any) {
      error = err.message
    } finally {
      busy = false
    }
  }

  async function remove() {
    if (!provider || !confirm(`Remove ${provider.name}? People who signed in with it keep their accounts but need another way to sign in.`)) return
    await api.deleteProvider(provider.id)
    onDeleted(provider.id)
    open = false
  }

  async function test() {
    if (!provider) return
    try {
      testResult = await api.testMapping(provider.id, { email: testEmail, groups: testGroups.split(/[\s,]+/).filter(Boolean) })
    } catch (err: any) { error = err.message }
  }
</script>

<Sheet.Root bind:open>
  <Sheet.Content class="w-full gap-0 overflow-y-auto sm:max-w-xl">
    <form onsubmit={save} class="flex min-h-full flex-col">
      <Sheet.Header class="border-b">
        <Sheet.Title class="flex items-center gap-2"><ProviderIcon kind={p.kind} class="size-5" />{provider ? `Edit ${provider.name}` : `Add ${p.kind === 'oidc' ? 'an OpenID Connect' : p.kind === 'google' ? 'a Google' : 'a Microsoft'} provider`}</Sheet.Title>
        <Sheet.Description>People see a "Continue with {p.name || '…'}" button on the sign-in page of the builder and of every app.</Sheet.Description>
      </Sheet.Header>

      <div class="flex flex-1 flex-col gap-5 p-4">
        {#if error}<p class="text-destructive bg-destructive/10 rounded-md px-3 py-2 text-sm">{error}</p>{/if}
        <div class="bg-muted/50 flex flex-col gap-1 rounded-lg border p-3">
          <span class="text-xs font-medium">Redirect URI</span>
          <code class="text-xs break-all">{redirectUrl}</code>
          <span class="text-muted-foreground text-xs">Register it with {p.kind === 'google' ? 'your Google Cloud OAuth client' : p.kind === 'microsoft' ? 'your Entra ID app registration' : 'your identity provider'}.</span>
        </div>

        <div class="grid gap-3 sm:grid-cols-[1fr_auto]">
          <div class="flex flex-col gap-1.5"><Label for="p-name">Button label</Label><Input id="p-name" bind:value={p.name} maxlength={60} /></div>
          <div class="flex flex-col gap-1.5"><Label for="p-on">Switched on</Label><div class="flex h-9 items-center"><Switch id="p-on" bind:checked={p.enabled} /></div></div>
        </div>
        {#if p.kind === 'oidc'}
          <div class="flex flex-col gap-1.5"><Label for="p-issuer">Issuer URL</Label><Input id="p-issuer" bind:value={p.issuer} placeholder="https://login.example.com" required />
            <p class="text-muted-foreground text-xs">Pimpbase reads <code>/.well-known/openid-configuration</code> from it.</p></div>
        {:else if p.kind === 'microsoft'}
          <div class="flex flex-col gap-1.5"><Label for="p-tenant">Tenant</Label><Input id="p-tenant" bind:value={p.tenant} placeholder="organizations" />
            <p class="text-muted-foreground text-xs">Your directory (tenant) id or domain. Leave empty to accept any work or school account.</p></div>
        {/if}
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="flex flex-col gap-1.5"><Label for="p-client">Client id</Label><Input id="p-client" bind:value={p.clientId} required class="font-mono text-xs" /></div>
          <div class="flex flex-col gap-1.5"><Label for="p-secret">Client secret</Label>
            <Input id="p-secret" type="password" bind:value={secret} placeholder={p.hasSecret ? 'Stored; type to replace' : ''} required={!p.hasSecret} autocomplete="off" /></div>
        </div>
        <div class="grid gap-3 sm:grid-cols-2">
          <div class="flex flex-col gap-1.5"><Label for="p-scopes">Extra scopes</Label><Input id="p-scopes" bind:value={scopes} placeholder="groups" class="font-mono text-xs" />
            <p class="text-muted-foreground text-xs">openid, email and profile are always asked for.</p></div>
          <div class="flex flex-col gap-1.5"><Label for="p-groups">Groups claim</Label><Input id="p-groups" bind:value={p.groupsClaim} class="font-mono text-xs" />
            <p class="text-muted-foreground text-xs">The ID token claim that lists groups.</p></div>
        </div>
        <div class="flex flex-col gap-1.5"><Label for="p-domains">Allowed email domains</Label><Input id="p-domains" bind:value={domains} placeholder="example.com, example.org" />
          <p class="text-muted-foreground text-xs">Leave empty to accept any email the provider vouches for.</p></div>

        <div class="flex flex-col gap-3 rounded-lg border p-3">
          <div class="flex items-start justify-between gap-3">
            <div><Label for="p-auto">Make accounts on first sign-in</Label><p class="text-muted-foreground text-xs">Otherwise an admin adds people on the Users page first.</p></div>
            <Switch id="p-auto" bind:checked={p.autoCreate} />
          </div>
          {#if p.autoCreate}
            <div class="flex items-center justify-between gap-3">
              <Label class="font-normal">New accounts are</Label>
              <Select.Root type="single" bind:value={p.defaultRole}>
                <Select.Trigger size="sm" class="w-36">{p.defaultRole === 'builder' ? 'Builders' : 'Members'}</Select.Trigger>
                <Select.Content><Select.Item value="member" label="Members">Members</Select.Item><Select.Item value="builder" label="Builders">Builders</Select.Item></Select.Content>
              </Select.Root>
            </div>
          {/if}
          <Separator />
          <div class="flex items-start justify-between gap-3">
            <div><Label for="p-trust">Link to existing accounts by email</Label><p class="text-muted-foreground text-xs">Even when the provider does not mark the email as verified. Only for providers you control.</p></div>
            <Switch id="p-trust" bind:checked={p.trustEmail} />
          </div>
        </div>

        <div class="flex flex-col gap-2.5">
          <div class="flex items-center gap-2">
            <h3 class="text-sm font-semibold">Role mappings</h3>
            <span class="text-muted-foreground text-xs">Applied at every sign-in. The first rule that names an app wins; roles given by hand win over all rules.</span>
          </div>
          {#each p.mappings as m, i (i)}
            <div class="bg-muted/30 flex flex-col gap-2.5 rounded-lg border p-3">
              <div class="flex items-center gap-2">
                <Badge variant="secondary" class="h-5 px-1.5 font-mono text-[10px]">{i + 1}</Badge>
                <Select.Root type="single" bind:value={m.claim}>
                  <Select.Trigger size="sm" class="w-40">{claimLabel(m.claim)}</Select.Trigger>
                  <Select.Content>{#each claims as c (c.value)}<Select.Item value={c.value} label={c.label}>{c.label}</Select.Item>{/each}</Select.Content>
                </Select.Root>
                <Input class="h-8 flex-1" bind:value={m.value} placeholder={m.claim === 'email_domain' ? 'example.com' : m.claim === 'group' ? 'sales' : ''} required />
                <Button variant="ghost" size="icon" class="hover:text-destructive size-8" onclick={() => p.mappings.splice(i, 1)} aria-label="Remove the rule"><TrashIcon /></Button>
              </div>
              <div class="flex items-center gap-2 pl-7">
                <span class="text-muted-foreground w-24 text-xs">Workspace</span>
                <Select.Root type="single" bind:value={m.workspaceRole}>
                  <Select.Trigger size="sm" class="w-44">{workspaceRoles.find((w) => w.value === m.workspaceRole)?.label}</Select.Trigger>
                  <Select.Content>{#each workspaceRoles as w (w.value)}<Select.Item value={w.value} label={w.label}>{w.label}</Select.Item>{/each}</Select.Content>
                </Select.Root>
              </div>
              {#each m.apps as ar, j (j)}
                <div class="flex items-center gap-2 pl-7">
                  <Select.Root type="single" bind:value={ar.appId} onValueChange={() => (ar.role = 'BASIC')}>
                    <Select.Trigger size="sm" class="w-40 truncate">{appName(ar.appId)}</Select.Trigger>
                    <Select.Content>{#each apps as a (a.id)}<Select.Item value={a.id} label={a.name}>{a.name}</Select.Item>{/each}</Select.Content>
                  </Select.Root>
                  <RoleSelect mode="assign" publicRole={false} class="w-44" roles={appRoles[ar.appId] ?? []} value={ar.role} onChange={(v) => (ar.role = v)} />
                  <Button variant="ghost" size="icon" class="size-8" onclick={() => m.apps.splice(j, 1)} aria-label="Remove the app"><XIcon /></Button>
                </div>
              {/each}
              {#if apps.length}
                <Button variant="link" size="sm" class="h-auto self-start p-0 pl-7 text-xs" onclick={() => m.apps.push({ appId: apps[0].id, role: 'BASIC' })}><PlusIcon />Give an app role</Button>
              {/if}
            </div>
          {/each}
          <Button variant="outline" size="sm" class="self-start" onclick={addMapping}><PlusIcon />Add a rule</Button>
        </div>

        {#if provider}
          <div class="flex flex-col gap-2.5 rounded-lg border border-dashed p-3">
            <div class="flex items-center gap-2 text-sm font-medium"><FlaskIcon class="size-4" />Try the saved rules</div>
            <div class="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
              <Input class="h-8" bind:value={testEmail} placeholder="kim@example.com" />
              <Input class="h-8" bind:value={testGroups} placeholder="Groups: sales, staff" />
              <Button size="sm" variant="secondary" onclick={test}>Try</Button>
            </div>
            {#if testResult}
              <div class="text-xs">
                {#if testResult.matched.length === 0}<p class="text-muted-foreground">No rule matches; the person keeps their roles.</p>
                {:else}
                  <p>Rules {testResult.matched.join(', ')} match.{#if testResult.workspace} Workspace role: <b>{testResult.workspace}</b>.{/if}</p>
                  {#each Object.entries(testResult.apps) as [appId, role] (appId)}<p>{appName(appId)}: <b>{(appRoles[appId] ?? []).find((r) => r.id === role)?.name ?? role}</b></p>{/each}
                {/if}
              </div>
            {/if}
          </div>
        {/if}
      </div>

      <Sheet.Footer class="bg-background sticky bottom-0 flex-row border-t">
        {#if provider}<Button type="button" variant="ghost" class="text-destructive mr-auto" onclick={remove}><TrashIcon />Remove</Button>{/if}
        <Button type="button" variant="outline" onclick={() => (open = false)}>Cancel</Button>
        <Button type="submit" disabled={busy}>{#if busy}<Spinner />{/if}{provider ? 'Save provider' : 'Add provider'}</Button>
      </Sheet.Footer>
    </form>
  </Sheet.Content>
</Sheet.Root>
