<script lang="ts">
  // AISettings sets up the assistant: which provider it uses, and each
  // provider's key, address and model. Keys stay on the server; the page
  // only ever sees a hint.
  import * as Card from '$lib/components/ui/card'
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Badge } from '$lib/components/ui/badge'
  import { Spinner } from '$lib/components/ui/spinner'
  import SparklesIcon from '@lucide/svelte/icons/sparkles'
  import ShieldCheckIcon from '@lucide/svelte/icons/shield-check'
  import ServerIcon from '@lucide/svelte/icons/server'
  import PlugZapIcon from '@lucide/svelte/icons/plug-zap'
  import CheckIcon from '@lucide/svelte/icons/check'
  import { toast } from 'svelte-sonner'
  import { ai, type AIStatus, type AIProviderStatus, type AIUpdate, type AICheck } from '../api'

  let status = $state<AIStatus | null>(null)
  let selected = $state('')
  let key = $state('')
  let baseUrl = $state('')
  let model = $state('')
  let apiVersion = $state('')
  let saving = $state(false)
  let testing = $state(false)
  let check = $state<{ ok: true; c: AICheck } | { ok: false; error: string } | null>(null)
  const modelNames: Record<string, string> = {
    'claude-opus-5-5': 'Claude Opus 5.5 (recommended)', 'claude-sonnet-5-5': 'Claude Sonnet 5.5 (faster)',
    'claude-haiku-5-5': 'Claude Haiku 5.5 (fastest, lowest cost)', 'claude-fable-5-1': 'Claude Fable 5.1 (most capable)',
  }

  const p = $derived(status?.providers.find((x) => x.id === selected) ?? null)
  const inUse = $derived(status?.provider === selected)

  function fill(x: AIProviderStatus | null) {
    key = ''
    baseUrl = x && x.baseUrlSource === 'workspace' ? x.baseUrl : ''
    model = x && x.modelSource === 'workspace' ? x.model : ''
    apiVersion = x && x.apiVersionSource === 'workspace' ? x.apiVersion : ''
  }
  function choose(id: string) { selected = id; check = null; fill(status?.providers.find((x) => x.id === id) ?? null) }

  async function load() {
    try { status = await ai.status(); choose(status.provider) } catch (e: any) { toast.error(e.message) }
  }
  load()

  async function save(u: AIUpdate, done: string) {
    saving = true
    try {
      status = await ai.update({ provider: selected, ...u })
      fill(p); check = null
      toast.success(done)
    } catch (e: any) { toast.error(e.message) } finally { saving = false }
  }

  // Only the fields that changed and are not set on the server are sent.
  const changes = $derived.by(() => {
    if (!p) return null
    const u: AIUpdate = {}
    const was = (src: string, v: string) => (src === 'workspace' ? v : '')
    if (p.baseUrlSource !== 'server' && baseUrl.trim() !== was(p.baseUrlSource, p.baseUrl)) u.baseUrl = baseUrl.trim()
    if (p.modelSource !== 'server' && !p.fixedModels && model.trim() !== was(p.modelSource, p.model)) u.model = model.trim()
    if (p.hasApiVersion && p.apiVersionSource !== 'server' && apiVersion.trim() !== was(p.apiVersionSource, p.apiVersion)) u.apiVersion = apiVersion.trim()
    return Object.keys(u).length ? u : null
  })

  async function test() {
    testing = true; check = null
    try { check = { ok: true, c: await ai.test(selected) } } catch (e: any) { check = { ok: false, error: e.message } } finally { testing = false }
  }

  const fromServer = 'Set where the server is started.'
  const secs = (ms: number) => (ms < 1000 ? `${ms} ms` : `${(ms / 1000).toFixed(1)} s`)
</script>

{#if status}
  <div class="grid gap-4 lg:grid-cols-[1fr_20rem]">
    <Card.Root>
      <Card.Header>
        <Card.Title class="flex items-center gap-2"><SparklesIcon class="size-4 text-violet-500" />AI assistant
          {#if status.configured}<Badge variant="secondary" class="text-emerald-700 dark:text-emerald-400">On</Badge>{:else}<Badge variant="outline">Off: not set up</Badge>{/if}
        </Card.Title>
        <Card.Description>Builders describe tables, screens, API queries and flows and get drafts to review; they get formula help and answers about their data. Pick the AI provider they use.</Card.Description>
      </Card.Header>
      <Card.Content class="flex flex-col gap-5">
        <div class="flex flex-col gap-2">
          <Label>Provider</Label>
          {#if status.providerSource === 'server'}
            <p class="text-muted-foreground flex items-center gap-1.5 text-xs"><ServerIcon class="size-3.5" />The server was started with a provider, which is used. Change it where the server is configured.</p>
          {/if}
          <div class="grid grid-cols-2 gap-2 sm:grid-cols-3" role="radiogroup" aria-label="Provider">
            {#each status.providers as x (x.id)}
              <button type="button" role="radio" aria-checked={selected === x.id} data-provider={x.id}
                class="hover:bg-muted/60 flex flex-col items-start gap-1 rounded-lg border p-3 text-left transition-colors {selected === x.id ? 'border-primary ring-primary/30 bg-muted/40 ring-2' : ''}"
                onclick={() => choose(x.id)}>
                <span class="flex w-full items-center gap-1.5 text-sm font-medium">{x.name}
                  {#if status.provider === x.id}<Badge class="ml-auto h-5 px-1.5 text-[10px]">In use</Badge>{/if}
                </span>
                <span class="text-muted-foreground flex items-center gap-1 text-xs">
                  {#if x.configured}<span class="size-1.5 rounded-full bg-emerald-500"></span>Ready{:else}<span class="bg-muted-foreground/40 size-1.5 rounded-full"></span>Needs {x.missing}{/if}
                </span>
              </button>
            {/each}
          </div>
        </div>

        {#if p}
          <div class="flex flex-col gap-4 rounded-lg border p-4" data-testid="provider-settings">
            <div class="flex flex-wrap items-center gap-2">
              <div class="text-sm font-medium">{p.name}</div>
              {#if !inUse}
                <Button size="sm" class="ml-auto" disabled={saving || status.providerSource === 'server'} onclick={() => save({ use: true }, `The assistant now uses ${p.name}`)}>Use {p.name}</Button>
              {/if}
            </div>

            <div class="flex flex-col gap-1.5">
              <Label for="ai-key">API key{#if !p.needsKey}<span class="text-muted-foreground font-normal"> (optional)</span>{/if}</Label>
              {#if p.keySource === 'server'}
                <p class="text-sm">The server was started with a key (<code class="font-mono text-xs">{p.keyHint}</code>), which is used. Change it where the server is configured.</p>
              {:else}
                {#if p.keySource === 'workspace'}<p class="text-sm">A key is saved: <code class="font-mono text-xs">{p.keyHint}</code></p>{/if}
                <div class="flex gap-2">
                  <Input id="ai-key" type="password" autocomplete="off" bind:value={key} placeholder={p.keySource ? 'Paste a new key to replace it' : (p.keyPrefix ? p.keyPrefix + '…' : 'Paste the key')} />
                  <Button disabled={saving || !key.trim()} onclick={() => save({ apiKey: key }, 'Key saved')}>Save</Button>
                </div>
                <p class="text-muted-foreground text-xs">{p.keyHelp}{#if p.needsKey} Usage is billed to that key's account.{/if}</p>
                {#if p.keySource === 'workspace'}<Button variant="outline" size="sm" class="self-start" disabled={saving} onclick={() => save({ apiKey: '' }, 'Key removed')}>Remove the key</Button>{/if}
              {/if}
            </div>

            <div class="grid gap-4 sm:grid-cols-2">
              <div class="flex flex-col gap-1.5 sm:col-span-2">
                <Label for="ai-url">{p.id === 'azure' ? 'Resource endpoint' : 'Address'}{#if !p.needsBaseUrl}<span class="text-muted-foreground font-normal"> (optional)</span>{/if}</Label>
                {#if p.baseUrlSource === 'server'}
                  <p class="text-sm"><code class="font-mono text-xs">{p.baseUrl}</code> <span class="text-muted-foreground text-xs">{fromServer}</span></p>
                {:else}
                  <Input id="ai-url" bind:value={baseUrl} autocomplete="off" placeholder={p.defaultBaseUrl || (p.id === 'azure' ? 'https://my-resource.openai.azure.com' : 'http://localhost:11434/v1')} />
                  {#if !p.needsBaseUrl}<p class="text-muted-foreground text-xs">Leave empty for {p.defaultBaseUrl}; set it to go through a proxy or gateway.</p>{/if}
                {/if}
              </div>

              <div class="flex flex-col gap-1.5">
                <Label for="ai-model">{p.modelLabel}</Label>
                {#if p.modelSource === 'server'}
                  <p class="text-sm"><code class="font-mono text-xs">{p.model}</code> <span class="text-muted-foreground text-xs">{fromServer}</span></p>
                {:else if p.fixedModels}
                  <Select.Root type="single" value={p.model} onValueChange={(v) => save({ model: v }, 'Model changed')}>
                    <Select.Trigger id="ai-model" class="w-full">{modelNames[p.model] ?? p.model}</Select.Trigger>
                    <Select.Content>{#each p.models as m (m)}<Select.Item value={m} label={modelNames[m] ?? m}>{modelNames[m] ?? m}</Select.Item>{/each}</Select.Content>
                  </Select.Root>
                {:else}
                  <Input id="ai-model" bind:value={model} autocomplete="off" list="ai-models" class="font-mono"
                    placeholder={p.defaultModel || (p.id === 'azure' ? 'the deployment name' : 'e.g. llama3.3, qwen3:8b')} />
                  <datalist id="ai-models">{#each p.models as m (m)}<option value={m}></option>{/each}</datalist>
                  {#if p.defaultModel}<p class="text-muted-foreground text-xs">Leave empty for {p.defaultModel}, or type any model the account offers.</p>{/if}
                {/if}
              </div>

              {#if p.hasApiVersion}
                <div class="flex flex-col gap-1.5">
                  <Label for="ai-version">API version</Label>
                  {#if p.apiVersionSource === 'server'}
                    <p class="text-sm"><code class="font-mono text-xs">{p.apiVersion}</code> <span class="text-muted-foreground text-xs">{fromServer}</span></p>
                  {:else}
                    <Input id="ai-version" bind:value={apiVersion} autocomplete="off" class="font-mono" placeholder={p.apiVersion} />
                    <p class="text-muted-foreground text-xs">Or <code>v1</code> for Azure's v1 API.</p>
                  {/if}
                </div>
              {/if}
            </div>

            <div class="flex flex-wrap items-center gap-2">
              {#if !p.fixedModels || p.hasApiVersion || p.baseUrlSource !== 'server'}
                <Button variant="secondary" size="sm" disabled={saving || !changes} onclick={() => changes && save(changes, 'Settings saved')}>Save settings</Button>
              {/if}
              <Button variant="outline" size="sm" disabled={testing || !p.configured || !!changes} onclick={test}>
                {#if testing}<Spinner />{:else}<PlugZapIcon />{/if}Test connection
              </Button>
              {#if changes}<span class="text-muted-foreground text-xs">Save to test the new settings.</span>{/if}
            </div>
            {#if check}
              {#if check.ok}
                <p class="flex items-center gap-1.5 text-sm text-emerald-700 dark:text-emerald-400" data-testid="ai-check"><CheckIcon class="size-4" />{p.name} answered with <code class="font-mono text-xs">{check.c.model}</code> in {secs(check.c.ms)}, using tools.</p>
              {:else}
                <p class="text-destructive text-sm" data-testid="ai-check">{check.error}</p>
              {/if}
            {/if}
          </div>
        {/if}
      </Card.Content>
    </Card.Root>
    <Card.Root class="bg-muted/30">
      <Card.Header><Card.Title class="flex items-center gap-2 text-sm"><ShieldCheckIcon class="size-4" />What stays private</Card.Title></Card.Header>
      <Card.Content class="text-muted-foreground flex flex-col gap-2 text-sm">
        <p>Keys are kept on the server. Browsers and published apps never receive them, and only owners and admins can change them.</p>
        <p>Drafts send the app's structure to the provider in use: table and field names, screens, queries, flows and roles. Secret variable values and row data are not sent.</p>
        <p>Answers about data read rows through the same permissions and row rules as the person asked about, and send only what they read.</p>
        <p>A local model (Ollama, LM Studio) keeps everything on your own machines.</p>
        <p>Nothing is applied until a builder reviews the draft and applies it.</p>
      </Card.Content>
    </Card.Root>
  </div>
{/if}
