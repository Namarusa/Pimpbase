// Helpers for showing app roles.
import { builtinRoles, type Role, type RoleDef } from '../api'

export const roleDef = (roles: RoleDef[], id: Role): RoleDef | undefined =>
  roles.find((r) => r.id === id) ?? builtinRoles.find((r) => r.id === id)

export const roleName = (roles: RoleDef[], id: Role): string =>
  !id || id === 'NONE' ? 'No access' : roleDef(roles, id)?.name ?? id

export const roleColor = (roles: RoleDef[], id: Role): string => roleDef(roles, id)?.color || '#64748b'

// requirement says who holds a role that a screen, table or query needs.
export function requirement(roles: RoleDef[], id: Role): string {
  if (id === 'PUBLIC') return 'Anyone, without signing in'
  if (id === 'ADMIN') return 'Admins only'
  return `${roleName(roles, id)} and roles that include it`
}

// shortRequirement is requirement for tight spaces.
export function shortRequirement(roles: RoleDef[], id: Role): string {
  if (id === 'PUBLIC') return 'Anyone'
  if (id === 'ADMIN') return 'Admins only'
  return `${roleName(roles, id)} and up`
}

// chain lists a role and the roles it inherits, nearest first.
export function chain(roles: RoleDef[], id: Role): Role[] {
  const out: Role[] = []
  for (let cur: Role | undefined = id; cur && !out.includes(cur); cur = roleDef(roles, cur)?.inherits) out.push(cur)
  return out
}

// sorted puts built-in roles first, most powerful first, then custom ones.
export const sorted = (roles: RoleDef[]): RoleDef[] => [...roles.filter((r) => r.builtin), ...roles.filter((r) => !r.builtin)]
