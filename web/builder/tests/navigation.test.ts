// Checks how Go to screen actions build addresses and what a screen
// receives. Run with: npm test
import { test } from 'node:test'
import assert from 'node:assert/strict'
import { buildPath, incoming, navTarget, parseAddress } from '../src/lib/screen/navigation.ts'

const screens = [
  { id: 'list', name: 'Orders', route: '/orders', definition: { id: 'root', type: 'root', props: {}, children: [
    { id: 't', type: 'table', props: { onRowClick: [{ type: 'navigate', screen: 'detail', params: { id: '{{ row._id }}' }, query: [{ key: 'tab', value: 'notes' }, { key: '', value: 'x' }] }] }, children: [] },
  ] } },
  { id: 'detail', name: 'Order', route: '/orders/:id', definition: { id: 'root', type: 'root', props: {}, children: [
    { id: 'b', type: 'button', props: { actions: [{ type: 'navigate', route: '/orders?status={{ row.Status }}' }] }, children: [] },
  ] } },
  { id: 'new', name: 'New order', route: '/orders/new', definition: { id: 'root', type: 'root', props: {}, children: [] } },
]

test('buildPath', () => {
  assert.equal(buildPath('/orders/:id', { id: 'a b/c' }, [{ key: 'tab', value: 'n&1' }, { key: 'x', value: '' }]), '/orders/a%20b%2Fc?tab=n%261')
  assert.equal(buildPath('/', {}, []), '/')
  assert.equal(buildPath('/orders/:id', { id: '{{ row._id }}' }, [], (s) => s), '/orders/{{ row._id }}')
})

test('parseAddress', () => {
  assert.deepEqual(parseAddress('/orders/{{ row._id }}', screens), { screen: 'detail', params: { id: '{{ row._id }}' }, query: [] })
  // A fixed part wins over a :part.
  assert.equal(parseAddress('/orders/new', screens)?.screen, 'new')
  assert.deepEqual(parseAddress('/orders?status=open', screens), { screen: 'list', params: {}, query: [{ key: 'status', value: 'open' }] })
  assert.equal(parseAddress('/nowhere', screens), null)
  assert.equal(navTarget({ type: 'navigate', address: true, route: '/orders' }, screens), null)
})

test('incoming', () => {
  assert.deepEqual(incoming(screens[1], screens), [{ path: 'url.id', from: ['Orders'] }, { path: 'query.tab', from: ['Orders'] }])
  assert.deepEqual(incoming(screens[0], screens), [{ path: 'query.status', from: ['Order'] }])
})
