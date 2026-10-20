<script lang="ts">
  // TablePermissions edits who may read, write and delete one table's rows,
  // and the row rules that limit which rows each role uses.
  import * as Card from '$lib/components/ui/card'
  import { Button } from '$lib/components/ui/button'
  import { Label } from '$lib/components/ui/label'
  import { Switch } from '$lib/components/ui/switch'
  import { Spinner } from '$lib/components/ui/spinner'
  import { toast } from 'svelte-sonner'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import TrashIcon from '@lucide/svelte/icons/trash-2'
  import TableIcon from '@lucide/svelte/icons/table-2'
  import FilterIcon from '@lucide/svelte/icons/list-filter'
  import { api, type Permissions, type RoleDef, type Table } from '../api'
  import { chain, roleName, sorted } from './roles'
  import RoleSelect from './RoleSelect.svelte'
  import RoleDot from './RoleDot.svelte'
  import RuleInput from './RuleInput.svelte'

  let { app, table, roles, onSaved }: { app: string; table: Table; roles: RoleDef[]; onSaved: (t: Table) => void } = $props()

  const clone = (p: Permissions): Permissions => ({ read: p.read, write: p.write, delete: p.delete ?? p.write, rows: (p.rows ?? []).map((r) => ({ ...r })) })
  let perm = $state(clone(table.permissions))
  let busy = $state(false)
  const dirty = $derived(JSON.stringify(perm) !== JSON.stringify(clone(table.permissions)))

  // What each role ends up with, following inheritance.
  const effective = $derived(sorted(roles).map((r) => {
    const holds = (need: string) => r.id === 'ADMIN' || chain(roles, r.id).includes(need)
    let rule: { filter: string; from: string } | null = null
    if (r.id !== 'ADMIN') {
      for (const c of chain(roles, r.id)) {
        const hit = perm.rows.find((x) => x.role === c)
        if (hit) { rule = { filter: hit.filter, from: c }; break }
      }
    }
    return { role: r, read: holds(perm.read), write: holds(perm.write), del: holds(perm.delete), rule }
  }))

  const free = $derived(sorted(roles).filter((r) => r.id !== 'ADMIN' && !perm.rows.some((x) => x.role === r.id)))
  function addRule() {
    const r = free.find((x) => x.id === 'BASIC') ?? free[0]
    if (r) perm.rows.push({ role: r.id, filter: '' })
  }

  async function save() {
    busy = true
    try {
      const t = await api.updateTable(app, table.id, { permissions: perm })
      onSaved(t)
      perm = clone(t.permissions)
      toast.success(`Saved the permissions of ${table.name}`)
    } catch (e: any) {
      toast.error(e.message)
    } finally {
      busy = false
    }
  }
</script>

<Card.Root class="gap-0 py-0">
  <Card.Header class="flex flex-row items-center gap-3 border-b px-5 py-3.5 [.border-b]:pb-3.5">
    <span class="bg-primary/10 text-primary grid size-8 place-items-center rounded-md"><TableIcon class="size-4" /></span>
    <div class="flex-1">
      <Card.Title class="text-base">{table.name}</Card.Title>
      <Card.Description class="text-xs">{table.fields.length} fields</Card.Description>
    </div>
    {#if dirty}
      <Button variant="ghost" size="sm" onclick={() => (perm = clone(table.permissions))}>Discard</Button>
      <Button size="sm" onclick={save} disabled={busy}>{#if busy}<Spinner />{/if}Save</Button>
    {/if}
  </Card.Header>
  <Card.Content class="grid gap-5 px-5 py-4 lg:grid-cols-[1fr_17rem]">
    <div class="flex min-w-0 flex-col gap-5">
      <div class="grid gap-3 sm:grid-cols-3">
        <div class="flex flex-col gap-1.5"><Label class="text-xs">Read rows</Label>
          <RoleSelect {roles} value={perm.read} onChange={(v) => (perm.read = v)} /></div>
        <div class="flex flex-col gap-1.5"><Label class="text-xs">Add and change rows</Label>
          <RoleSelect {roles} value={perm.write} onChange={(v) => (perm.write = v)} /></div>
        <div class="flex flex-col gap-1.5"><Label class="text-xs">Delete rows</Label>
          <RoleSelect {roles} value={perm.delete} onChange={(v) => (perm.delete = v)} /></div>
      </div>
      <div class="flex flex-col gap-2.5">
        <div class="flex items-center gap-2">
          <FilterIcon class="text-muted-foreground size-4" />
          <span class="text-sm font-medium">Row rules</span>
          <span class="text-muted-foreground text-xs">Limit the rows a role reads, changes and deletes. Roles inherit the nearest rule.</span>
        </div>
        {#each perm.rows as rule, i (i)}
          <div class="bg-muted/30 flex flex-col gap-2.5 rounded-lg border p-3">
            <div class="flex flex-wrap items-center gap-2">
              <RoleSelect mode="assign" class="w-48" roles={roles.filter((r) => r.id !== 'ADMIN' && (r.id === rule.role || !perm.rows.some((x) => x.role === r.id)))} value={rule.role} onChange={(v) => (rule.role = v)} />
              <div class="ml-auto flex items-center gap-2">
                <Label for={`all-${table.id}-${i}`} class="text-xs font-normal">Every row</Label>
                <Switch id={`all-${table.id}-${i}`} checked={rule.filter === ''} onCheckedChange={(v) => (rule.filter = v ? '' : '[Owner] = Current.User.Email')} />
                <Button variant="ghost" size="icon" class="hover:text-destructive size-8" onclick={() => perm.rows.splice(i, 1)} aria-label="Remove the rule"><TrashIcon /></Button>
              </div>
            </div>
            {#if rule.filter !== ''}
              <RuleInput {app} {table} value={rule.filter} onChange={(v) => (rule.filter = v)} />
            {:else}
              <p class="text-muted-foreground text-xs">{roleName(roles, rule.role)} uses every row, even when a role it inherits has a rule.</p>
            {/if}
          </div>
        {/each}
        <Button variant="outline" size="sm" class="self-start" onclick={addRule} disabled={free.length === 0}><PlusIcon />Add a row rule</Button>
      </div>
    </div>
    <div class="flex flex-col gap-2 lg:border-l lg:pl-5">
      <span class="text-muted-foreground text-[11px] font-semibold tracking-wider uppercase">What each role gets</span>
      {#each effective as e (e.role.id)}
        <div class="flex flex-col gap-0.5 py-1">
          <div class="flex items-center gap-1.5 text-sm"><RoleDot color={e.role.color || '#64748b'} /><span class="font-medium">{e.role.name}</span>
            <span class="ml-auto flex gap-1 font-mono text-[10px]">
              <span class={e.read ? 'text-emerald-600 dark:text-emerald-400' : 'text-muted-foreground/50 line-through'}>R</span>
              <span class={e.write ? 'text-emerald-600 dark:text-emerald-400' : 'text-muted-foreground/50 line-through'}>W</span>
              <span class={e.del ? 'text-emerald-600 dark:text-emerald-400' : 'text-muted-foreground/50 line-through'}>D</span>
            </span></div>
          <div class="text-muted-foreground truncate pl-3.5 text-xs" title={e.rule?.filter}>
            {#if !e.read}No access
            {:else if !e.rule || e.rule.filter === ''}Every row
            {:else}<span class="font-mono">{e.rule.filter}</span>{#if e.rule.from !== e.role.id} <span class="italic">· from {roleName(roles, e.rule.from)}</span>{/if}{/if}
          </div>
        </div>
      {/each}
    </div>
  </Card.Content>
</Card.Root>
