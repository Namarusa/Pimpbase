// accentVars turns an app's accent colour into the design tokens it sets.
export function accentVars(hex: string | undefined): Record<string, string> {
  if (!hex || !/^#[0-9a-f]{6}$/i.test(hex)) return {}
  const r = parseInt(hex.slice(1, 3), 16), g = parseInt(hex.slice(3, 5), 16), b = parseInt(hex.slice(5, 7), 16)
  const light = (0.299 * r + 0.587 * g + 0.114 * b) / 255 > 0.62
  return {
    '--primary': hex,
    '--primary-foreground': light ? '#111111' : '#ffffff',
    '--ring': hex,
    '--sidebar-primary': hex,
    '--chart-1': hex,
  }
}

export const styleString = (vars: Record<string, string>) => Object.entries(vars).map(([k, v]) => `${k}: ${v}`).join('; ')

export const accentChoices = ['#4f46e5', '#2563eb', '#0891b2', '#059669', '#65a30d', '#d97706', '#dc2626', '#db2777', '#7c3aed', '#18181b']
