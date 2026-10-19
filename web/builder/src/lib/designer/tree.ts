// Edits of a screen's component tree. Every function changes the tree in
// place; the designer snapshots it first for undo.
import { registry, newId } from '../screen/registry'
import type { Node } from '../screen/types'

export function parentOf(root: Node, id: string): Node | null {
  for (const c of root.children) {
    if (c.id === id) return root
    const p = parentOf(c, id)
    if (p) return p
  }
  return null
}

export function contains(node: Node, id: string): boolean {
  return node.id === id || node.children.some((c) => contains(c, id))
}

// accepts says whether a child of this type may go inside parent.
export function accepts(parent: Node, type: string): boolean {
  const pd = registry[parent.type]
  const cd = registry[type]
  if (!pd?.container || !cd) return false
  if (pd.childTypes && !pd.childTypes.includes(type)) return false
  if (cd.parents && !cd.parents.includes(parent.type)) return false
  return true
}

// insertAt puts a node into parent at index.
export function insertAt(parent: Node, node: Node, index: number) {
  parent.children.splice(Math.max(0, Math.min(index, parent.children.length)), 0, node)
}

// place finds where a new component goes relative to the selection: inside
// a selected container, else after the selected component.
export function place(root: Node, selected: string | null, type: string): { parent: Node; index: number } | null {
  const target = (selected && findIn(root, selected)) || root
  if (accepts(target, type)) return { parent: target, index: target.children.length }
  let child = target
  let parent = parentOf(root, child.id)
  while (parent) {
    if (accepts(parent, type)) return { parent, index: parent.children.indexOf(child) + 1 }
    child = parent
    parent = parentOf(root, child.id)
  }
  return accepts(root, type) ? { parent: root, index: root.children.length } : null
}

export function findIn(root: Node, id: string): Node | null {
  if (root.id === id) return root
  for (const c of root.children) {
    const f = findIn(c, id)
    if (f) return f
  }
  return null
}

export function remove(root: Node, id: string): Node | null {
  const parent = parentOf(root, id)
  if (!parent) return null
  const i = parent.children.findIndex((c) => c.id === id)
  const [gone] = parent.children.splice(i, 1)
  return gone
}

// clone copies a node with fresh ids.
export function clone(node: Node): Node {
  return { ...structuredClone(node), id: newId(node.type), children: node.children.map(clone) }
}

export function duplicate(root: Node, id: string): Node | null {
  const parent = parentOf(root, id)
  if (!parent) return null
  const i = parent.children.findIndex((c) => c.id === id)
  const copy = clone(parent.children[i])
  parent.children.splice(i + 1, 0, copy)
  return copy
}

// move puts a node before, after or inside another one.
export function move(root: Node, id: string, targetId: string, where: 'before' | 'after' | 'inside'): boolean {
  if (id === targetId || id === root.id) return false
  const node = findIn(root, id)
  const target = findIn(root, targetId)
  if (!node || !target || contains(node, targetId)) return false
  if (where === 'inside') {
    if (!accepts(target, node.type)) return false
    remove(root, id)
    target.children.push(node)
    return true
  }
  const parent = parentOf(root, targetId)
  if (!parent || !accepts(parent, node.type)) return false
  remove(root, id)
  const i = parent.children.findIndex((c) => c.id === targetId)
  parent.children.splice(where === 'before' ? i : i + 1, 0, node)
  return true
}

export function shift(root: Node, id: string, by: -1 | 1) {
  const parent = parentOf(root, id)
  if (!parent) return
  const i = parent.children.findIndex((c) => c.id === id)
  const j = i + by
  if (j < 0 || j >= parent.children.length) return
  const [n] = parent.children.splice(i, 1)
  parent.children.splice(j, 0, n)
}

// all lists every node of a tree, depth first.
export function all(root: Node): Node[] {
  return [root, ...root.children.flatMap(all)]
}
