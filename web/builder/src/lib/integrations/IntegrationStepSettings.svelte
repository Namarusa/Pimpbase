<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Label } from '$lib/components/ui/label'
  import ChevronDownIcon from '@lucide/svelte/icons/chevron-down'
  import PencilIcon from '@lucide/svelte/icons/pencil'
  import FlaskConicalIcon from '@lucide/svelte/icons/flask-conical'
  import type { FlowStep } from '../api'
  import type { BindingGroup } from '../designer/context'
  import { getEditor } from '../flows/editor.svelte'
  import { navigate } from '../router.svelte'
  import IntegrationIcon from './IntegrationIcon.svelte'
  import InputField from './InputField.svelte'
  import { actionOf, integrations, keyMode, manifestOf } from './state.svelte'

  // IntegrationStepSettings edits a step that runs an action of a
  // connection: which connection, which action, and its settings.
  let { step, groups, scope }: { step: FlowStep; groups: BindingGroup[]; scope: Record<string, any> | null } = $props()
  const ed = getEditor()
  const found = $derived(actionOf(step.inputs))
  const params = $derived((step.inputs.params ?? {}) as Record<string, any>)
  const basic = $derived((found.action?.inputs ?? []).filter((i) => !i.advanced))
  const advanced = $derived((found.action?.inputs ?? []).filter((i) => i.advanced))
  let more = $state(false)
  const testKey = $derived(found.conn ? Object.values(found.conn.secrets).some((s) => s.test) : false)

  function setParam(k: string, v: any) {
    step.inputs.params = { ...params, [k]: v }
    ed.changed()
  }
  function setConnection(id: string) {
    const next = integrations.connections.find((c) => c.id === id)
    const same = next && next.kind === found.conn?.kind
    step.inputs.connection = id
    if (!same) {
      step.inputs.action = manifestOf(next?.kind)?.actions[0]?.key ?? ''
      step.inputs.params = {}
    }
    ed.changed()
  }
  function setAction(key: string) {
    // Keep what still applies, such as the customer id.
    const a = found.manifest?.actions.find((x) => x.key === key)
    step.inputs.action = key
    step.inputs.params = Object.fromEntries(Object.entries(params).filter(([k]) => a?.inputs.some((i) => i.key === k)))
    ed.changed()
  }
</script>

<div class="flex flex-col gap-1.5">
  <Label class="text-xs">Connection</Label>
  <div class="flex items-center gap-1.5">
    <Select.Root type="single" value={step.inputs.connection ?? ''} onValueChange={setConnection}>
      <Select.Trigger size="sm" class="min-w-0 flex-1">
        {#if found.conn}<span class="flex items-center gap-2"><IntegrationIcon manifest={found.manifest} size="sm" />{found.conn.name}</span>{:else}Choose a connection{/if}
      </Select.Trigger>
      <Select.Content>
        {#each integrations.connections as c (c.id)}
          <Select.Item value={c.id} label={c.name}><span class="flex items-center gap-2"><IntegrationIcon manifest={manifestOf(c.kind)} size="sm" />{c.name}</span></Select.Item>
        {/each}
        {#if !integrations.connections.length}<div class="text-muted-foreground px-2 py-1.5 text-xs">Connect a service in the Integrations tab first.</div>{/if}
      </Select.Content>
    </Select.Root>
    {#if ed.app}<Button variant="outline" size="sm" class="h-8 shrink-0 text-xs" onclick={() => navigate(`/apps/${ed.app!.slug}/integrations${found.conn ? '/' + found.conn.id : ''}`)}><PencilIcon />{found.conn ? 'Settings' : 'Connect'}</Button>{/if}
  </div>
  {#if found.conn && testKey}
    <p class="text-muted-foreground flex items-center gap-1 text-xs"><FlaskConicalIcon class="size-3" />Test runs use the test {keyMode(found.conn, 'test') === 'test' ? 'mode key' : 'values'}; switched-on runs use the live ones.</p>
  {/if}
</div>

{#if found.manifest}
  <div class="flex flex-col gap-1.5">
    <Label class="text-xs">Action</Label>
    <Select.Root type="single" value={step.inputs.action ?? ''} onValueChange={setAction}>
      <Select.Trigger size="sm" class="w-full">{found.action?.label ?? 'Choose what it does'}</Select.Trigger>
      <Select.Content>
        {#each found.manifest.actions as a (a.key)}
          <Select.Item value={a.key} label={a.label}><div><div>{a.label}</div><div class="text-muted-foreground text-xs">{a.description}</div></div></Select.Item>
        {/each}
      </Select.Content>
    </Select.Root>
    {#if found.action}<p class="text-muted-foreground text-xs">{found.action.description}</p>{/if}
  </div>
  {#each basic as i (i.key)}
    <InputField input={i} value={params[i.key]} {groups} {scope} onChange={(v) => setParam(i.key, v)} />
  {/each}
  {#if advanced.length}
    <button type="button" class="text-muted-foreground hover:text-foreground flex items-center gap-1 self-start text-xs font-medium" onclick={() => (more = !more)}>
      <ChevronDownIcon class={['size-3.5 transition-transform', more ? '' : '-rotate-90']} />More settings ({advanced.length})
    </button>
    {#if more}
      {#each advanced as i (i.key)}
        <InputField input={i} value={params[i.key]} {groups} {scope} onChange={(v) => setParam(i.key, v)} />
      {/each}
    {/if}
  {/if}
{:else if step.inputs.connection}
  <p class="text-destructive text-xs">This step's connection was deleted. Choose another.</p>
{/if}
