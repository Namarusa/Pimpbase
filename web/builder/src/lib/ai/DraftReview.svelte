<script lang="ts">
  // DraftReview shows a draft before it is applied: what it does, a
  // preview, and the exact changes as a diff.
  import * as Tabs from '$lib/components/ui/tabs'
  import * as Alert from '$lib/components/ui/alert'
  import { Badge } from '$lib/components/ui/badge'
  import InfoIcon from '@lucide/svelte/icons/info'
  import type { AIDraft, Table } from '../api'
  import { registry } from '../screen/registry'
  import { stepInfo, triggerInfo } from '../flows/steps'
  import { comparable, tableText } from './apply'
  import { diffLines, fold } from './diff'
  import { fillDefaults } from './catalog'

  let { draft, tables }: { draft: AIDraft; tables: Table[] } = $props()
  let view = $state('preview')

  const texts = $derived(draft.kind === 'table' ? tableText(tables, draft) : comparable(draft))
  const lines = $derived(fold(diffLines(texts.before, texts.after)))
  const added = $derived(lines.filter((l) => l.kind === 'add').length)
  const removed = $derived(lines.filter((l) => l.kind === 'del').length)
  const tableName = (id?: string) => (id?.startsWith('query:') ? 'query ' + id.slice(6) : tables.find((t) => t.id === id)?.name ?? id)

  // Outline rows for screens and flows: depth, label, detail.
  type Row = { depth: number; label: string; detail?: string }
  function screenRows(n: any, depth = 0, out: Row[] = []): Row[] {
    const p = n.props ?? {}
    const detail = p.text ?? p.title ?? (p.table ? tableName(p.table) : undefined) ?? p.field ?? p.label
    out.push({ depth, label: registry[n.type]?.label ?? n.type, detail: detail ? String(detail) : undefined })
    for (const c of n.children ?? []) screenRows(c, depth + 1, out)
    return out
  }
  function stepRows(list: any[], depth = 0, out: Row[] = []): Row[] {
    for (const s of list ?? []) {
      out.push({ depth, label: s.name, detail: stepInfo(s.type)?.label ?? s.type })
      if (s.then?.length) { out.push({ depth: depth + 1, label: 'Then' }); stepRows(s.then, depth + 2, out) }
      if (s.else?.length) { out.push({ depth: depth + 1, label: 'Otherwise' }); stepRows(s.else, depth + 2, out) }
      if (s.do?.length) stepRows(s.do, depth + 1, out)
    }
    return out
  }
  const outline = $derived<Row[]>(
    draft.kind === 'screen' ? screenRows(fillDefaults(draft.draft.definition))
      : draft.kind === 'flow' ? [{ depth: 0, label: 'When', detail: triggerInfo(draft.draft.trigger?.type).label + (draft.draft.trigger?.table ? ` · ${tableName(draft.draft.trigger.table)}` : draft.draft.trigger?.cron ? ` · ${draft.draft.trigger.cron}` : '') }, ...stepRows(draft.draft.steps)]
        : [],
  )
</script>

<div class="flex flex-col gap-3">
  <div>
    <div class="text-sm font-medium">{draft.summary}</div>
    {#if draft.changes.length}
      <ul class="text-muted-foreground mt-1.5 list-disc space-y-0.5 pl-5 text-sm">{#each draft.changes as c, i (i)}<li>{c}</li>{/each}</ul>
    {/if}
  </div>
  {#if draft.notes}
    <Alert.Root><InfoIcon /><Alert.Title>Before you apply</Alert.Title><Alert.Description>{draft.notes}</Alert.Description></Alert.Root>
  {/if}
  <Tabs.Root bind:value={view}>
    <Tabs.List class="w-full">
      <Tabs.Trigger value="preview">Preview</Tabs.Trigger>
      <Tabs.Trigger value="diff">Changes <Badge variant="secondary" class="ml-1 h-5 px-1.5 font-mono text-[11px] text-emerald-700 dark:text-emerald-400">+{added}</Badge>{#if removed}<Badge variant="secondary" class="h-5 px-1.5 font-mono text-[11px] text-red-700 dark:text-red-400">−{removed}</Badge>{/if}</Tabs.Trigger>
    </Tabs.List>
    <Tabs.Content value="preview" class="mt-2">
      {#if draft.kind === 'table'}
        <div class="flex flex-col gap-2">
          {#each draft.draft.tables as t (t.name)}
            <div class="rounded-lg border">
              <div class="flex items-center gap-2 border-b px-3 py-2 text-sm font-medium">{t.name}<Badge variant={t.existing ? 'outline' : 'default'} class="h-5 text-[11px]">{t.existing ? 'new fields' : 'new table'}</Badge></div>
              <div class="divide-y">
                {#each t.fields as f (f.name)}
                  <div class="flex items-baseline gap-2 px-3 py-1.5 text-sm">
                    <span class="font-medium">{f.name}</span>
                    <span class="text-muted-foreground text-xs">{f.type}{f.type === 'link' ? ` → ${String(f.linkTable).startsWith('draft:') ? draft.draft.tables.find((x: any) => 'draft:' + x.name.toLowerCase() === f.linkTable)?.name : tableName(f.linkTable)}${f.linkMany ? ' (many)' : ''}` : ''}{f.required ? ' · required' : ''}</span>
                    {#if f.options?.length}<span class="text-muted-foreground ml-auto truncate text-xs">{f.options.join(' · ')}</span>{/if}
                    {#if f.formula}<code class="text-muted-foreground ml-auto truncate font-mono text-xs">{f.formula}</code>{/if}
                  </div>
                {/each}
              </div>
            </div>
          {/each}
        </div>
      {:else if draft.kind === 'query'}
        <div class="rounded-lg border p-3 text-sm">
          <div class="font-medium">{draft.draft.name}</div>
          <div class="mt-1 font-mono text-xs"><Badge variant="outline" class="mr-1.5 font-mono">{draft.draft.method ?? 'GET'}</Badge>{draft.draft.path}</div>
          {#if draft.draft.parameters?.length}<div class="text-muted-foreground mt-2 text-xs">Parameters: {draft.draft.parameters.map((p: any) => p.name).join(', ')}</div>{/if}
          {#if draft.draft.body}<pre class="bg-muted mt-2 max-h-48 overflow-auto rounded p-2 font-mono text-xs">{draft.draft.body}</pre>{/if}
        </div>
      {:else}
        <div class="rounded-lg border py-1.5">
          {#if draft.kind === 'screen'}<div class="text-muted-foreground px-3 pb-1 text-xs">{draft.draft.name} · <span class="font-mono">{draft.draft.route}</span> · {draft.draft.role}</div>{/if}
          {#each outline as r, i (i)}
            <div class="flex items-baseline gap-2 py-0.5 pr-3 text-sm" style:padding-left={`${12 + r.depth * 16}px`}>
              <span class={r.detail === undefined && draft.kind === 'flow' ? 'text-muted-foreground text-xs uppercase' : 'font-medium'}>{r.label}</span>
              {#if r.detail}<span class="text-muted-foreground truncate text-xs">{r.detail}</span>{/if}
            </div>
          {/each}
        </div>
      {/if}
    </Tabs.Content>
    <Tabs.Content value="diff" class="mt-2">
      <div class="bg-muted/40 max-h-[50vh] overflow-auto rounded-lg border font-mono text-[11.5px] leading-5">
        {#each lines as l, i (i)}
          {#if l.kind === 'fold'}
            <div class="text-muted-foreground bg-muted px-3 text-[11px]">⋯ {l.count} unchanged lines</div>
          {:else}
            <div class={['px-3 whitespace-pre', l.kind === 'add' && 'bg-emerald-500/12 text-emerald-800 dark:text-emerald-300', l.kind === 'del' && 'bg-red-500/12 text-red-800 dark:text-red-300']}><span class="inline-block w-3 select-none opacity-60">{l.kind === 'add' ? '+' : l.kind === 'del' ? '−' : ' '}</span>{l.text}</div>
          {/if}
        {/each}
      </div>
    </Tabs.Content>
  </Tabs.Root>
</div>
