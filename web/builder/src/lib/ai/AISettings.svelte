<script lang="ts">
  // AISettings sets up the assistant: the Anthropic API key, kept on the
  // server, and the model.
  import * as Card from '$lib/components/ui/card'
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Badge } from '$lib/components/ui/badge'
  import SparklesIcon from '@lucide/svelte/icons/sparkles'
  import ShieldCheckIcon from '@lucide/svelte/icons/shield-check'
  import { toast } from 'svelte-sonner'
  import { ai, type AIStatus } from '../api'

  let status = $state<AIStatus | null>(null)
  let key = $state('')
  let saving = $state(false)
  const modelNames: Record<string, string> = {
    'claude-opus-5-5': 'Claude Opus 5.5 (recommended)', 'claude-sonnet-5-5': 'Claude Sonnet 5.5 (faster)',
    'claude-haiku-5-5': 'Claude Haiku 5.5 (fastest, lowest cost)', 'claude-fable-5-1': 'Claude Fable 5.1 (most capable)',
  }

  async function load() { try { status = await ai.status() } catch (e: any) { toast.error(e.message) } }
  load()

  async function save(p: { apiKey?: string; model?: string }, done: string) {
    saving = true
    try { status = await ai.update(p); key = ''; toast.success(done) } catch (e: any) { toast.error(e.message) } finally { saving = false }
  }
</script>

{#if status}
  <div class="grid gap-4 lg:grid-cols-[1fr_20rem]">
    <Card.Root>
      <Card.Header>
        <Card.Title class="flex items-center gap-2"><SparklesIcon class="size-4 text-violet-500" />AI assistant
          {#if status.configured}<Badge variant="secondary" class="text-emerald-700 dark:text-emerald-400">On</Badge>{:else}<Badge variant="outline">Off: no API key</Badge>{/if}
        </Card.Title>
        <Card.Description>Builders describe tables, screens, API queries and flows and get drafts to review; they get formula help and answers about their data.</Card.Description>
      </Card.Header>
      <Card.Content class="flex flex-col gap-5">
        <div class="flex flex-col gap-1.5">
          <Label for="ai-key">Anthropic API key</Label>
          {#if status.source === 'server'}
            <p class="text-sm">The server was started with a key (<code class="font-mono text-xs">{status.keyHint}</code>), which is used. Change it where the server is configured.</p>
          {:else}
            {#if status.configured}<p class="text-sm">A key is saved: <code class="font-mono text-xs">{status.keyHint}</code></p>{/if}
            <div class="flex gap-2">
              <Input id="ai-key" type="password" autocomplete="off" bind:value={key} placeholder={status.configured ? 'Paste a new key to replace it' : 'sk-ant-…'} />
              <Button disabled={saving || !key.trim()} onclick={() => save({ apiKey: key }, 'Key saved')}>Save</Button>
            </div>
            <p class="text-muted-foreground text-xs">Create one at console.anthropic.com. Usage is billed to that key's account.</p>
            {#if status.configured}<Button variant="outline" size="sm" class="self-start" disabled={saving} onclick={() => save({ apiKey: '' }, 'Key removed; the assistant is off')}>Remove the key</Button>{/if}
          {/if}
        </div>
        <div class="flex flex-col gap-1.5">
          <Label>Model</Label>
          <Select.Root type="single" value={status.model} onValueChange={(v) => save({ model: v }, 'Model changed')}>
            <Select.Trigger class="w-full sm:w-80">{modelNames[status.model] ?? status.model}</Select.Trigger>
            <Select.Content>{#each status.models as m (m)}<Select.Item value={m} label={modelNames[m] ?? m}>{modelNames[m] ?? m}</Select.Item>{/each}</Select.Content>
          </Select.Root>
        </div>
      </Card.Content>
    </Card.Root>
    <Card.Root class="bg-muted/30">
      <Card.Header><Card.Title class="flex items-center gap-2 text-sm"><ShieldCheckIcon class="size-4" />What stays private</Card.Title></Card.Header>
      <Card.Content class="text-muted-foreground flex flex-col gap-2 text-sm">
        <p>The key is kept on the server. Browsers and published apps never receive it, and only owners and admins can change it.</p>
        <p>Drafts send the app's structure: table and field names, screens, queries, flows and roles. Secret variable values and row data are not sent.</p>
        <p>Answers about data read rows through the same permissions and row rules as the person asked about, and send only what they read.</p>
        <p>Nothing is applied until a builder reviews the draft and applies it.</p>
      </Card.Content>
    </Card.Root>
  </div>
{/if}
