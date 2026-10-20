<script lang="ts">
  import { Button } from '$lib/components/ui/button'
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link'
  import type { App } from '../lib/api'
  import { navigate } from '../lib/router.svelte'
  import { isBuilder } from '../lib/session.svelte'
  import SparklesIcon from '@lucide/svelte/icons/sparkles'
  import AssistantSheet from '../lib/ai/AssistantSheet.svelte'
  import { route, match } from '../lib/router.svelte'
  import type { DraftKind } from '../lib/api'

  let { app, active }: { app: App; active: 'data' | 'design' | 'apis' | 'flows' | 'access' | 'settings' } = $props()
  let assistant = $state(false)
  // The assistant starts on what this page edits, and offers to change
  // the screen, query or flow that is open.
  const kind = $derived<DraftKind>(({ design: 'screen', apis: 'query', flows: 'flow' } as Record<string, DraftKind>)[active] ?? 'table')
  const target = $derived.by(() => {
    const m = match('/apps/:app/:tab/:item', route.path)
    if (!m?.item) return undefined
    if (active === 'design') return { kind: 'screen' as DraftKind, id: m.item }
    if (active === 'flows') return { kind: 'flow' as DraftKind, id: m.item }
    if (active === 'apis' && m.item.startsWith('qry_')) return { kind: 'query' as DraftKind, id: m.item }
    return undefined
  })
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
  {#if isBuilder()}<Button variant="ghost" size="sm" class="ml-auto h-7" onclick={() => (assistant = true)}><SparklesIcon class="text-violet-500" />Assistant</Button>{/if}
  <Button variant="ghost" size="sm" class={['h-7', !isBuilder() && 'ml-auto']} href={`/app/${app.slug}`} target="_blank"><ExternalLinkIcon />Open app</Button>
</div>
{#if isBuilder()}<AssistantSheet bind:open={assistant} {app} {kind} {target} />{/if}
