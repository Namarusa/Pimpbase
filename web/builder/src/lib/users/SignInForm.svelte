<script lang="ts">
  // SignInForm signs in with a password or a single sign-on provider. It
  // shows the error a provider sent back with ?sso_error=, and, when email
  // is set up, offers a password reset and sign-in links by email.
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import { api, type PublicProvider, type User } from '../api'
  import ProviderIcon from './ProviderIcon.svelte'
  import MailIcon from '@lucide/svelte/icons/mail'
  import MailCheckIcon from '@lucide/svelte/icons/mail-check'

  let { providers, onSignedIn, returnTo }: { providers: PublicProvider[]; onSignedIn: (u: User) => void; returnTo?: string } = $props()
  let email = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)
  let showPassword = $state(false)
  let mode = $state<'password' | 'forgot' | 'link' | 'sent'>('password')
  let options = $state({ passwordReset: false, emailLinks: false })
  api.authOptions().then((o) => (options = o)).catch(() => {})

  const params = new URLSearchParams(location.search)
  for (const k of ['sso_error', 'linkError']) {
    if (params.get(k)) {
      error = params.get(k)!
      params.delete(k)
      history.replaceState(null, '', location.pathname + (params.size ? `?${params}` : ''))
    }
  }
  async function sendLink(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    busy = true
    try {
      const next = returnTo ?? location.pathname + location.search
      if (mode === 'forgot') await api.forgotPassword(email, next)
      else await api.emailLink(email, next)
      mode = 'sent'
    } catch (err: any) { error = err.message } finally { busy = false }
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
  {#if (!providers.length || showPassword) && mode === 'password'}
    <form class="flex flex-col gap-4" onsubmit={submit}>
      <div class="flex flex-col gap-2"><Label for="si-email">Email</Label><Input id="si-email" type="email" bind:value={email} required autocomplete="email" /></div>
      <div class="flex flex-col gap-2">
        <div class="flex items-center justify-between">
          <Label for="si-pw">Password</Label>
          {#if options.passwordReset}<button type="button" class="text-muted-foreground hover:text-foreground text-xs underline-offset-4 hover:underline" onclick={() => { mode = 'forgot'; error = '' }}>Forgot password?</button>{/if}
        </div>
        <Input id="si-pw" type="password" bind:value={password} required autocomplete="current-password" />
      </div>
      <Button type="submit" disabled={busy} class="w-full">{#if busy}<Spinner />{/if}Sign in</Button>
      {#if options.emailLinks}
        <Button type="button" variant="outline" class="w-full" onclick={() => { mode = 'link'; error = '' }}><MailIcon />Email me a sign-in link</Button>
      {/if}
    </form>
  {:else if mode === 'forgot' || mode === 'link'}
    <form class="flex flex-col gap-4" onsubmit={sendLink}>
      <p class="text-muted-foreground text-sm">{mode === 'forgot' ? 'Enter your email address and we send you a link to choose a new password.' : 'Enter your email address and we send you a link that signs you in.'}</p>
      <div class="flex flex-col gap-2"><Label for="si-link-email">Email</Label><Input id="si-link-email" type="email" bind:value={email} required autocomplete="email" /></div>
      <Button type="submit" disabled={busy} class="w-full">{#if busy}<Spinner />{/if}{mode === 'forgot' ? 'Send reset link' : 'Send sign-in link'}</Button>
      <button type="button" class="text-muted-foreground hover:text-foreground text-center text-xs underline-offset-4 hover:underline" onclick={() => (mode = 'password')}>Back to sign in</button>
    </form>
  {:else if mode === 'sent'}
    <div class="flex flex-col items-center gap-3 py-2 text-center">
      <span class="bg-primary/10 text-primary grid size-11 place-items-center rounded-full"><MailCheckIcon class="size-5" /></span>
      <p class="text-sm">If <strong>{email}</strong> has an account, an email with a link is on its way. It works for a short while only.</p>
      <button type="button" class="text-muted-foreground hover:text-foreground text-xs underline-offset-4 hover:underline" onclick={() => (mode = 'password')}>Back to sign in</button>
    </div>
  {/if}
</div>
