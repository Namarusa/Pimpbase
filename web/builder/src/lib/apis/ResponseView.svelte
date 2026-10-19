<script lang="ts">
  import * as Tabs from '$lib/components/ui/tabs'
  import * as Table from '$lib/components/ui/table'
  import * as Empty from '$lib/components/ui/empty'
  import { Badge } from '$lib/components/ui/badge'
  import { Spinner } from '$lib/components/ui/spinner'
  import SendIcon from '@lucide/svelte/icons/send'
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert'
  import { toast } from 'svelte-sonner'
  import { toText } from '../screen/bindings'
  import type { QueryResult } from '../api'
  import JsonTree from './JsonTree.svelte'

  // ResponseView shows the answer of a test run: rows, body, raw text,
  // headers and the request as sent.
  let { result, error, running, queryName, resultPath, onRows }: {
    result: QueryResult | null; error: string; running: boolean; queryName: string; resultPath: string
    onRows: (path: string) => void
  } = $props()
  let tab = $state('rows')
  const size = (n: number) => (n < 1024 ? `${n} B` : n < 1048576 ? `${(n / 1024).toFixed(1)} KB` : `${(n / 1048576).toFixed(1)} MB`)
  const cols = $derived(result?.fields.slice(0, 12) ?? [])
  const binding = (path: string[]) => `queries.${queryName}.body${path.length ? '.' + path.join('.') : ''}`
  async function pick(path: string[]) {
    const text = `{{ ${binding(path)} }}`
    try { await navigator.clipboard.writeText(text); toast.success(`Copied ${text}`) } catch { toast.info(text) }
  }
  $effect(() => { if (result && !result.rows.length && tab === 'rows') tab = 'body' })
</script>

<div class="flex h-full min-h-0 flex-col">
  <div class="flex h-10 shrink-0 items-center gap-3 border-b px-4">
    <span class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">Response</span>
    {#if running}
      <span class="text-muted-foreground flex items-center gap-1.5 text-xs"><Spinner class="size-3.5" />Sending…</span>
    {:else if result}
      <Badge variant="outline" class={result.ok ? 'border-emerald-500/40 bg-emerald-500/10 text-emerald-700 dark:text-emerald-400' : 'border-destructive/40 bg-destructive/10 text-destructive'}>{result.status} {result.statusText}</Badge>
      <span class="text-muted-foreground text-xs tabular-nums">{result.durationMs} ms · {size(result.size)}</span>
      <Badge variant="secondary" class="uppercase">{result.format}</Badge>
      {#if result.cached}<Badge variant="secondary">cached</Badge>{/if}
      <span class="text-muted-foreground ml-auto text-xs">{result.rows.length} {result.rows.length === 1 ? 'row' : 'rows'}{resultPath ? ` at ${resultPath}` : ''}</span>
    {/if}
  </div>
  {#if error}
    <div class="text-destructive flex items-start gap-2 p-4 text-sm"><CircleAlertIcon class="mt-0.5 size-4 shrink-0" /><span class="whitespace-pre-wrap">{error}</span></div>
  {:else if result}
    {#if result.parseError}<p class="text-destructive border-b px-4 py-2 text-xs">{result.parseError}</p>{/if}
    <Tabs.Root bind:value={tab} class="flex min-h-0 flex-1 flex-col gap-0">
      <div class="border-b px-4 py-1.5">
        <Tabs.List class="h-8">
          <Tabs.Trigger value="rows" class="text-xs">Rows</Tabs.Trigger>
          <Tabs.Trigger value="body" class="text-xs">Body</Tabs.Trigger>
          <Tabs.Trigger value="raw" class="text-xs">Raw</Tabs.Trigger>
          <Tabs.Trigger value="headers" class="text-xs">Headers</Tabs.Trigger>
          {#if result.request}<Tabs.Trigger value="request" class="text-xs">Request</Tabs.Trigger>{/if}
        </Tabs.List>
      </div>
      <Tabs.Content value="rows" class="min-h-0 flex-1 overflow-auto">
        {#if result.rows.length}
          <Table.Root class="text-xs">
            <Table.Header class="bg-muted/50 sticky top-0"><Table.Row>
              <Table.Head class="w-10 text-right">#</Table.Head>
              {#each cols as c (c.name)}<Table.Head>{c.name}<span class="text-muted-foreground ml-1.5 font-normal">{c.type}</span></Table.Head>{/each}
            </Table.Row></Table.Header>
            <Table.Body>
              {#each result.rows.slice(0, 200) as r, i (i)}
                <Table.Row>
                  <Table.Cell class="text-muted-foreground text-right tabular-nums">{i + 1}</Table.Cell>
                  {#each cols as c (c.name)}<Table.Cell class="max-w-64 truncate">{toText(r[c.name])}</Table.Cell>{/each}
                </Table.Row>
              {/each}
            </Table.Body>
          </Table.Root>
        {:else}
          <p class="text-muted-foreground p-4 text-sm">No rows. Pick a list in the Body tab to use as rows.</p>
        {/if}
      </Tabs.Content>
      <Tabs.Content value="body" class="min-h-0 flex-1 overflow-auto px-3 py-2">
        {#if result.format === 'text'}
          <pre class="font-mono text-xs whitespace-pre-wrap">{result.body ?? ''}</pre>
        {:else}
          <JsonTree value={result.body} onPick={pick} onRows={(p) => onRows(p.join('.'))} rowsPath={resultPath} />
          <p class="text-muted-foreground mt-3 text-xs">Click a name to copy its binding. Lists can be used as the query’s rows.</p>
        {/if}
      </Tabs.Content>
      <Tabs.Content value="raw" class="min-h-0 flex-1 overflow-auto">
        <pre class="p-4 font-mono text-xs break-all whitespace-pre-wrap">{result.raw}</pre>
      </Tabs.Content>
      <Tabs.Content value="headers" class="min-h-0 flex-1 overflow-auto">
        <Table.Root class="text-xs"><Table.Body>
          {#each Object.entries(result.headers).sort() as [k, v] (k)}
            <Table.Row><Table.Cell class="w-56 font-mono font-medium">{k}</Table.Cell><Table.Cell class="font-mono break-all whitespace-normal">{v}</Table.Cell></Table.Row>
          {/each}
        </Table.Body></Table.Root>
      </Tabs.Content>
      {#if result.request}
        <Tabs.Content value="request" class="min-h-0 flex-1 overflow-auto p-4">
          <div class="font-mono text-xs"><span class="font-semibold">{result.request.method}</span> <span class="break-all">{result.request.url}</span></div>
          <div class="mt-3 flex flex-col gap-0.5 font-mono text-xs">
            {#each result.request.headers as h (h.key)}<div><span class="text-muted-foreground">{h.key}:</span> <span class="break-all">{h.value}</span></div>{/each}
          </div>
          {#if result.request.body}<pre class="bg-muted/50 mt-3 rounded-md p-3 font-mono text-xs break-all whitespace-pre-wrap">{result.request.body}</pre>{/if}
          <p class="text-muted-foreground mt-3 text-xs">Secret variables and sign-in values are hidden.</p>
        </Tabs.Content>
      {/if}
    </Tabs.Root>
  {:else}
    <Empty.Root class="flex-1 border-0">
      <Empty.Header>
        <Empty.Media variant="icon"><SendIcon /></Empty.Media>
        <Empty.Title>Send the query to see its answer</Empty.Title>
        <Empty.Description>Test runs use the parameters’ test values and the development values of variables.</Empty.Description>
      </Empty.Header>
    </Empty.Root>
  {/if}
</div>
