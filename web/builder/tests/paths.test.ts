// Checks binding paths read as the server reads them (internal/interp
// SplitPath) and the previews the flow editor shows. Run with: npm test
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { formatPath, parsePath, walk } from '../src/lib/paths.ts'
import { preview } from '../src/lib/preview.ts'

test('paths', () => {
  assert.deepEqual(parsePath('steps.Api.rows.0.name'), ['steps', 'Api', 'rows', '0', 'name'])
  assert.deepEqual(parsePath('steps.Api.rows[0]["name"]'), ['steps', 'Api', 'rows', '0', 'name'])
  assert.deepEqual(parsePath("loop.item[ 'first name' ].x"), ['loop', 'item', 'first name', 'x'])
  assert.deepEqual(parsePath('row.First name'), ['row', 'First name'])
  assert.equal(parsePath('a[0'), null)
  assert.equal(parsePath('a["x]'), null)
  assert.equal(formatPath(['loop', 'item', 'first name', '0', '@id']), 'loop.item["first name"].0.@id')
  assert.deepEqual(walk({ A: { b: [1, 2] } }, ['a', 'b', '1']), { value: 2, found: true })
  assert.equal(walk({ a: [1] }, ['a', '3']).found, false)
})

test('previews', () => {
  const scope = { steps: { Api: { rows: [{ name: 'Ada', address: { city: 'Paris' } }] } }, loop: { item: { name: 'Ada' } }, trigger: { x: 1 } }
  assert.deepEqual(preview('{{ steps.Api.rows }}', scope).value, scope.steps.Api.rows)
  assert.equal(preview('Hi {{ loop.item.name | upper }}!', scope).text, 'Hi ADA!')
  assert.equal(preview('{{ steps.Api.rows[0].address.city }}', scope).value, 'Paris')
  assert.deepEqual(preview('{{ loop.item.nmae }}', scope).missing, ['loop.item.nmae'])
  // Steps that did not run in the sample are not flagged.
  assert.deepEqual(preview('{{ steps.Later.x }}', scope).missing, [])
  assert.equal(preview('{{ loop.item["name"] }} {{ loop.item.name }}', scope).text, 'Ada Ada')
  assert.equal(preview('{{ = 1 + 2 }}', scope).kind, 'formula')
  assert.equal(preview('{{#each steps.Api.rows}}x{{/each}}', scope).kind, 'blocks')
})
