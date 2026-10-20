<script lang="ts">
  // AppGate is a published app's sign-in page, in the app's own colours,
  // and the page people see when they are signed in without access.
  import { onDestroy, untrack } from 'svelte'
  import { setMode } from 'mode-watcher'
  import * as Card from '$lib/components/ui/card'
  import { Button } from '$lib/components/ui/button'
  import LockIcon from '@lucide/svelte/icons/lock'
  import { api, type PublicProvider, type User } from '../api'
  import { themeVars } from './theme'
  import type { Design } from './types'
  import SignInForm from '../users/SignInForm.svelte'

  let { slug, mode, user, message, onSignedIn, onSignOut }: {
    slug: string; mode: 'signin' | 'denied'; user?: { name: string; email: string } | null; message?: string
    onSignedIn: (u: User) => void; onSignOut: () => void
  } = $props()
  let brand = $state<{ name: string; design: Design; providers: PublicProvider[] } | null>(null)
  api.brand(slug).then((b) => (brand = b)).catch(() => (brand = { name: slug, design: { theme: 'system', accent: '', navigation: 'sidebar', title: '' }, providers: [] }))

  $effect(() => {
    if (!brand) return
    const theme = brand.design.theme
    untrack(() => setMode(theme === 'dark' ? 'dark' : theme === 'light' ? 'light' : 'system'))
    const vars = themeVars(brand.design)
    for (const [k, v] of Object.entries(vars)) document.documentElement.style.setProperty(k, v)
    document.title = `Sign in · ${brand.design.title || brand.name}`
    return () => { for (const k of Object.keys(vars)) document.documentElement.style.removeProperty(k) }
  })
  onDestroy(() => setMode('system'))
  const title = $derived(brand ? brand.design.title || brand.name : '')
</script>

<div class="bg-muted/40 relative grid min-h-screen place-items-center p-4">
  <div class="from-primary/15 pointer-events-none absolute inset-x-0 top-0 h-64 bg-gradient-to-b to-transparent"></div>
  {#if brand}
    <Card.Root class="relative w-full max-w-sm shadow-lg">
      <Card.Header class="items-center text-center">
        <div class="bg-primary text-primary-foreground mx-auto mb-2 grid size-12 place-items-center rounded-xl text-xl font-bold shadow-sm">{title.slice(0, 1).toUpperCase()}</div>
        {#if mode === 'signin'}
          <Card.Title class="text-xl">Sign in to {title}</Card.Title>
          <Card.Description>Use the account your team gave you.</Card.Description>
        {:else}
          <Card.Title class="flex items-center justify-center gap-2 text-xl"><LockIcon class="size-5" />No access yet</Card.Title>
          <Card.Description>{message || `You are signed in, but have no role in ${title}.`}</Card.Description>
        {/if}
      </Card.Header>
      <Card.Content>
        {#if mode === 'signin'}
          <SignInForm providers={brand.providers} {onSignedIn} />
        {:else}
          <div class="flex flex-col gap-3">
            {#if user}<div class="bg-muted/60 rounded-lg px-3 py-2 text-sm"><div class="font-medium">{user.name}</div><div class="text-muted-foreground text-xs">{user.email}</div></div>{/if}
            <p class="text-muted-foreground text-sm">Ask whoever runs {title} to give you a role, then reload this page.</p>
            <Button variant="outline" onclick={onSignOut}>Sign in as someone else</Button>
          </div>
        {/if}
      </Card.Content>
    </Card.Root>
  {/if}
  <p class="text-muted-foreground absolute bottom-4 text-xs">Made with Bubibase</p>
</div>
