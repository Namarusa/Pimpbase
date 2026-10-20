<script lang="ts">
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import type { FlowStep } from '../api'
  import { getEditor } from './editor.svelte'
  import { stepTypes } from './steps'

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
    </DropdownMenu.Content>
  </DropdownMenu.Root>
</div>
