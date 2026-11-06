import { test } from 'node:test'
import assert from 'node:assert/strict'
import { componentPath, componentRef, componentScope, nameComponents, renameRefs } from '../src/lib/screen/components.ts'
import { parsePath, walk } from '../src/lib/paths.ts'
import { runFormula } from '../src/lib/screen/formulas.ts'

const lookup = (scope: any, path: string) => walk(scope, parsePath(path)!).value

const labels = { text_input: 'Text field', table: 'Table', container: 'Container', form: 'Form' }
const node = (id: string, type: string, props: Record<string, any> = {}, children: any[] = [], name?: string) => ({ id, type, props, children, name })

test('components that share values get unique names', () => {
  const root = node('root', 'container', {}, [
    node('a', 'text_input'),
    node('b', 'text_input', {}, [], 'Search'),
    node('c', 'text_input', {}, [], 'Search'), // a copy
    node('d', 'container'),
    node('e', 'table'),
  ])
  assert.equal(nameComponents(root as any, labels), true)
  assert.deepEqual(root.children.map((n: any) => n.name), ['Text field 1', 'Search', 'Text field 2', undefined, 'Table 1'])
  assert.equal(nameComponents(root as any, labels), false)
})

test('a name being typed is left alone, and others too', () => {
  const root = node('root', 'container', {}, [node('a', 'text_input', {}, [], 'Search'), node('b', 'text_input', {}, [], 'Search')])
  nameComponents(root as any, labels, 'b')
  assert.deepEqual(root.children.map((n: any) => n.name), ['Search', 'Search'])
})

test('renaming rewrites bindings in every form', () => {
  const root = node('root', 'container', {}, [
    node('t', 'table', { filter: '[Name] = "{{ components["Search box"].value }}"', params: { q: '{{ components.Search box.value | upper }}' }, showWhen: 'components.[Search box].value <> ""' }),
    node('u', 'text', { text: '{{ components["Search box 2"].value }}' }),
  ])
  const n = renameRefs(root as any, 'Search box', 'Find')
  assert.equal(n, 3)
  const t = root.children[0].props
  assert.equal(t.filter, '[Name] = "{{ components.Find.value }}"')
  assert.equal(t.params.q, '{{ components.Find.value | upper }}')
  assert.equal(t.showWhen, 'components.[Find].value <> ""')
  assert.equal(root.children[1].props.text, '{{ components["Search box 2"].value }}')
  renameRefs(root as any, 'Find', 'Find it')
  assert.equal(t.filter, '[Name] = "{{ components["Find it"].value }}"')
})

test('component scope reads live values by name and by id', () => {
  const root = node('root', 'container', {}, [node('a', 'text_input', {}, [], 'Search box'), node('e', 'table', {}, [], 'Orders')])
  let typed = 'ab'
  const scope = { components: componentScope(root as any, (id) => (id === 'a' ? { value: typed } : undefined)) }
  assert.equal(lookup(scope, componentRef('Search box', 'value')), 'ab')
  typed = 'abc'
  assert.equal(lookup(scope, 'components["Search box"].value'), 'abc')
  assert.equal(lookup(scope, 'components.a.value'), 'abc')
  // Not on the screen: the empty shape.
  assert.deepEqual(lookup(scope, 'components.Orders.rows'), [])
  assert.equal(componentPath('components["Search box"].value'), 'Search box')
})

test('formulas read component values', () => {
  const root = node('root', 'container', {}, [node('a', 'text_input', {}, [], 'Search box')])
  const scope: any = { components: componentScope(root as any, () => ({ value: 'kim' })) }
  assert.equal(runFormula('UPPER(components.[Search box].value)', scope), 'KIM')
})
