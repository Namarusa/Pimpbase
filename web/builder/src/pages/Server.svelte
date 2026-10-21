<script lang="ts">
  // Server shows how this Pimpbase runs: its version and storage, the
  // servers sharing its database, and a backup to download on SQLite.
  import * as Card from '$lib/components/ui/card'
  import * as Table from '$lib/components/ui/table'
  import { Badge } from '$lib/components/ui/badge'
  import { Button } from '$lib/components/ui/button'
  import { Skeleton } from '$lib/components/ui/skeleton'
  import DownloadIcon from '@lucide/svelte/icons/download'
  import { api, type ServerStatus } from '../lib/api'

  let status = $state<ServerStatus | null>(null)
  let error = $state('')

  async function load() {
    try { status = await api.serverStatus() } catch (e: any) { error = e.message }
  }
  load()
  const timer = setInterval(load, 10_000)
  $effect(() => () => clearInterval(timer))

  const storage: Record<string, string> = { database: 'Inside each app’s database', dir: 'A shared folder', s3: 'An S3 bucket' }
  const ago = (t: string) => {
    const s = Math.max(0, Math.round((Date.now() - new Date(t).getTime()) / 1000))
    return s < 60 ? `${s}s ago` : s < 3600 ? `${Math.round(s / 60)} min ago` : `${Math.round(s / 3600)} h ago`
  }
</script>

<div class="mx-auto grid max-w-4xl gap-4 px-4 py-8">
  <h1 class="text-2xl font-semibold">Server</h1>
  {#if error}<p class="text-destructive text-sm">{error}</p>{/if}
  {#if !status && !error}
    <Skeleton class="h-40 w-full" />
  {:else if status}
    <div class="grid gap-4 sm:grid-cols-3">
      <Card.Root class="gap-1 py-4"><Card.Header class="px-4"><Card.Description>Version</Card.Description><Card.Title class="text-xl">{status.version}</Card.Title></Card.Header></Card.Root>
      <Card.Root class="gap-1 py-4"><Card.Header class="px-4"><Card.Description>Database</Card.Description><Card.Title class="text-xl">{status.database === 'sqlite' ? 'SQLite' : 'Postgres'}</Card.Title></Card.Header></Card.Root>
      <Card.Root class="gap-1 py-4"><Card.Header class="px-4"><Card.Description>Uploaded files</Card.Description><Card.Title class="text-xl">{storage[status.storage] ?? status.storage}</Card.Title></Card.Header></Card.Root>
    </div>

    <Card.Root>
      <Card.Header>
        <Card.Title>Servers</Card.Title>
        <Card.Description>
          {#if status.instances.length > 1}
            {status.instances.length} servers share this database. Each scheduled flow runs once a minute across all of them.
          {:else}
            One server. To run more, point them at the same Postgres database and file storage.
          {/if}
        </Card.Description>
      </Card.Header>
      <Card.Content>
        <Table.Root>
          <Table.Header><Table.Row><Table.Head>Server</Table.Head><Table.Head>Host</Table.Head><Table.Head>Version</Table.Head><Table.Head>Started</Table.Head><Table.Head>Last seen</Table.Head></Table.Row></Table.Header>
          <Table.Body>
            {#each status.instances as i (i.id)}
              <Table.Row>
                <Table.Cell class="font-mono text-xs">{i.id}{#if i.self} <Badge variant="secondary" class="ml-1">this one</Badge>{/if}</Table.Cell>
                <Table.Cell>{i.host}</Table.Cell>
                <Table.Cell>{i.version}</Table.Cell>
                <Table.Cell>{new Date(i.startedAt).toLocaleString()}</Table.Cell>
                <Table.Cell>{ago(i.seenAt)}</Table.Cell>
              </Table.Row>
            {/each}
          </Table.Body>
        </Table.Root>
      </Card.Content>
    </Card.Root>

    <Card.Root>
      <Card.Header>
        <Card.Title>Limits and monitoring</Card.Title>
        <Card.Description>
          {status.rateLimit > 0 ? `Each client may make ${status.rateLimit} API requests a second to each server, in bursts of ${status.rateBurst}.` : 'The request rate limit is off.'}
          Health checks answer at <code>/healthz</code> and <code>/readyz</code>, and Prometheus metrics at <code>/metrics</code>. Per-app quotas are in each app’s settings.
        </Card.Description>
      </Card.Header>
    </Card.Root>

    <Card.Root>
      <Card.Header>
        <Card.Title>Backup</Card.Title>
        <Card.Description>
          {#if status.backups}
            A copy of every database, taken while the server runs. Restore it with <code>pimpbase restore</code>. Files kept in a folder or bucket are not included.
          {:else}
            This server keeps its data in Postgres; back it up with pg_dump or your provider’s snapshots.
          {/if}
        </Card.Description>
      </Card.Header>
      {#if status.backups}
        <Card.Content><Button href={api.backupURL()} variant="outline"><DownloadIcon />Download a backup</Button></Card.Content>
      {/if}
    </Card.Root>
  {/if}
</div>
