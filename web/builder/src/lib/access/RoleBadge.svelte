<script lang="ts">
  import type { Role, RoleDef } from '../api'
  import { roleColor, roleName } from './roles'
  import RoleDot from './RoleDot.svelte'

  let { roles, role, class: cls = '' }: { roles: RoleDef[]; role: Role; class?: string } = $props()
</script>

<span class={['bg-muted/60 inline-flex h-6 items-center gap-1.5 rounded-full border px-2 text-xs font-medium whitespace-nowrap', cls]}>
  <RoleDot color={roleColor(roles, role)} />{roleName(roles, role)}
</span>
