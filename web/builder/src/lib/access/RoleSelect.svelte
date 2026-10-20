<script lang="ts">
  // RoleSelect picks one app role. As a requirement ("Who can open it"),
  // labels say who that lets in; as an assignment, they name the role.
  import * as Select from '$lib/components/ui/select'
  import type { Role, RoleDef } from '../api'
  import { requirement, roleColor, shortRequirement, roleName, sorted } from './roles'
  import RoleDot from './RoleDot.svelte'

  let { value, roles, onChange, mode = 'require', none = false, noneLabel = 'No access', publicRole = true, id, size = 'sm', class: cls = 'w-full', disabled = false }: {
    value: Role; roles: RoleDef[]; onChange: (r: Role) => void; mode?: 'require' | 'assign'
    none?: boolean; noneLabel?: string; publicRole?: boolean; id?: string; size?: 'sm' | 'default'; class?: string; disabled?: boolean
  } = $props()

  const list = $derived(sorted(roles).filter((r) => publicRole || r.id !== 'PUBLIC'))
  const short = (r: Role) => (r === 'NONE' ? noneLabel : mode === 'require' ? shortRequirement(roles, r) : roleName(roles, r))
  const label = (r: Role) => (r === 'NONE' ? noneLabel : mode === 'require' ? requirement(roles, r) : roleName(roles, r))
</script>

<Select.Root type="single" value={value || 'NONE'} onValueChange={(v) => onChange(v as Role)} {disabled}>
  <Select.Trigger {id} {size} class={cls}>
    <span class="flex min-w-0 items-center gap-2">
      {#if value && value !== 'NONE'}<RoleDot color={roleColor(roles, value)} />{/if}
      <span class="truncate" title={label(value || 'NONE')}>{short(value || 'NONE')}</span>
    </span>
  </Select.Trigger>
  <Select.Content>
    {#if none}<Select.Item value="NONE" label={noneLabel}><span class="text-muted-foreground">{noneLabel}</span></Select.Item>{/if}
    {#each list as r (r.id)}
      <Select.Item value={r.id} label={label(r.id)}>
        <RoleDot color={r.color || '#64748b'} />
        <span class="truncate">{label(r.id)}</span>
        {#if !r.builtin}<span class="text-muted-foreground ml-auto font-mono text-[10px]">{r.id}</span>{/if}
      </Select.Item>
    {/each}
  </Select.Content>
</Select.Root>
