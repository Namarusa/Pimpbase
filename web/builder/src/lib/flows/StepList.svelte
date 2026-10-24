<script lang="ts">
  import * as DropdownMenu from '$lib/components/ui/dropdown-menu'
  import EllipsisIcon from '@lucide/svelte/icons/ellipsis'
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check'
  import CircleXIcon from '@lucide/svelte/icons/circle-x'
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert'
  import type { FlowStep } from '../api'
  import AddStep from './AddStep.svelte'
  import Self from './StepList.svelte'
  import { getEditor } from './editor.svelte'
  import { stepInfo, summary } from './steps'

  // StepList draws a list of steps top to bottom, with the branches of
  // conditions side by side and the body of loops in a frame.
  let { steps, nested = false }: { steps: FlowStep[]; nested?: boolean } = $props()
  const ed = getEditor()
</script>

<div class="flex flex-col items-center">
  {#each steps as s, i (s.id)}
    {@const info = stepInfo(s.type)}
    {@const run = ed.lastRun[s.id]}
    <AddStep list={steps} index={i} />
    <div class={['group relative w-76', s.disabled ? 'opacity-55' : '']}>
      <button type="button" data-step={s.id} onclick={() => (ed.selected = s.id)}
        class={['bg-card hover:border-primary/50 flex w-full items-start gap-3 rounded-xl border p-3 text-left shadow-xs transition-all',
          ed.selected === s.id ? 'border-primary ring-primary/20 ring-[3px]' : '',
          ed.errorStep === s.id ? 'border-destructive ring-destructive/20 ring-[3px]' : '']}>
        {#if info}<span class={['grid size-9 shrink-0 place-items-center rounded-lg', info.tone]}><info.icon class="size-4.5" /></span>{/if}
        <span class="flex min-w-0 flex-1 flex-col gap-0.5 pr-5">
          <span class="flex items-center gap-1.5">
            <span class="text-sm font-semibold">{info?.label ?? s.type}</span>
            {#if run === 'ok'}<CircleCheckIcon class="size-3.5 text-emerald-600 dark:text-emerald-400" />{:else if run === 'failed'}<CircleXIcon class="text-destructive size-3.5" />{/if}
            {#if ed.errorStep === s.id}<CircleAlertIcon class="text-destructive size-3.5" />{/if}
          </span>
          <span class="text-muted-foreground truncate font-mono text-[11px]">{s.name}{s.disabled ? ' · off' : ''}{s.continueOnError ? ' · goes on if it fails' : ''}</span>
          <span class="text-foreground/80 mt-0.5 line-clamp-2 text-xs [overflow-wrap:anywhere]">{summary(s, ed.tables, ed.queries, (id) => ed.emailTemplates.find((t) => t.id === id)?.name)}</span>
        </span>
      </button>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger>
          {#snippet child({ props })}
            <button {...props} type="button" aria-label={`Step ${s.name} actions`} class="text-muted-foreground hover:bg-muted absolute top-2 right-2 grid size-6 place-items-center rounded-md opacity-0 group-hover:opacity-100 focus-visible:opacity-100 data-[state=open]:opacity-100"><EllipsisIcon class="size-4" /></button>
          {/snippet}
        </DropdownMenu.Trigger>
        <DropdownMenu.Content align="end" class="w-44">
          <DropdownMenu.Item onclick={() => ed.duplicate(s.id)}>Duplicate</DropdownMenu.Item>
          <DropdownMenu.Item disabled={i === 0} onclick={() => ed.move(s.id, -1)}>Move up</DropdownMenu.Item>
          <DropdownMenu.Item disabled={i === steps.length - 1} onclick={() => ed.move(s.id, 1)}>Move down</DropdownMenu.Item>
          <DropdownMenu.Item onclick={() => { s.disabled = !s.disabled || undefined; ed.changed() }}>{s.disabled ? 'Switch on' : 'Switch off'}</DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item variant="destructive" onclick={() => ed.remove(s.id)}>Delete</DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu.Root>
    </div>

    {#if s.type === 'condition'}
      <div class="bg-border h-5 w-px"></div>
      <div class="flex items-stretch">
        {#each [{ key: 'then', label: 'Yes' }, { key: 'else', label: 'No' }] as b, bi (b.key)}
          {@const list = s[b.key as 'then' | 'else']!}
          <div class="relative flex min-w-80 flex-col items-center px-3">
            <!-- The bar joining the branches. -->
            <div class={['bg-border absolute top-0 h-px', bi === 0 ? 'right-0 left-1/2' : 'right-1/2 left-0']}></div>
            <div class="bg-border h-4 w-px"></div>
            <span class={['rounded-full border px-2.5 py-0.5 text-[11px] font-semibold', bi === 0 ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-300' : 'border-rose-500/40 bg-rose-500/10 text-rose-700 dark:text-rose-300']}>{b.label}</span>
            <Self steps={list} nested />
            <div class="bg-border w-px flex-1 min-h-4"></div>
            <div class={['bg-border absolute bottom-0 h-px', bi === 0 ? 'right-0 left-1/2' : 'right-1/2 left-0']}></div>
          </div>
        {/each}
      </div>
    {:else if s.type === 'loop'}
      <div class="bg-border h-4 w-px"></div>
      <div class="rounded-2xl border-2 border-dashed border-amber-500/35 bg-amber-500/[0.03] px-5 pt-3 pb-1">
        <div class="text-center text-[11px] font-semibold tracking-wide text-amber-700 uppercase dark:text-amber-300">For each item</div>
        <Self steps={s.do!} nested />
      </div>
    {/if}
  {/each}
  <AddStep list={steps} index={steps.length} tall={!nested} />
  {#if !nested}
    <span class="text-muted-foreground bg-muted rounded-full px-3 py-1 text-xs font-medium">End</span>
  {:else if steps.length === 0}
    <span class="text-muted-foreground pb-2 text-xs">No steps</span>
  {/if}
</div>
