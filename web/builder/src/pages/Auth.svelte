<script lang="ts">
  import * as Card from '$lib/components/ui/card'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import { api, type PublicProvider } from '../lib/api'
  import { session } from '../lib/session.svelte'
  import SignInForm from '../lib/users/SignInForm.svelte'

  let { setup, onDone, title }: { setup: boolean; onDone: () => void; title?: string } = $props()
  let email = $state('')
  let name = $state('')
  let password = $state('')
  let error = $state('')
  let busy = $state(false)
  let providers = $state<PublicProvider[] | null>(null)
  if (!setup) api.providers().then((p) => (providers = p)).catch(() => (providers = []))

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    busy = true
    try {
      session.user = setup ? await api.createOwner(email, name, password) : await api.login(email, password)
      onDone()
    } catch (err: any) {
      error = err.message
    } finally {
      busy = false
    }
  }
</script>

<div class="bg-muted/40 grid min-h-screen place-items-center p-4">
  <Card.Root class="w-full max-w-sm">
    <Card.Header>
      <div class="bg-primary text-primary-foreground mb-2 grid size-10 place-items-center rounded-lg text-lg font-bold">B</div>
      <Card.Title class="text-xl">{setup ? 'Set up Pimpbase' : title ? `Sign in to ${title}` : 'Sign in'}</Card.Title>
      <Card.Description>{setup ? 'Create the owner account for this workspace.' : 'Welcome back.'}</Card.Description>
    </Card.Header>
    <Card.Content>
      {#if !setup}
        {#if providers}<SignInForm {providers} onSignedIn={(u) => { session.user = u; onDone() }} />{/if}
      {:else}
      <form class="flex flex-col gap-4" onsubmit={submit}>
        {#if error}<p class="text-destructive bg-destructive/10 rounded-md px-3 py-2 text-sm">{error}</p>{/if}
        {#if setup}
          <div class="flex flex-col gap-2"><Label for="name">Your name</Label><Input id="name" bind:value={name} autocomplete="name" /></div>
        {/if}
        <div class="flex flex-col gap-2"><Label for="email">Email</Label><Input id="email" type="email" bind:value={email} required autocomplete="email" /></div>
        <div class="flex flex-col gap-2">
          <Label for="pw">Password</Label>
          <Input id="pw" type="password" bind:value={password} required minlength={setup ? 10 : undefined}
            autocomplete={setup ? 'new-password' : 'current-password'} />
          {#if setup}<p class="text-muted-foreground text-xs">At least 10 characters.</p>{/if}
        </div>
        <Button type="submit" disabled={busy} class="w-full">{#if busy}<Spinner />{/if}{setup ? 'Create account' : 'Sign in'}</Button>
      </form>
      {/if}
    </Card.Content>
  </Card.Root>
</div>
