<script lang="ts">
  // RolesPicker picks any number of roles; none means everyone.
  import * as Popover from '$lib/components/ui/popover'
  import { Checkbox } from '$lib/components/ui/checkbox'
  import { Button } from '$lib/components/ui/button'
  import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down'
  import type { Role, RoleDef } from '../api'
  import { roleColor, roleName, sorted } from './roles'
  import RoleDot from './RoleDot.svelte'

  let { value, roles, onChange, empty = 'Everyone who can open the screen', id }: {
    value: Role[]; roles: RoleDef[]; onChange: (v: Role[]) => void; empty?: string; id?: string
  } = $props()

  function toggle(r: Role, on: boolean) {
    onChange(on ? [...value.filter((x) => x !== r), r] : value.filter((x) => x !== r))
  }
</script>

<Popover.Root>
  <Popover.Trigger {id}>
    {#snippet child({ props })}
      <Button {...props} variant="outline" size="sm" class="h-auto min-h-8 w-full justify-between px-2.5 py-1 font-normal">
        {#if value.length === 0}
          <span class="text-muted-foreground truncate">{empty}</span>
        {:else}
          <span class="flex flex-wrap gap-1">
            {#each value as r (r)}<span class="bg-muted inline-flex items-center gap-1 rounded px-1.5 py-0.5 text-xs"><RoleDot color={roleColor(roles, r)} />{roleName(roles, r)}</span>{/each}
          </span>
        {/if}
        <ChevronsUpDownIcon class="opacity-50" />
      </Button>
    {/snippet}
  </Popover.Trigger>
  <Popover.Content class="w-64 p-1" align="start">
    {#each sorted(roles).filter((r) => r.id !== 'PUBLIC') as r (r.id)}
      <label class="hover:bg-muted flex cursor-pointer items-center gap-2 rounded-md px-2 py-1.5 text-sm">
        <Checkbox checked={value.includes(r.id)} onCheckedChange={(v) => toggle(r.id, !!v)} />
        <RoleDot color={r.color || '#64748b'} />
        <span class="flex-1 truncate">{r.name}</span>
        {#if !r.builtin}<span class="text-muted-foreground font-mono text-[10px]">{r.id}</span>{/if}
      </label>
    {/each}
    <p class="text-muted-foreground border-t px-2 pt-1.5 pb-1 text-xs">Roles that include a picked role see it too. Admins see everything.</p>
  </Popover.Content>
</Popover.Root>
