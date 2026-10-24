<script lang="ts">
  import * as Card from '$lib/components/ui/card'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import { api } from '../lib/api'
  import { session } from '../lib/session.svelte'
  import { navigate } from '../lib/router.svelte'

  // Reset is where invitation and password-reset links land: it shows who
  // the link is for and sets the new password, which signs them in.
  const q = new URLSearchParams(location.search)
  const token = q.get('token') ?? ''
  const next = (q.get('next') ?? '').startsWith('/') && !(q.get('next') ?? '').startsWith('//') ? q.get('next')! : '/'
  let info = $state<{ email: string; name: string; kind: 'invite' | 'reset' } | null>(null)
  let linkError = $state('')
  let password = $state('')
  let again = $state('')
  let error = $state('')
  let busy = $state(false)
  api.linkInfo(token).then((i) => (info = i)).catch((e) => (linkError = e.message))

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    if (password !== again) { error = 'The two passwords differ.'; return }
    error = ''
    busy = true
    try {
      session.user = await api.resetPassword(token, password)
      session.ready = true
      if (next.startsWith('/app/')) location.href = next
      else navigate(next)
    } catch (err: any) { error = err.message } finally { busy = false }
  }
</script>

<div class="bg-muted/40 grid min-h-screen place-items-center p-4">
  <Card.Root class="w-full max-w-sm">
    <Card.Header>
      <div class="bg-primary text-primary-foreground mb-2 grid size-10 place-items-center rounded-lg text-lg font-bold">B</div>
      <Card.Title class="text-xl">{info?.kind === 'invite' ? `Welcome, ${info.name}` : 'Choose a new password'}</Card.Title>
      <Card.Description>{info ? (info.kind === 'invite' ? `Choose a password for ${info.email} to finish setting up your account.` : `For ${info.email}.`) : linkError ? '' : 'Checking the link…'}</Card.Description>
    </Card.Header>
    <Card.Content>
      {#if linkError}
        <p class="text-destructive bg-destructive/10 rounded-md px-3 py-2 text-sm" role="alert">{linkError}</p>
        <Button variant="outline" class="mt-4 w-full" onclick={() => navigate('/')}>Go to sign in</Button>
      {:else if info}
        <form class="flex flex-col gap-4" onsubmit={submit}>
          {#if error}<p class="text-destructive bg-destructive/10 rounded-md px-3 py-2 text-sm" role="alert">{error}</p>{/if}
          <div class="flex flex-col gap-2">
            <Label for="np">New password</Label>
            <Input id="np" type="password" bind:value={password} required minlength={10} autocomplete="new-password" />
            <p class="text-muted-foreground text-xs">At least 10 characters.</p>
          </div>
          <div class="flex flex-col gap-2"><Label for="np2">Once more</Label><Input id="np2" type="password" bind:value={again} required autocomplete="new-password" /></div>
          <Button type="submit" disabled={busy} class="w-full">{#if busy}<Spinner />{/if}{info.kind === 'invite' ? 'Set password and sign in' : 'Save and sign in'}</Button>
        </form>
      {/if}
    </Card.Content>
  </Card.Root>
</div>
