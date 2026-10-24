<script lang="ts">
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import CableIcon from '@lucide/svelte/icons/cable'
  import { navigate } from '../router.svelte'
  import type { FlowStep } from '../api'
  import { getEditor } from './editor.svelte'
  import { stepTypes } from './steps'
  import IntegrationIcon from '../integrations/IntegrationIcon.svelte'
  import { integrations, manifestOf } from '../integrations/state.svelte'

  // AddStep is the line between two steps, with a button that inserts a
  // step there.
  let { list, index, tall = false }: { list: FlowStep[]; index: number; tall?: boolean } = $props()
  const ed = getEditor()
  const groups = ['Data', 'Logic', 'Actions'] as const
</script>

<div class={['relative flex flex-col items-center', tall ? 'h-14' : 'h-11']}>
  <div class="bg-border absolute inset-y-0 w-px"></div>
  <DropdownMenu.Root>
    <DropdownMenu.Trigger>
      {#snippet child({ props })}
        <button {...props} type="button" aria-label="Add a step here"
          class="bg-background text-muted-foreground hover:text-primary hover:border-primary data-[state=open]:border-primary data-[state=open]:text-primary relative z-10 my-auto grid size-6 place-items-center rounded-full border shadow-xs transition-colors">
          <PlusIcon class="size-3.5" />
        </button>
      {/snippet}
    </DropdownMenu.Trigger>
    <DropdownMenu.Content class="w-72" align="center">
      {#each groups as g, gi (g)}
        {#if gi > 0}<DropdownMenu.Separator />{/if}
        <DropdownMenu.Label class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">{g}</DropdownMenu.Label>
        {#each stepTypes.filter((t) => t.group === g) as t (t.type)}
          <DropdownMenu.Item onclick={() => ed.insert(list, index, t.type)} class="items-start gap-2.5 py-1.5">
            <span class={['mt-0.5 grid size-6 shrink-0 place-items-center rounded-md', t.tone]}><t.icon class="size-3.5" /></span>
            <span class="flex flex-col"><span class="text-sm font-medium">{t.label}</span><span class="text-muted-foreground text-xs">{t.description}</span></span>
          </DropdownMenu.Item>
        {/each}
      {/each}
      <DropdownMenu.Separator />
      <DropdownMenu.Label class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">Integrations</DropdownMenu.Label>
      {#each integrations.connections as c (c.id)}
        {@const m = manifestOf(c.kind)}
        {#if m}
          <DropdownMenu.Sub>
            <DropdownMenu.SubTrigger class="gap-2.5 py-1.5">
              <IntegrationIcon manifest={m} size="sm" />
              <span class="flex flex-col"><span class="text-sm font-medium">{c.name}</span><span class="text-muted-foreground text-xs">{m.actions.length} actions</span></span>
            </DropdownMenu.SubTrigger>
            <DropdownMenu.SubContent class="max-h-96 w-72 overflow-y-auto">
              {#each m.actions as a (a.key)}
                <DropdownMenu.Item onclick={() => ed.insertIntegration(list, index, c.id, a.key, a.label)} class="flex-col items-start gap-0 py-1.5">
                  <span class="text-sm font-medium">{a.label}</span><span class="text-muted-foreground text-xs">{a.description}</span>
                </DropdownMenu.Item>
              {/each}
            </DropdownMenu.SubContent>
          </DropdownMenu.Sub>
        {/if}
      {/each}
      <DropdownMenu.Item onclick={() => ed.app && navigate(`/apps/${ed.app.slug}/integrations`)} class="text-muted-foreground gap-2.5 py-1.5 text-xs">
        <CableIcon class="size-3.5" />{integrations.connections.length ? 'Connect another service' : 'Connect Stripe, Slack, GitHub and more'}
      </DropdownMenu.Item>
    </DropdownMenu.Content>
  </DropdownMenu.Root>
</div>
