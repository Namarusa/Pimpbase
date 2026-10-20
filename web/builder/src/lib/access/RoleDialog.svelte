<script lang="ts">
  import * as Dialog from '$lib/components/ui/dialog'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import { Textarea } from '$lib/components/ui/textarea'
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import CheckIcon from '@lucide/svelte/icons/check'
  import { api, type RoleDef } from '../api'
  import RoleSelect from './RoleSelect.svelte'

  let { open = $bindable(false), app, roles, role, onSaved }: {
    open: boolean; app: string; roles: RoleDef[]; role: RoleDef | null; onSaved: (r: RoleDef) => void
  } = $props()

  const swatches = ['#16a34a', '#0891b2', '#2563eb', '#7c3aed', '#c026d3', '#e11d48', '#ea580c', '#ca8a04', '#475569']
  let name = $state('')
  let id = $state('')
  let idTouched = $state(false)
  let inherits = $state('BASIC')
  let color = $state(swatches[0])
  let description = $state('')
  let error = $state('')
  let busy = $state(false)

  $effect(() => {
    if (!open) return
    error = ''
    name = role?.name ?? ''
    id = role?.id ?? ''
    idTouched = !!role
    inherits = role?.inherits ?? 'BASIC'
    color = role?.color || swatches[roles.filter((r) => !r.builtin).length % swatches.length]
    description = role?.description ?? ''
  })

  const autoId = (n: string) => n.toUpperCase().replace(/[^A-Z0-9]+/g, '_').replace(/^[^A-Z]+|_+$/g, '').slice(0, 32)
  // A role cannot inherit itself, a role that inherits it, or Admin.
  const parents = $derived(roles.filter((r) => r.id !== 'ADMIN' && r.id !== role?.id && !inheritsFrom(r.id, role?.id)))
  function inheritsFrom(r: string, target?: string): boolean {
    if (!target) return false
    const seen = new Set<string>()
    for (let cur: string | undefined = r; cur && !seen.has(cur); cur = roles.find((x) => x.id === cur)?.inherits) {
      if (cur === target) return true
      seen.add(cur)
    }
    return false
  }

  async function save(e: SubmitEvent) {
    e.preventDefault()
    error = ''
    busy = true
    try {
      const body = { name, inherits, color, description }
      const saved = role ? await api.updateRole(app, role.id, body) : await api.createRole(app, { ...body, id: id || undefined })
      open = false
      onSaved(saved)
    } catch (err: any) {
      error = err.message
    } finally {
      busy = false
    }
  }
</script>

<Dialog.Root bind:open>
  <Dialog.Content class="sm:max-w-md">
    <form class="flex flex-col gap-4" onsubmit={save}>
      <Dialog.Header>
        <Dialog.Title>{role ? `Edit ${role.name}` : 'New role'}</Dialog.Title>
        <Dialog.Description>A role holds everything the role it inherits holds, plus what you open to it.</Dialog.Description>
      </Dialog.Header>
      {#if error}<p class="text-destructive bg-destructive/10 rounded-md px-3 py-2 text-sm">{error}</p>{/if}
      <div class="grid grid-cols-[1fr_9rem] gap-3">
        <div class="flex flex-col gap-1.5"><Label for="role-name">Name</Label>
          <Input id="role-name" bind:value={name} required maxlength={60} placeholder="Sales team" oninput={() => { if (!idTouched) id = autoId(name) }} /></div>
        <div class="flex flex-col gap-1.5"><Label for="role-id">Id</Label>
          <Input id="role-id" class="font-mono text-xs uppercase" bind:value={id} disabled={!!role} placeholder="SALES" oninput={() => (idTouched = true)} /></div>
      </div>
      <div class="flex flex-col gap-1.5"><Label for="role-inherits">Inherits</Label>
        <RoleSelect id="role-inherits" mode="assign" roles={parents} value={inherits} onChange={(v) => (inherits = v)} />
        <p class="text-muted-foreground text-xs">Members of this role can do everything this role can.</p></div>
      <div class="flex flex-col gap-1.5"><Label>Colour</Label>
        <div class="flex flex-wrap gap-1.5">
          {#each swatches as c (c)}
            <button type="button" class="grid size-7 place-items-center rounded-full ring-offset-2 ring-offset-background" style:background={c} class:ring-2={color === c} style:--tw-ring-color={c} onclick={() => (color = c)} aria-label={c}>
              {#if color === c}<CheckIcon class="size-3.5 text-white" />{/if}
            </button>
          {/each}
        </div></div>
      <div class="flex flex-col gap-1.5"><Label for="role-desc">Description</Label>
        <Textarea id="role-desc" rows={2} bind:value={description} maxlength={300} placeholder="Who gets this role, and why" /></div>
      <Dialog.Footer>
        <Button type="button" variant="outline" onclick={() => (open = false)}>Cancel</Button>
        <Button type="submit" disabled={busy}>{#if busy}<Spinner />{/if}{role ? 'Save role' : 'Create role'}</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
