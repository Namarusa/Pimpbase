// Component values: what a component on the screen shares with the
// others, read as {{ components["Search box"].value }}. A search box's
// text can feed a table's filter, an API query's parameters or a heading.
//
// Components are found by their name in the tree, so every component that
// shares values gets a name ("Text field 1") when it is placed, and a
// rename rewrites the bindings that use the old name.
import { formatPath, parsePath } from '../paths.ts'
import type { Node } from './types'

// A Shared value is one key a component offers, with a hint for the
// binding picker.
export interface Shared { key: string; hint: string }

const inputKeys: Shared[] = [{ key: 'value', hint: 'what was typed or chosen' }]
const rowKeys: Shared[] = [{ key: 'rows', hint: 'list' }, { key: 'total', hint: 'rows matching' }, { key: 'loading', hint: 'yes while loading' }]

// shared lists the values each type of component offers.
export const shared: Record<string, Shared[]> = {
  text_input: inputKeys,
  textarea_input: inputKeys,
  number_input: inputKeys,
  select_input: inputKeys,
  radio_input: inputKeys,
  checkbox_group_input: inputKeys,
  date_input: inputKeys,
  checkbox_input: inputKeys,
  switch_input: inputKeys,
  slider_input: inputKeys,
  file_input: inputKeys,
  link_input: inputKeys,
  form: [{ key: 'values', hint: 'every field' }, { key: 'valid', hint: 'yes when nothing is wrong' }, { key: 'submitting', hint: 'yes while saving' }],
  data_provider: rowKeys,
  table: [...rowKeys, { key: 'search', hint: 'search box text' }, { key: 'selectedRows', hint: 'ticked rows' }, { key: 'selectedIds', hint: 'ticked row ids' }],
  list: rowKeys,
  cards: rowKeys,
  repeater: rowKeys,
  row_provider: [{ key: 'row', hint: 'the loaded row' }, { key: 'loading', hint: 'yes while loading' }],
  dialog: [{ key: 'open', hint: 'yes while open' }],
  side_panel: [{ key: 'open', hint: 'yes while open' }],
}

export const sharesValues = (type: string) => type in shared
export const isInput = (type: string) => type.endsWith('_input')

const walkTree = (n: Node, out: Node[] = []): Node[] => {
  out.push(n)
  for (const c of n.children) walkTree(c, out)
  return out
}

// nameComponents gives every component that shares values a name of its
// own: "Text field 1", "Text field 2". A copy keeps its original's name
// until it gets a number. keep is a component being renamed by hand, left
// as typed. It returns whether anything changed.
export function nameComponents(root: Node, labels: Record<string, string>, keep?: string | null): boolean {
  const nodes = walkTree(root).filter((n) => n.id !== 'root' && sharesValues(n.type))
  const taken = new Set<string>()
  let changed = false
  // Names typed by hand win over generated ones.
  for (const n of nodes) {
    const name = n.name?.trim()
    if (!name) continue
    if (!taken.has(name.toLowerCase()) || n.id === keep) taken.add(name.toLowerCase())
  }
  const seen = new Set<string>()
  for (const n of nodes) {
    const name = n.name?.trim()
    if (n.id === keep && name) { seen.add(name.toLowerCase()); continue }
    if (name && !seen.has(name.toLowerCase())) { seen.add(name.toLowerCase()); continue }
    const base = labels[n.type] ?? n.type
    let i = 1
    while (taken.has(`${base} ${i}`.toLowerCase())) i++
    n.name = `${base} ${i}`
    taken.add(n.name.toLowerCase())
    seen.add(n.name.toLowerCase())
    changed = true
  }
  return changed
}

// refPattern finds the start of a reference to a component by name:
// components.Name, components["Name"] or components.[Name] (formulas).
function refPattern(name: string): RegExp {
  const esc = name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  return new RegExp(`components(?:\\.${esc}(?=[.\\s|}\\[]|$)|\\[\\s*["']${esc}["']\\s*\\]|\\.\\[${esc}\\])`, 'g')
}

// componentRef writes the binding path of a component's value.
export const componentRef = (name: string, key?: string) => formatPath(key ? ['components', name, key] : ['components', name])

// renameRefs rewrites every setting that reads a component under its old
// name. It returns how many settings changed.
export function renameRefs(root: Node, from: string, to: string): number {
  if (!from || !to || from === to) return 0
  const re = refPattern(from)
  const plain = /^[A-Za-z_][\w]*$/.test(to)
  let count = 0
  const fix = (v: any): any => {
    if (typeof v === 'string') {
      if (!v.includes('components')) return v
      const out = v.replace(re, (m) => (m.startsWith('components.[') ? `components.[${to}]` : plain ? `components.${to}` : `components[${JSON.stringify(to)}]`))
      if (out !== v) count++
      return out
    }
    if (Array.isArray(v)) return v.map(fix)
    if (v && typeof v === 'object') {
      for (const k of Object.keys(v)) v[k] = fix(v[k])
      return v
    }
    return v
  }
  for (const n of walkTree(root)) n.props = fix(n.props)
  return count
}

// usesComponent says whether a text reads a component by name.
export function usesComponent(text: string, name: string): boolean {
  return refPattern(name).test(text)
}

// componentScope builds the "components" object bindings read. get
// returns a component's live values (by id); a component not on the
// screen right now (in a closed dialog, a hidden part) gives its empty
// shape, so bindings read nothing rather than fail.
export function componentScope(root: Node, get: (id: string) => Record<string, any> | undefined): Record<string, any> {
  const out: Record<string, any> = {}
  for (const n of walkTree(root)) {
    if (n.id === 'root' || !sharesValues(n.type)) continue
    const read = () => get(n.id) ?? empty(n.type)
    const name = n.name?.trim()
    if (name && !(name in out)) Object.defineProperty(out, name, { get: read, enumerable: true, configurable: true })
    if (!(n.id in out)) Object.defineProperty(out, n.id, { get: read, enumerable: false, configurable: true })
  }
  return out
}

function empty(type: string): Record<string, any> {
  const o: Record<string, any> = {}
  for (const s of shared[type] ?? []) o[s.key] = s.key === 'rows' || s.key === 'selectedRows' || s.key === 'selectedIds' ? [] : s.key === 'total' ? 0 : s.key === 'values' ? {} : null
  return o
}

// componentPath reads the name back from a binding path such as
// components["Search box"].value, or "" when it is not one.
export function componentPath(path: string): string {
  const segs = parsePath(path)
  return segs && segs[0] === 'components' && segs[1] ? segs[1] : ''
}
