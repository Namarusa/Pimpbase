<script lang="ts">
  import { Button } from '$lib/components/ui/button'
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link'
  import type { App } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import { isBuilder } from '../lib/session.svelte'

  let { app, active }: { app: App; active: 'data' | 'design' | 'apis' | 'flows' | 'access' | 'settings' } = $props()
  const go = (e: MouseEvent, path: string) => { e.preventDefault(); navigate(path) }
  const tabs = $derived([
    { key: 'data', label: 'Data', path: `/apps/${app.slug}/data` },
    ...(isBuilder() ? [{ key: 'design', label: 'Design', path: `/apps/${app.slug}/design` }, { key: 'apis', label: 'APIs', path: `/apps/${app.slug}/apis` }, { key: 'flows', label: 'Flows', path: `/apps/${app.slug}/flows` }, { key: 'access', label: 'Access', path: `/apps/${app.slug}/access` }, { key: 'settings', label: 'Settings', path: `/apps/${app.slug}/settings` }] : []),
  ])
</script>

<div class="bg-card flex h-11 shrink-0 items-center gap-5 border-b px-4">
  <strong class="text-sm">{app.name}</strong>
  <nav class="flex h-full items-stretch gap-1">
    {#each tabs as t (t.key)}
      <a href={t.path} onclick={(e) => go(e, t.path)}
        class={['flex items-center border-b-2 px-2.5 text-sm font-medium transition-colors', active === t.key ? 'border-primary text-foreground' : 'text-muted-foreground hover:text-foreground border-transparent']}>{t.label}</a>
    {/each}
  </nav>
  <Button variant="ghost" size="sm" class="ml-auto h-7" href={`/app/${app.slug}`} target="_blank"><ExternalLinkIcon />Open app</Button>
</div>
