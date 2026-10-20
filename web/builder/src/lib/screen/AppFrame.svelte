<script lang="ts">
  import type { Snippet } from 'svelte'
  import * as Sheet from '$lib/components/ui/sheet'
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import * as Avatar from '$lib/components/ui/avatar'
  import { Button } from '$lib/components/ui/button'
  import MenuIcon from '@lucide/svelte/icons/menu'
  import LogOutIcon from '@lucide/svelte/icons/log-out'
  import LogInIcon from '@lucide/svelte/icons/log-in'
  import type { Design, Screen } from './types'

  // AppFrame is the published app's navigation around its screens.
  let { design, appName, screens, current, user, onNavigate, onSignOut, onSignIn, children, compact = false }: {
    design: Design; appName: string; screens: Screen[]; current?: string
    user: { name: string; email: string } | null
    onNavigate: (route: string) => void; onSignOut?: () => void; onSignIn?: () => void
    children: Snippet; compact?: boolean
  } = $props()

  let menu = $state(false)
  const nav = $derived(screens.filter((s) => !s.route.includes(':')))
  const title = $derived(design.title || appName)
  const initials = $derived((user?.name || user?.email || '?').split(/\s+/).slice(0, 2).map((w) => w[0]?.toUpperCase()).join(''))
  const go = (e: MouseEvent, route: string) => { e.preventDefault(); menu = false; onNavigate(route) }
</script>

{#snippet links(vertical: boolean)}
  {#each nav as s (s.id)}
    <a href={s.route} onclick={(e) => go(e, s.route)}
      class={['rounded-md px-3 py-2 text-sm font-medium transition-colors', vertical ? 'block' : '',
        current === s.id ? (vertical ? 'bg-sidebar-accent text-sidebar-accent-foreground' : 'bg-accent text-accent-foreground') : 'text-muted-foreground hover:text-foreground hover:bg-accent/60']}>
      {s.name}
    </a>
  {/each}
{/snippet}

{#snippet account()}
  {#if user}
    <DropdownMenu.Root>
      <DropdownMenu.Trigger>
        {#snippet child({ props })}
          <button {...props} class="hover:bg-accent flex items-center gap-2 rounded-md p-1.5 text-left">
            <Avatar.Root class="size-8"><Avatar.Fallback class="bg-primary/10 text-primary text-xs font-medium">{initials}</Avatar.Fallback></Avatar.Root>
            {#if design.navigation === 'sidebar' && !compact}<span class="min-w-0 flex-1 truncate text-sm font-medium">{user.name}</span>{/if}
          </button>
        {/snippet}
      </DropdownMenu.Trigger>
      <DropdownMenu.Content align="end" class="w-56">
        <DropdownMenu.Label class="font-normal"><div class="text-sm font-medium">{user.name}</div><div class="text-muted-foreground text-xs">{user.email}</div></DropdownMenu.Label>
        <DropdownMenu.Separator />
        <DropdownMenu.Item onclick={onSignOut}><LogOutIcon />Sign out</DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu.Root>
  {:else if onSignIn}
    <Button variant="outline" size="sm" onclick={onSignIn}><LogInIcon />Sign in</Button>
  {/if}
{/snippet}

{#snippet brand()}
  <div class="flex items-center gap-2 font-semibold">
    <span class="bg-primary text-primary-foreground grid size-7 place-items-center rounded-md text-sm">{title.slice(0, 1).toUpperCase()}</span>
    <span class="truncate">{title}</span>
  </div>
{/snippet}

{#if design.navigation === 'sidebar' && !compact}
  <div class="bg-background text-foreground flex h-full min-h-0 print:block print:h-auto">
    <aside class="bg-sidebar text-sidebar-foreground hidden w-60 shrink-0 flex-col border-r md:flex print:hidden">
      <div class="flex h-14 items-center border-b px-4">{@render brand()}</div>
      <nav class="flex flex-1 flex-col gap-1 overflow-y-auto p-3">{@render links(true)}</nav>
      <div class="border-t p-3">{@render account()}</div>
    </aside>
    <div class="flex min-w-0 flex-1 flex-col print:block">
      <header class="flex h-14 items-center gap-2 border-b px-4 md:hidden print:hidden">
        <Sheet.Root bind:open={menu}>
          <Sheet.Trigger>{#snippet child({ props })}<Button {...props} variant="ghost" size="icon" aria-label="Menu"><MenuIcon /></Button>{/snippet}</Sheet.Trigger>
          <Sheet.Content side="left" class="w-64 p-0">
            <Sheet.Header class="border-b"><Sheet.Title>{title}</Sheet.Title></Sheet.Header>
            <nav class="flex flex-col gap-1 p-3">{@render links(true)}</nav>
          </Sheet.Content>
        </Sheet.Root>
        {@render brand()}
        <div class="ml-auto">{@render account()}</div>
      </header>
      <main class="min-h-0 flex-1 overflow-y-auto print:overflow-visible">{@render children()}</main>
    </div>
  </div>
{:else if design.navigation === 'none'}
  <div class="bg-background text-foreground h-full overflow-y-auto print:h-auto print:overflow-visible">{@render children()}</div>
{:else}
  <div class="bg-background text-foreground flex h-full min-h-0 flex-col print:block print:h-auto">
    <header class="flex h-14 shrink-0 items-center gap-4 border-b px-4 print:hidden">
      {#if nav.length > 1}
        <Sheet.Root bind:open={menu}>
          <Sheet.Trigger class={compact ? '' : 'md:hidden'}>{#snippet child({ props })}<Button {...props} variant="ghost" size="icon" class={compact ? '' : 'md:hidden'} aria-label="Menu"><MenuIcon /></Button>{/snippet}</Sheet.Trigger>
          <Sheet.Content side="left" class="w-64 p-0">
            <Sheet.Header class="border-b"><Sheet.Title>{title}</Sheet.Title></Sheet.Header>
            <nav class="flex flex-col gap-1 p-3">{@render links(true)}</nav>
          </Sheet.Content>
        </Sheet.Root>
      {/if}
      {@render brand()}
      {#if !compact}<nav class="hidden items-center gap-1 md:flex">{@render links(false)}</nav>{/if}
      <div class="ml-auto">{@render account()}</div>
    </header>
    <main class="min-h-0 flex-1 overflow-y-auto print:overflow-visible">{@render children()}</main>
  </div>
{/if}
