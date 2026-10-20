// Formulas on screens: bindings, show-when rules and conditional settings.
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { applyConditions, conditionFormula, formulaName, formulaProblem, isTrue, runFormula } from '../src/lib/screen/formulas.ts'

const scope: any = {
  user: { id: 'usr_1', name: 'Kim', email: 'kim@example.com' },
  app: { id: 'app_1', name: 'Shop', slug: 'shop' },
  url: { id: '7' }, query: {}, state: { search: 'desk', count: 3 },
  row: { _id: 7, Item: 'Desk', 'Unit price': 40, Qty: 2, Due: '2026-10-01T00:00:00Z', Customer: { _id: 1, label: 'Ada' }, Tags: ['rush', 'gift'], Secret: { _id: 2, label: '', hidden: true } },
  form: { Type: 'Business', 'First name': 'Lena' },
  data: { rows: [{ a: 1 }, { a: 2 }], total: 2 },
  device: 'phone',
}

test('formulas read the scope', () => {
  assert.equal(runFormula('[Qty] * [Unit price]', scope), 80)
  assert.equal(runFormula('row.Qty + state.count', scope), 5)
  assert.equal(runFormula('UPPER(user.name) & "!"', scope), 'KIM!')
  assert.equal(runFormula('form.[First name]', scope), 'Lena')
  assert.equal(runFormula('[Customer]', scope), 'Ada')
  assert.equal(runFormula('[Secret]', scope), '')
  assert.equal(runFormula('JOIN([Tags], "+")', scope), 'rush+gift')
  assert.equal(runFormula('url.id = [_id]', scope), true)
  assert.equal(runFormula('data.total', scope), 2)
  assert.equal(runFormula('DATEADD([Due], 1, "day")', scope), '2026-10-02T00:00:00Z')
  assert.equal(runFormula('FORMATDATE([Due], "D MMM")', scope), '1 Oct')
  assert.equal(runFormula('[Nope]', scope), null)
  assert.equal(runFormula('device = "phone"', scope), true)
  assert.equal(runFormula('[item]', scope), 'Desk')
})

test('problems are reported, not thrown, in the designer', () => {
  assert.equal(formulaProblem(''), null)
  assert.equal(formulaProblem('[Qty] > 1'), null)
  assert.match(formulaProblem('nobody.x')!.message, /unknown name nobody\.x/)
  assert.match(formulaProblem('SUMOF([Lines], [Qty])')!.message, /only table formulas/)
  assert.match(formulaProblem('[Customer].[City]')!.message, /cannot be read here/)
  assert.match(formulaProblem('IF(')!.message, /ends too early/)
})

test('show-when rules', () => {
  assert.equal(isTrue(undefined, scope), true)
  assert.equal(isTrue('form.Type = "Business"', scope), true)
  assert.equal(isTrue('form.Type = "Home"', scope), false)
  assert.equal(isTrue('VALUE("x") > 1', scope), false) // fails: hidden
  assert.equal(isTrue('nobody.x', scope), false)
  assert.equal(isTrue('[Tags]', scope), true)
})

test('conditional settings apply in order and never touch access', () => {
  const props = { text: 'Open', variant: 'default', roles: ['ADMIN'] }
  const out = applyConditions(props, [
    { when: '[Qty] > 1', set: { variant: 'destructive', text: 'Bulk' } },
    { when: 'FALSE', set: { text: 'Never' } },
    { when: 'TRUE', set: { text: 'Last wins', roles: [] } },
  ], scope)
  assert.deepEqual(out, { text: 'Last wins', variant: 'destructive', roles: ['ADMIN'] })
  assert.equal(applyConditions(props, undefined, scope), props)
})

test('older show-only-when rules become formulas', () => {
  assert.equal(conditionFormula({ left: '{{ user.email }}', op: 'not_empty' }), 'NOT ISBLANK(user.email)')
  assert.equal(conditionFormula({ left: '{{ row.First name }}', op: 'equals', right: 'Lena' }), 'row.[First name] = "Lena"')
  assert.equal(conditionFormula({ left: '{{ row.Qty }}', op: 'gt', right: '1' }), 'row.Qty > 1')
  assert.equal(conditionFormula({ left: 'Hi {{ user.name }}', op: 'equals', right: 'x' }), '')
  for (const [c, want] of [
    [{ left: '{{ row.Qty }}', op: 'gt', right: '1' }, true],
    [{ left: '{{ row.Item }}', op: 'contains', right: 'ES' }, true],
    [{ left: '{{ state.none }}', op: 'empty' }, true],
    [{ left: '{{ form.Type }}', op: 'not_equals', right: 'Business' }, false],
  ] as const) assert.equal(isTrue(conditionFormula(c as any), scope), want, JSON.stringify(c))
  assert.equal(formulaName('row.Unit price | currency'), 'row.[Unit price]')
})
