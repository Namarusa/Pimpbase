<script lang="ts">
  import * as Table from '$lib/components/ui/table'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Switch } from '$lib/components/ui/switch'
  import { Badge } from '$lib/components/ui/badge'
  import TrashIcon from '@lucide/svelte/icons/trash'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import KeyRoundIcon from '@lucide/svelte/icons/key-round'
  import LockIcon from '@lucide/svelte/icons/lock'
  import { toast } from 'svelte-sonner'
  import { api, type Variable } from '../api'

  // VariablesEditor edits the app's {{ env.NAME }} values: one for the
  // builder (development) and one for the published app (production).
  let { appId, variables = $bindable() }: { appId: string; variables: Variable[] } = $props()
  let name = $state('')
  let error = $state('')

  async function save(v: Variable, p: Partial<{ secret: boolean; development: string; production: string }>) {
    try {
      const next = await api.setVariable(appId, v.name, p)
      variables = variables.map((x) => (x.name === v.name ? next : x))
    } catch (e: any) { toast.error(e.message) }
  }
  async function add(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    try {
      const v = await api.setVariable(appId, name.trim(), { development: '', production: '' })
      variables = [...variables, v].sort((a, b) => a.name.localeCompare(b.name))
      name = ''
    } catch (err: any) { error = err.message }
  }
  async function del(v: Variable) {
    try {
      await api.deleteVariable(appId, v.name)
      variables = variables.filter((x) => x.name !== v.name)
    } catch (e: any) { toast.error(e.message) }
  }
  const changed = (e: Event & { currentTarget: HTMLInputElement }, before: string) => e.currentTarget.value !== before
</script>

<div class="mx-auto flex max-w-4xl flex-col gap-5 p-6">
  <div class="flex items-center gap-3">
    <span class="bg-primary/10 text-primary grid size-10 place-items-center rounded-lg"><KeyRoundIcon class="size-5" /></span>
    <div>
      <h2 class="text-lg font-semibold">Variables</h2>
      <p class="text-muted-foreground text-sm">Queries read them as <code class="text-xs">{'{{ env.NAME }}'}</code>. The API studio and designer use development values; the published app uses production. Secrets stay on the server.</p>
    </div>
  </div>
  <div class="rounded-lg border">
    <Table.Root>
      <Table.Header><Table.Row>
        <Table.Head class="w-48">Name</Table.Head><Table.Head>Development</Table.Head><Table.Head>Production</Table.Head><Table.Head class="w-20 text-center">Secret</Table.Head><Table.Head class="w-10"></Table.Head>
      </Table.Row></Table.Header>
      <Table.Body>
        {#each variables as v (v.name)}
          <Table.Row>
            <Table.Cell class="font-mono text-xs font-medium">{v.name}{#if v.secret}<LockIcon class="text-muted-foreground ml-1.5 inline size-3" />{/if}</Table.Cell>
            {#each [['development', v.development, v.hasDevelopment], ['production', v.production, v.hasProduction]] as [key, val, has] (key)}
              <Table.Cell>
                {#if v.secret}
                  <Input type="password" class="h-8 font-mono text-xs" placeholder={has ? '•••••• set, type to replace' : 'not set'} autocomplete="off"
                    onchange={(e) => { const x = e.currentTarget.value; e.currentTarget.value = ''; save(v, { [key as string]: x }) }} aria-label={`${v.name} ${key}`} />
                {:else}
                  <Input class="h-8 font-mono text-xs" value={val as string} placeholder={key === 'development' && v.production ? 'same as production' : ''}
                    onchange={(e) => changed(e, val as string) && save(v, { [key as string]: e.currentTarget.value })} aria-label={`${v.name} ${key}`} />
                {/if}
              </Table.Cell>
            {/each}
            <Table.Cell class="text-center"><Switch checked={v.secret} onCheckedChange={(s) => save(v, { secret: s })} aria-label="Secret" /></Table.Cell>
            <Table.Cell><Button variant="ghost" size="icon" class="hover:text-destructive size-8" onclick={() => del(v)} aria-label={`Delete ${v.name}`}><TrashIcon class="size-4" /></Button></Table.Cell>
          </Table.Row>
        {/each}
        {#if !variables.length}
          <Table.Row><Table.Cell colspan={5} class="text-muted-foreground py-6 text-center text-sm">No variables yet. Add one for each address, key or token your APIs need.</Table.Cell></Table.Row>
        {/if}
      </Table.Body>
    </Table.Root>
  </div>
  <form class="flex items-center gap-2" onsubmit={add}>
    <Input class="h-8 w-64 font-mono text-xs" bind:value={name} placeholder="API_KEY" aria-label="New variable name" />
    <Button type="submit" size="sm" variant="outline" disabled={!name.trim()}><PlusIcon />Add variable</Button>
    {#if error}<span class="text-destructive text-xs">{error}</span>{/if}
  </form>
  <p class="text-muted-foreground text-xs">A development value left empty falls back to production, except for secrets. <Badge variant="secondary" class="ml-1">Tip</Badge> Point <code>API_URL</code> at a test server in development.</p>
</div>
