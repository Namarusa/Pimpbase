// Screen variables: named values a screen keeps while it is open, read as
// {{ state.name }} and changed with the Update variable action. They are
// declared in the screen's settings with a first value (which may hold
// bindings, such as {{ query.customer }}).
import { resolve } from './bindings'

export interface Variable { name: string; value?: string }

export const validName = (n: string) => /^[A-Za-z_][\w]*$/.test(n)

// initVariables gives each declared variable its first value.
export function initVariables(vars: Variable[] | undefined, state: Record<string, any>, scope: Record<string, any>) {
  for (const v of vars ?? []) {
    if (!v?.name || !validName(v.name)) continue
    state[v.name] = firstValue(v, scope)
  }
}

export function firstValue(v: Variable, scope: Record<string, any>): any {
  const raw = v.value ?? ''
  return raw === '' ? '' : resolve(raw, scope)
}
