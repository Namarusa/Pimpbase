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
  }
}

export const styleString = (vars: Record<string, string>) => Object.entries(vars).map(([k, v]) => `${k}: ${v}`).join('; ')

export const accentChoices = ['#4f46e5', '#2563eb', '#0891b2', '#059669', '#65a30d', '#d97706', '#dc2626', '#db2777', '#7c3aed', '#18181b']

// Chart palettes fill the five --chart-N colours. Each colour is written
// light-dark(light, dark), so it follows the app's light or dark theme;
// tints of the five are used past the fifth group (see Chart.svelte).
type LCH = [number, number, number]
const lch = ([l, c, h]: LCH) => `oklch(${l} ${c} ${h})`
// The dark-theme colour is the same hue, lighter, so it reads on a dark card.
const pair = (c: LCH) => `light-dark(${lch(c)}, ${lch([Math.min(0.86, c[0] + 0.1), c[1] * 0.9, c[2]])})`
const mix = (pct: number, to: string) => `color-mix(in oklch, var(--primary) ${pct}%, var(--${to}))`

export interface ChartPalette { id: string; label: string; colors: string[] }

export const chartPalettes: ChartPalette[] = [
  { id: 'theme', label: 'Theme', colors: ['var(--primary)', ...([[0.65, 0.15, 185], [0.75, 0.16, 75], [0.63, 0.2, 20], [0.6, 0.15, 145]] as LCH[]).map(pair)] },
  { id: 'ocean', label: 'Ocean', colors: ([[0.55, 0.17, 250], [0.68, 0.13, 210], [0.74, 0.12, 180], [0.45, 0.13, 270], [0.8, 0.1, 155]] as LCH[]).map(pair) },
  { id: 'sunset', label: 'Sunset', colors: ([[0.62, 0.2, 30], [0.74, 0.16, 60], [0.82, 0.15, 90], [0.55, 0.21, 0], [0.48, 0.16, 320]] as LCH[]).map(pair) },
  { id: 'forest', label: 'Forest', colors: ([[0.55, 0.14, 150], [0.72, 0.16, 125], [0.62, 0.08, 85], [0.45, 0.09, 180], [0.8, 0.14, 105]] as LCH[]).map(pair) },
  { id: 'berry', label: 'Berry', colors: ([[0.55, 0.22, 340], [0.5, 0.2, 300], [0.7, 0.16, 0], [0.62, 0.18, 270], [0.78, 0.11, 20]] as LCH[]).map(pair) },
  { id: 'mono', label: 'Shades', colors: ['var(--primary)', mix(70, 'foreground'), mix(55, 'background'), mix(45, 'foreground'), mix(30, 'background')] },
]

// paletteVars gives the --chart-N values of a palette. "custom" uses the
// given #rrggbb colours as they are in light and dark themes.
export function paletteVars(id: string | undefined, custom?: string[]): Record<string, string> {
  const colors = id === 'custom' ? (custom ?? []) : (chartPalettes.find((p) => p.id === id) ?? chartPalettes[0]).colors
  return Object.fromEntries(colors.slice(0, 5).map((c, i) => [`--chart-${i + 1}`, c]))
}

// Corner roundness and typefaces an app may choose.
export const radii: Record<string, string> = { none: '0rem', sm: '0.375rem', md: '0.625rem', lg: '0.875rem', xl: '1.25rem' }
export const fonts: Record<string, { label: string; css: string }> = {
  inter: { label: 'Inter', css: "Inter, ui-sans-serif, system-ui, -apple-system, 'Segoe UI', sans-serif" },
  system: { label: 'System', css: "system-ui, -apple-system, 'Segoe UI', Roboto, sans-serif" },
  serif: { label: 'Serif', css: "ui-serif, Georgia, Cambria, 'Times New Roman', serif" },
  mono: { label: 'Mono', css: "ui-monospace, 'SF Mono', Menlo, Consolas, monospace" },
  rounded: { label: 'Rounded', css: "ui-rounded, 'SF Pro Rounded', 'Nunito', system-ui, sans-serif" },
}

interface ThemeDesign { accent?: string; chartPalette?: string; chartColors?: string[]; radius?: string; font?: string; tokens?: { name: string; value: string }[] }

// tokenVar is the CSS variable of a named token.
export const tokenVar = (name: string) => `--t-${name}`

// themeVars are all the design tokens an app's design sets.
export function themeVars(design: ThemeDesign): Record<string, string> {
  const out: Record<string, string> = { ...accentVars(design.accent), ...paletteVars(design.chartPalette, design.chartColors) }
  if (design.radius && radii[design.radius]) out['--radius'] = radii[design.radius]
  if (design.font && fonts[design.font]) out['--font-sans'] = fonts[design.font].css
  for (const t of design.tokens ?? []) if (/^[A-Za-z][A-Za-z0-9_]*$/.test(t.name) && t.value) out[tokenVar(t.name)] = t.value.replace(/[;{}<>\\]/g, '')
  return out
}

// themeColors are the theme's own colours, by the name styles use.
export const themeColors: { name: string; css: string; label: string }[] = [
  ['primary', 'Accent'], ['primaryForeground', 'Text on accent'], ['background', 'Page'], ['foreground', 'Text'],
  ['card', 'Card'], ['cardForeground', 'Text on card'], ['muted', 'Muted'], ['mutedForeground', 'Muted text'],
  ['accent', 'Highlight'], ['border', 'Border'], ['destructive', 'Danger'],
  ['chart1', 'Chart 1'], ['chart2', 'Chart 2'], ['chart3', 'Chart 3'], ['chart4', 'Chart 4'], ['chart5', 'Chart 5'],
].map(([name, label]) => ({ name, label, css: `var(--${name.replace(/[A-Z]/g, (c) => '-' + c.toLowerCase()).replace(/(\d)$/, '-$1')})` }))

// themeScope is what {{ theme.x }} reads: CSS that follows the app's
// theme, light or dark, so styles can use it.
export function themeScope(design: ThemeDesign): Record<string, string> {
  const out: Record<string, string> = Object.fromEntries(themeColors.map((c) => [c.name, c.css]))
  out.radius = 'var(--radius)'
  out.font = 'var(--font-sans)'
  for (const t of design.tokens ?? []) if (/^[A-Za-z][A-Za-z0-9_]*$/.test(t.name)) out[t.name] = `var(${tokenVar(t.name)})`
  return out
}
