<script lang="ts">
  // SignInForm signs in with a password or a single sign-on provider. It
  // shows the error a provider sent back with ?sso_error=.
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import { api, type PublicProvider, type User } from '../api'
  import ProviderIcon from './ProviderIcon.svelte'

  let { providers, onSignedIn, returnTo }: { providers: PublicProvider[]; onSignedIn: (u: User) => void; returnTo?: string } = $props()
  let email = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)
  let showPassword = $state(false)

  const params = new URLSearchParams(location.search)
  if (params.get('sso_error')) {
    error = params.get('sso_error')!
    params.delete('sso_error')
    history.replaceState(null, '', location.pathname + (params.size ? `?${params}` : ''))
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    busy = true
    try {
      onSignedIn(await api.login(email, password))
    } catch (err: any) {
      error = err.message
    } finally {
      busy = false
    }
  }
  const sso = (p: PublicProvider) => { location.href = api.ssoStartURL(p.id, returnTo ?? location.pathname + location.search) }
</script>

<div class="flex flex-col gap-4">
  {#if error}<p class="text-destructive bg-destructive/10 rounded-md px-3 py-2 text-sm" role="alert">{error}</p>{/if}
  {#if providers.length}
    <div class="flex flex-col gap-2">
      {#each providers as p (p.id)}
        <Button variant="outline" class="h-10 w-full" onclick={() => sso(p)}><ProviderIcon kind={p.kind} />Continue with {p.name}</Button>
      {/each}
    </div>
    {#if !showPassword}
      <button class="text-muted-foreground hover:text-foreground text-center text-xs underline-offset-4 hover:underline" onclick={() => (showPassword = true)}>Sign in with email and password</button>
    {:else}
      <div class="text-muted-foreground flex items-center gap-3 text-xs"><span class="bg-border h-px flex-1"></span>or<span class="bg-border h-px flex-1"></span></div>
    {/if}
  {/if}
  {#if !providers.length || showPassword}
    <form class="flex flex-col gap-4" onsubmit={submit}>
      <div class="flex flex-col gap-2"><Label for="si-email">Email</Label><Input id="si-email" type="email" bind:value={email} required autocomplete="email" /></div>
      <div class="flex flex-col gap-2"><Label for="si-pw">Password</Label><Input id="si-pw" type="password" bind:value={password} required autocomplete="current-password" /></div>
      <Button type="submit" disabled={busy} class="w-full">{#if busy}<Spinner />{/if}Sign in</Button>
    </form>
  {/if}
</div>
