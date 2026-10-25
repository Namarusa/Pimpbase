// Styles builders set on a component in the Style tab. They live in
// props.style as layers of CSS values:
//
//   { base: { padding: '16px', color: '{{ theme.brand }}' },
//     tablet: { 'grid-template-columns': '2' }, phone: { hidden: 'true' },
//     hover: { 'box-shadow': 'lg' }, focus: {…}, active: {…},
//     css: 'outline: 1px solid red' }
//
// Values are text and may hold {{ bindings }}, so a row, a form value, a
// route value, a theme token or a formula can choose them. A value that
// comes out empty is left unset.

export type StyleLayer = 'base' | 'tablet' | 'phone' | 'hover' | 'focus' | 'active'
export const layers: StyleLayer[] = ['base', 'tablet', 'phone', 'hover', 'focus', 'active']

export type StyleMap = Record<string, string>
export type Style = Partial<Record<StyleLayer, StyleMap>> & { css?: string }

// Where a value goes: box is the component's place on the screen (size,
// margin, position), surface is the component itself (colours, border,
// padding, text) and layout is the element that lays out its children.
export type StyleTarget = 'box' | 'surface' | 'layout'
export type StyleInput = 'choice' | 'length' | 'color' | 'text' | 'number' | 'switch'

export interface StyleProp {
  key: string
  label: string
  group: string
  target: StyleTarget
  input: StyleInput
  options?: { value: string; label: string }[]
  // suggestions are offered while typing.
  suggestions?: string[]
  placeholder?: string
  // px adds "px" to a bare number.
  px?: boolean
}

const o = (...pairs: [string, string][]) => pairs.map(([value, label]) => ({ value, label }))
const P = (group: string, target: StyleTarget, input: StyleInput, key: string, label: string, extra: Partial<StyleProp> = {}): StyleProp =>
  ({ key, label, group, target, input, px: input === 'length', ...extra })

const lengths = ['auto', '0', '8px', '16px', '24px', '1rem', '50%', '100%', 'fit-content', 'max-content']
const gaps = ['0', '4px', '8px', '12px', '16px', '24px', '32px', '48px']
const tracks = ['1', '2', '3', '4', 'repeat(auto-fill, minmax(200px, 1fr))', '240px 1fr', '1fr 2fr', '1fr auto']
const align = o(['start', 'Start'], ['center', 'Center'], ['end', 'End'], ['stretch', 'Fill'], ['baseline', 'Baseline'])

// shadows are named so a binding can pick one: {{ = IF(row.Late, "lg", "none") }}.
export const shadows: Record<string, string> = {
  none: 'none',
  xs: '0 1px 2px 0 rgb(0 0 0 / 0.05)',
  sm: '0 1px 3px 0 rgb(0 0 0 / 0.1), 0 1px 2px -1px rgb(0 0 0 / 0.1)',
  md: '0 4px 6px -1px rgb(0 0 0 / 0.1), 0 2px 4px -2px rgb(0 0 0 / 0.1)',
  lg: '0 10px 15px -3px rgb(0 0 0 / 0.1), 0 4px 6px -4px rgb(0 0 0 / 0.1)',
  xl: '0 20px 25px -5px rgb(0 0 0 / 0.1), 0 8px 10px -6px rgb(0 0 0 / 0.1)',
  inner: 'inset 0 2px 4px 0 rgb(0 0 0 / 0.06)',
  ring: '0 0 0 3px color-mix(in oklch, var(--primary) 35%, transparent)',
}

export const styleProps: StyleProp[] = [
  P('Layout', 'layout', 'choice', 'display', 'Display', { options: o(['flex', 'Flex'], ['grid', 'Grid'], ['block', 'Block']) }),
  P('Layout', 'layout', 'choice', 'flex-direction', 'Direction', { options: o(['column', 'Column'], ['row', 'Row'], ['column-reverse', 'Column ↑'], ['row-reverse', 'Row ←']) }),
  P('Layout', 'layout', 'choice', 'flex-wrap', 'Wrap', { options: o(['nowrap', 'No'], ['wrap', 'Wrap'], ['wrap-reverse', 'Reverse']) }),
  P('Layout', 'layout', 'choice', 'justify-content', 'Justify', { options: o(['start', 'Start'], ['center', 'Center'], ['end', 'End'], ['space-between', 'Between'], ['space-around', 'Around'], ['space-evenly', 'Evenly']) }),
  P('Layout', 'layout', 'choice', 'align-items', 'Align items', { options: align }),
  P('Layout', 'layout', 'choice', 'align-content', 'Align lines', { options: o(['start', 'Start'], ['center', 'Center'], ['end', 'End'], ['space-between', 'Between'], ['stretch', 'Fill']) }),
  P('Layout', 'layout', 'length', 'gap', 'Gap', { suggestions: gaps }),
  P('Layout', 'layout', 'length', 'row-gap', 'Row gap', { suggestions: gaps }),
  P('Layout', 'layout', 'length', 'column-gap', 'Column gap', { suggestions: gaps }),

  P('Grid', 'layout', 'text', 'grid-template-columns', 'Columns', { suggestions: tracks, placeholder: '3 or 240px 1fr' }),
  P('Grid', 'layout', 'text', 'grid-template-rows', 'Rows', { suggestions: tracks, placeholder: 'auto 1fr auto' }),
  P('Grid', 'layout', 'text', 'grid-template-areas', 'Areas', { placeholder: '"head head" "side main"' }),
  P('Grid', 'layout', 'choice', 'grid-auto-flow', 'Fill by', { options: o(['row', 'Rows'], ['column', 'Columns'], ['row dense', 'Dense']) }),
  P('Grid', 'layout', 'length', 'grid-auto-rows', 'Row height', { suggestions: ['auto', 'minmax(120px, auto)', '1fr'] }),
  P('Grid', 'layout', 'choice', 'justify-items', 'Justify cells', { options: o(['start', 'Start'], ['center', 'Center'], ['end', 'End'], ['stretch', 'Fill']) }),

  P('Placement', 'box', 'choice', 'align-self', 'Align self', { options: o(['auto', 'Auto'], ['start', 'Start'], ['center', 'Center'], ['end', 'End'], ['stretch', 'Fill']) }),
  P('Placement', 'box', 'choice', 'justify-self', 'Justify self', { options: o(['auto', 'Auto'], ['start', 'Start'], ['center', 'Center'], ['end', 'End'], ['stretch', 'Fill']) }),
  P('Placement', 'box', 'number', 'flex-grow', 'Grow', { placeholder: '0' }),
  P('Placement', 'box', 'number', 'flex-shrink', 'Shrink', { placeholder: '1' }),
  P('Placement', 'box', 'length', 'flex-basis', 'Basis', { suggestions: ['auto', '0', '200px', '33%', '50%'] }),
  P('Placement', 'box', 'number', 'order', 'Order', { placeholder: '0' }),
  P('Placement', 'box', 'text', 'grid-column', 'Grid column', { suggestions: ['2', '1 / -1', '1 / 3', 'span 2'], placeholder: '2 spans two columns' }),
  P('Placement', 'box', 'text', 'grid-row', 'Grid row', { suggestions: ['2', '1 / 3', 'span 2'] }),
  P('Placement', 'box', 'text', 'grid-area', 'Grid area', { placeholder: 'main' }),

  P('Size', 'box', 'length', 'width', 'Width', { suggestions: lengths }),
  P('Size', 'box', 'length', 'height', 'Height', { suggestions: lengths }),
  P('Size', 'box', 'length', 'min-width', 'Min width', { suggestions: lengths }),
  P('Size', 'box', 'length', 'max-width', 'Max width', { suggestions: ['none', '480px', '720px', '960px', '1200px', '100%'] }),
  P('Size', 'box', 'length', 'min-height', 'Min height', { suggestions: lengths }),
  P('Size', 'box', 'length', 'max-height', 'Max height', { suggestions: ['none', '300px', '50vh', '100%'] }),
  P('Size', 'box', 'text', 'aspect-ratio', 'Aspect ratio', { suggestions: ['1 / 1', '16 / 9', '4 / 3', '3 / 4'] }),

  P('Spacing', 'box', 'length', 'margin', 'Margin', { suggestions: ['0', '8px', '16px', '0 auto', '24px 0'] }),
  P('Spacing', 'box', 'length', 'margin-top', 'Top'),
  P('Spacing', 'box', 'length', 'margin-right', 'Right'),
  P('Spacing', 'box', 'length', 'margin-bottom', 'Bottom'),
  P('Spacing', 'box', 'length', 'margin-left', 'Left'),
  P('Spacing', 'surface', 'length', 'padding', 'Padding', { suggestions: ['0', '8px', '16px', '24px', '12px 16px'] }),
  P('Spacing', 'surface', 'length', 'padding-top', 'Top'),
  P('Spacing', 'surface', 'length', 'padding-right', 'Right'),
  P('Spacing', 'surface', 'length', 'padding-bottom', 'Bottom'),
  P('Spacing', 'surface', 'length', 'padding-left', 'Left'),

  P('Position', 'box', 'choice', 'position', 'Position', { options: o(['static', 'Normal'], ['relative', 'Relative'], ['absolute', 'Absolute'], ['fixed', 'Fixed'], ['sticky', 'Sticky']) }),
  P('Position', 'box', 'length', 'top', 'Top'),
  P('Position', 'box', 'length', 'right', 'Right'),
  P('Position', 'box', 'length', 'bottom', 'Bottom'),
  P('Position', 'box', 'length', 'left', 'Left'),
  P('Position', 'box', 'number', 'z-index', 'Layer (z-index)', { placeholder: '0' }),
  P('Position', 'surface', 'choice', 'overflow', 'Overflow', { options: o(['visible', 'Show'], ['hidden', 'Clip'], ['auto', 'Scroll']) }),

  P('Typography', 'surface', 'text', 'font-family', 'Font', { suggestions: ['var(--font-sans)', 'ui-serif, Georgia, serif', 'ui-monospace, monospace', 'ui-rounded, system-ui, sans-serif'] }),
  P('Typography', 'surface', 'length', 'font-size', 'Size', { suggestions: ['12px', '14px', '16px', '20px', '24px', '32px', '1.25rem', 'clamp(1.5rem, 4cqw, 3rem)'] }),
  P('Typography', 'surface', 'choice', 'font-weight', 'Weight', { options: o(['300', 'Light'], ['400', 'Regular'], ['500', 'Medium'], ['600', 'Semibold'], ['700', 'Bold'], ['800', 'Heavy']) }),
  P('Typography', 'surface', 'color', 'color', 'Colour'),
  P('Typography', 'surface', 'choice', 'text-align', 'Align', { options: o(['left', 'Left'], ['center', 'Center'], ['right', 'Right'], ['justify', 'Justify']) }),
  P('Typography', 'surface', 'text', 'line-height', 'Line height', { suggestions: ['1', '1.25', '1.5', '1.75', '2', '24px'] }),
  P('Typography', 'surface', 'length', 'letter-spacing', 'Letter spacing', { suggestions: ['-0.02em', '0', '0.05em', '0.1em', '1px'] }),
  P('Typography', 'surface', 'choice', 'text-transform', 'Case', { options: o(['none', 'As typed'], ['uppercase', 'AB'], ['lowercase', 'ab'], ['capitalize', 'Ab']) }),
  P('Typography', 'surface', 'choice', 'text-decoration', 'Decoration', { options: o(['none', 'None'], ['underline', 'Underline'], ['line-through', 'Strike']) }),
  P('Typography', 'surface', 'choice', 'font-style', 'Style', { options: o(['normal', 'Normal'], ['italic', 'Italic']) }),
  P('Typography', 'surface', 'choice', 'white-space', 'Line breaks', { options: o(['normal', 'Wrap'], ['nowrap', 'One line'], ['pre-line', 'Keep']) }),

  P('Background', 'surface', 'color', 'background-color', 'Colour'),
  P('Background', 'surface', 'text', 'background-image', 'Image or gradient', { suggestions: ['linear-gradient(135deg, var(--primary), var(--chart-2))', 'radial-gradient(circle at top, var(--muted), transparent)', 'https://'], placeholder: 'a URL or a gradient' }),
  P('Background', 'surface', 'choice', 'background-size', 'Image size', { options: o(['cover', 'Cover'], ['contain', 'Contain'], ['auto', 'Actual']) }),
  P('Background', 'surface', 'choice', 'background-position', 'Image position', { options: o(['center', 'Center'], ['top', 'Top'], ['bottom', 'Bottom'], ['left', 'Left'], ['right', 'Right']) }),
  P('Background', 'surface', 'choice', 'background-repeat', 'Repeat', { options: o(['no-repeat', 'No'], ['repeat', 'Tile'], ['repeat-x', 'Across'], ['repeat-y', 'Down']) }),

  P('Border', 'surface', 'length', 'border-width', 'Width', { suggestions: ['0', '1px', '2px', '0 0 1px 0'] }),
  P('Border', 'surface', 'choice', 'border-style', 'Style', { options: o(['solid', 'Solid'], ['dashed', 'Dashed'], ['dotted', 'Dotted'], ['none', 'None']) }),
  P('Border', 'surface', 'color', 'border-color', 'Colour'),
  P('Border', 'surface', 'length', 'border-radius', 'Radius', { suggestions: ['0', '4px', '8px', 'var(--radius)', '16px', '9999px', '12px 12px 0 0'] }),

  P('Effects', 'surface', 'text', 'box-shadow', 'Shadow', { suggestions: Object.keys(shadows), placeholder: 'sm, md, lg or CSS' }),
  P('Effects', 'box', 'text', 'opacity', 'Opacity', { suggestions: ['1', '0.75', '0.5', '0.25', '0'], placeholder: '0 to 1' }),
  P('Effects', 'box', 'text', 'transform', 'Transform', { suggestions: ['translateY(-2px)', 'scale(1.02)', 'rotate(-2deg)', 'none'] }),
  P('Effects', 'surface', 'text', 'filter', 'Filter', { suggestions: ['grayscale(1)', 'blur(2px)', 'brightness(1.1)', 'none'] }),
  P('Effects', 'surface', 'text', 'backdrop-filter', 'Backdrop', { suggestions: ['blur(8px)', 'none'] }),
  P('Effects', 'box', 'text', 'transition', 'Transition', { suggestions: ['all 150ms ease', 'all 300ms ease-out', 'none'] }),
  P('Effects', 'box', 'choice', 'cursor', 'Pointer', { options: o(['auto', 'Auto'], ['pointer', 'Hand'], ['not-allowed', 'Blocked'], ['grab', 'Grab']) }),
  P('Effects', 'box', 'switch', 'hidden', 'Hide'),
]

export const styleProp: Record<string, StyleProp> = Object.fromEntries(styleProps.map((p) => [p.key, p]))
export const styleGroups = [...new Set(styleProps.map((p) => p.group))]

// clean keeps a value from closing its rule or the style element.
const clean = (s: string) => s.replace(/[;{}<>\\]/g, '').replace(/\/\*|\*\//g, '').slice(0, 1000).trim()
const cleanCss = (s: string) => s.replace(/[{}<>\\]/g, '').replace(/\/\*|\*\//g, '').replace(/@import/gi, '').slice(0, 4000).trim()

const isTrue = (v: string) => ['true', 'yes', '1', 'on'].includes(v.toLowerCase())

// cssValue turns a resolved value into CSS: bare numbers get px, a number
// of grid tracks becomes equal columns, a picture address becomes url().
export function cssValue(key: string, raw: unknown): string {
  if (raw === null || raw === undefined || raw === false) return ''
  let v = clean(String(raw))
  if (!v) return ''
  const p = styleProp[key]
  const num = /^-?\d+(\.\d+)?$/.test(v)
  if (num && p?.px && v !== '0') return `${v}px`
  if (key === 'grid-template-columns' || key === 'grid-template-rows') {
    if (/^\d+$/.test(v)) return Number(v) > 0 ? `repeat(${Math.min(Number(v), 24)}, minmax(0, 1fr))` : ''
  }
  if ((key === 'grid-column' || key === 'grid-row') && /^\d+$/.test(v)) return `span ${v}`
  if (key === 'box-shadow' && shadows[v]) return shadows[v]
  if (key === 'background-image' && /^(https?:\/\/|\/|data:image\/)/.test(v) && !v.includes('(')) return `url("${v.replaceAll('"', '%22')}")`
  if (key === 'opacity' && /^\d+(\.\d+)?%$/.test(v)) return String(Number(v.slice(0, -1)) / 100)
  return v
}

// effective merges the layers that apply on a device: phones use the
// tablet values too.
export function effective(style: Style | undefined, device: string | undefined): StyleMap {
  if (!style) return {}
  return { ...style.base, ...(device === 'tablet' || device === 'phone' ? style.tablet : {}), ...(device === 'phone' ? style.phone : {}) }
}

export interface Selectors { box: string; surface: string; layout: string }

// selectorsFor names the elements of one component on the page. inner
// says the component lays out its children in an inner element marked
// with the layout class; extra marks a surface outside the box, such as a
// dialog drawn over the page.
export function selectorsFor(uid: string, inner = false): Selectors {
  const box = `.bbs-${uid}`
  const surface = `${box} > :not(.bb-tag):not(style), .bbf-${uid}`
  return { box, surface, layout: inner ? `.bbl-${uid}` : surface }
}

const stateSel: Record<string, string> = { hover: ':hover', focus: ':focus-within', active: ':active' }

// stateSelectors puts a state on the box, so hovering anywhere on the
// component styles all of it.
function stateSelectors(s: Selectors, state: string): Selectors {
  const pc = stateSel[state]
  const on = (sel: string) => sel.split(',').map((x) => x.trim()).map((x) => x.startsWith(s.box) ? s.box + pc + x.slice(s.box.length) : `${s.box}${pc} ${x}`).join(', ')
  return { box: s.box + pc, surface: on(s.surface), layout: on(s.layout) }
}

export interface StyleOptions {
  device?: string
  // resolve fills {{ bindings }}.
  resolve?: (v: string) => unknown
  // editing keeps hidden components on the canvas, faded.
  editing?: boolean
}

// rules writes one set of values as CSS rules.
function rules(map: StyleMap, sel: Selectors, opt: StyleOptions, css?: string): string {
  const out: Record<StyleTarget, string[]> = { box: [], surface: [], layout: [] }
  const res = (v: string) => (opt.resolve && v.includes('{{') ? opt.resolve(v) : v)
  // Shorthands go first so the sides set after them win.
  const keys = Object.keys(map).sort((a, b) => (a.includes('-') && a.split('-')[0] in map ? 1 : 0) - (b.includes('-') && b.split('-')[0] in map ? 1 : 0))
  for (const key of keys) {
    const p = styleProp[key]
    if (!p) continue
    let v: string
    try { v = cssValue(key, res(map[key])) } catch { continue }
    if (!v) continue
    if (key === 'hidden') {
      if (isTrue(v)) out.box.push(opt.editing ? 'opacity: 0.35; outline: 1px dashed var(--muted-foreground)' : 'display: none')
      continue
    }
    out[p.target].push(`${key}: ${v}`)
    if (key === 'transition') out.surface.push(`transition: ${v}`)
    // The component fills a box given a size.
    if (key === 'width') out.surface.push('width: 100%')
    if (key === 'height') out.surface.push('height: 100%')
    if (key === 'min-height') out.surface.push('min-height: inherit')
  }
  if (css) {
    let c: string
    try { c = cleanCss(String(res(css) ?? '')) } catch { c = '' }
    if (c) out.surface.push(c.replace(/;+\s*$/, ''))
  }
  const block = (s: string, decls: string[]) => (decls.length ? `${s} { ${decls.join('; ')} }\n` : '')
  // When layout and surface are one element, their values share a rule.
  if (sel.layout === sel.surface) return block(sel.box, out.box) + block(sel.surface, [...out.surface, ...out.layout])
  return block(sel.box, out.box) + block(sel.surface, out.surface) + block(sel.layout, out.layout)
}

// styleCss writes a component's styles as CSS for the given selectors.
export function styleCss(style: Style | undefined, sel: Selectors, opt: StyleOptions = {}): string {
  if (!style) return ''
  let css = rules(effective(style, opt.device), sel, opt, style.css)
  for (const state of ['hover', 'focus', 'active'] as const) {
    const m = style[state]
    if (m && Object.keys(m).length) css += rules(m, stateSelectors(sel, state), opt)
  }
  return css
}

// hasStyle says whether a style sets anything.
export const hasStyle = (s: Style | undefined) => !!s && (!!s.css?.trim() || layers.some((l) => s[l] && Object.keys(s[l]!).length))

// countSet counts the values set in a layer, by group.
export function countSet(map: StyleMap | undefined, group: string): number {
  return Object.keys(map ?? {}).filter((k) => styleProp[k]?.group === group && map![k] !== '').length
}

// setStyle returns a style with one value changed; "" removes it, and
// empty layers go away.
export function setStyle(style: Style | undefined, layer: StyleLayer, key: string, value: string | undefined): Style | undefined {
  const next: Style = { ...(style ?? {}) }
  const map = { ...(next[layer] ?? {}) }
  if (value === undefined || value === '') delete map[key]
  else map[key] = value
  if (Object.keys(map).length) next[layer] = map
  else delete next[layer]
  return hasStyle(next) ? next : undefined
}
