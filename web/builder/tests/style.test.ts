// Checks how component styles become CSS. Run with: npm test
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { cssValue, effective, selectorsFor, setStyle, styleCss } from '../src/lib/screen/style.ts'
import { themeScope, themeVars } from '../src/lib/screen/theme.ts'
import { applyConditions } from '../src/lib/screen/formulas.ts'

const sel = selectorsFor('u1')
const inner = selectorsFor('u1', true)

test('values become CSS', () => {
  assert.equal(cssValue('width', '120'), '120px')
  assert.equal(cssValue('width', '0'), '0')
  assert.equal(cssValue('line-height', '1.5'), '1.5')
  assert.equal(cssValue('grid-template-columns', '3'), 'repeat(3, minmax(0, 1fr))')
  assert.equal(cssValue('grid-template-columns', '240px 1fr'), '240px 1fr')
  assert.equal(cssValue('grid-column', '2'), 'span 2')
  assert.equal(cssValue('box-shadow', 'none'), 'none')
  assert.match(cssValue('box-shadow', 'lg'), /rgb/)
  assert.equal(cssValue('background-image', 'https://x.test/a b.png'), 'url("https://x.test/a b.png")')
  assert.equal(cssValue('background-image', 'linear-gradient(red, blue)'), 'linear-gradient(red, blue)')
  assert.equal(cssValue('opacity', '50%'), '0.5')
  assert.equal(cssValue('color', ''), '')
  assert.equal(cssValue('color', null), '')
})

test('values cannot leave their rule', () => {
  assert.equal(cssValue('color', 'red; } body { display: none'), 'red  body  display: none')
  assert.equal(cssValue('color', 'red</style><script>'), 'red/stylescript')
  const css = styleCss({ css: 'color: red } .x { color: blue; <b>' }, sel)
  assert.ok(!css.includes('.x {') && !css.includes('<'))
})

test('box, surface and layout values go to their elements', () => {
  const css = styleCss({ base: { margin: '8', padding: '16', display: 'grid', 'grid-template-columns': '2', color: 'red' } }, inner)
  assert.match(css, /^\.bbs-u1 \{ margin: 8px \}/)
  assert.match(css, /\.bbs-u1 > :not\(\.bb-tag\):not\(style\), \.bbf-u1 \{ padding: 16px; color: red \}/)
  assert.match(css, /\.bbl-u1 \{ display: grid; grid-template-columns: repeat\(2, minmax\(0, 1fr\)\) \}/)
  // Without an inner layout element, layout and surface share a rule.
  assert.match(styleCss({ base: { gap: '4', padding: '2' } }, sel), /\.bbf-u1 \{ padding: 2px; gap: 4px \}/)
})

test('sides come after their shorthand', () => {
  const css = styleCss({ base: { 'padding-top': '0', padding: '16' } }, sel)
  assert.match(css, /padding: 16px; padding-top: 0/)
})

test('a sized box makes the component fill it', () => {
  const css = styleCss({ base: { width: '200', height: '50%' } }, sel)
  assert.match(css, /\.bbs-u1 \{ width: 200px; height: 50% \}/)
  assert.match(css, /width: 100%; height: 100%/)
})

test('devices layer their values', () => {
  const style = { base: { color: 'red', gap: '8' }, tablet: { color: 'blue' }, phone: { gap: '2' } }
  assert.deepEqual(effective(style, 'desktop'), { color: 'red', gap: '8' })
  assert.deepEqual(effective(style, 'tablet'), { color: 'blue', gap: '8' })
  assert.deepEqual(effective(style, 'phone'), { color: 'blue', gap: '2' })
  assert.match(styleCss({ phone: { hidden: 'true' } }, sel, { device: 'phone' }), /display: none/)
  assert.equal(styleCss({ phone: { hidden: 'true' } }, sel, { device: 'desktop' }), '')
  // The canvas keeps hidden components, faded.
  assert.match(styleCss({ base: { hidden: 'yes' } }, sel, { editing: true }), /opacity: 0\.35/)
})

test('states get their own rules', () => {
  const css = styleCss({ hover: { 'background-color': 'var(--muted)', transform: 'scale(1.02)' }, active: { opacity: '0.8' } }, inner)
  assert.match(css, /\.bbs-u1:hover \{ transform: scale\(1\.02\) \}/)
  assert.match(css, /\.bbs-u1:hover > :not\(\.bb-tag\):not\(style\), \.bbs-u1:hover \.bbf-u1 \{ background-color: var\(--muted\) \}/)
  assert.match(css, /\.bbs-u1:active \{ opacity: 0\.8 \}/)
})

test('bindings choose values, and empty ones are left out', () => {
  const scope: Record<string, any> = { row: { Late: true, Size: 3 }, theme: themeScope({ tokens: [{ name: 'brand', value: '#0f766e' }] }) }
  const resolve = (v: string) => v.replace(/\{\{\s*([^}]+?)\s*\}\}/g, (_, p) => String(p.split('.').reduce((o: any, k: string) => o?.[k], scope) ?? ''))
  const css = styleCss({ base: { color: '{{ theme.brand }}', 'grid-template-columns': '{{ row.Size }}', 'border-color': '{{ row.Missing }}' } }, sel, { resolve })
  assert.match(css, /color: var\(--t-brand\)/)
  assert.match(css, /repeat\(3, minmax\(0, 1fr\)\)/)
  assert.ok(!css.includes('border-color'))
  // A failing binding drops the value rather than the page.
  assert.equal(styleCss({ base: { color: '{{ x }}' } }, sel, { resolve: () => { throw new Error('no') } }), '')
})

test('setStyle adds and removes values', () => {
  let s = setStyle(undefined, 'base', 'color', 'red')
  assert.deepEqual(s, { base: { color: 'red' } })
  s = setStyle(s, 'hover', 'color', 'blue')
  s = setStyle(s, 'base', 'color', '')
  assert.deepEqual(s, { hover: { color: 'blue' } })
  assert.equal(setStyle(s, 'hover', 'color', ''), undefined)
})

test('theme tokens become CSS variables', () => {
  const vars = themeVars({ radius: 'lg', font: 'mono', tokens: [{ name: 'brand', value: '#0f766e' }, { name: 'bad-name', value: 'red' }, { name: 'gutter', value: '12px; color: red' }] })
  assert.equal(vars['--radius'], '0.875rem')
  assert.match(vars['--font-sans'], /monospace/)
  assert.equal(vars['--t-brand'], '#0f766e')
  assert.equal(vars['--t-bad-name'], undefined)
  assert.equal(vars['--t-gutter'], '12px color: red')
  assert.equal(themeScope({}).primaryForeground, 'var(--primary-foreground)')
  assert.equal(themeScope({}).chart2, 'var(--chart-2)')
})

test('conditions change style values', () => {
  const props = { style: { base: { color: 'red', padding: '4' } }, conditions: [{ when: 'row.Late', set: { 'style:color': 'blue', text: 'Late' } }] }
  const out = applyConditions(props, props.conditions, { row: { Late: true } } as any)
  assert.deepEqual(out.style.base, { color: 'blue', padding: '4' })
  assert.equal(out.text, 'Late')
  assert.deepEqual(props.style.base, { color: 'red', padding: '4' })
})
