// The designer's working state: the screen being edited, the selection,
// undo history and saving.
import { getContext, setContext } from 'svelte'
import { api, type App, type Flow, type Query, type Table } from '../api'
import { queryTable } from '../apis/bindings'
import type { Node, Screen } from '../screen/types'
import { findIn } from './tree'

export class Designer {
  app = $state<App>(null!)
  tables = $state<Table[]>([])
  queries = $state<Query[]>([])
  flows = $state<Flow[]>([])
  screens = $state<Screen[]>([])
  screen = $state<Screen | null>(null)
  selected = $state<string | null>('root')
  hovered = $state<string | null>(null)
  saving = $state<'saved' | 'saving' | 'error'>('saved')
  saveError = $state('')
  device = $state<'desktop' | 'tablet' | 'phone'>('desktop')
  private undoStack = $state<string[]>([])
  private redoStack = $state<string[]>([])
  private lastSnap = 0
  private timer: ReturnType<typeof setTimeout> | undefined

  get canUndo() { return this.undoStack.length > 0 }
  get canRedo() { return this.redoStack.length > 0 }

  get root(): Node | null { return this.screen?.definition ?? null }
  get node(): Node | null { return this.root && this.selected ? findIn(this.root, this.selected) : null }
  table(id?: string): Table | undefined {
    if (id?.startsWith('query:')) {
      const q = this.queries.find((x) => x.id === id.slice(6))
      return q ? queryTable(q, this.app?.id) : undefined
    }
    return this.tables.find((t) => t.id === id)
  }

  open(s: Screen | null) {
    this.flush()
    this.screen = s ? structuredClone($state.snapshot(s)) as Screen : null
    this.selected = 'root'
    this.undoStack = []
    this.redoStack = []
  }

  // change edits the tree. Quick successive edits, like typing, share one
  // undo step.
  change(fn: (root: Node) => void, coalesce = false) {
    if (!this.screen) return
    const now = Date.now()
    if (!coalesce || now - this.lastSnap > 800) {
      this.undoStack.push(JSON.stringify($state.snapshot(this.screen.definition)))
      if (this.undoStack.length > 100) this.undoStack.shift()
      this.redoStack = []
    }
    this.lastSnap = now
    fn(this.screen.definition)
    this.scheduleSave()
  }

  undo() {
    if (!this.screen || !this.undoStack.length) return
    this.redoStack.push(JSON.stringify($state.snapshot(this.screen.definition)))
    this.screen.definition = JSON.parse(this.undoStack.pop()!)
    this.lastSnap = 0
    if (this.selected && !findIn(this.screen.definition, this.selected)) this.selected = 'root'
    this.scheduleSave()
  }

  redo() {
    if (!this.screen || !this.redoStack.length) return
    this.undoStack.push(JSON.stringify($state.snapshot(this.screen.definition)))
    this.screen.definition = JSON.parse(this.redoStack.pop()!)
    this.lastSnap = 0
    this.scheduleSave()
  }

  scheduleSave() {
    this.saving = 'saving'
    clearTimeout(this.timer)
    this.timer = setTimeout(() => this.save(), 600)
  }

  flush() {
    if (this.timer) {
      clearTimeout(this.timer)
      this.timer = undefined
      this.save()
    }
  }

  async save() {
    this.timer = undefined
    const s = this.screen
    if (!s) return
    const definition = $state.snapshot(s.definition) as Node
    try {
      const saved = await api.updateScreen(this.app.id, s.id, { definition })
      this.screens = this.screens.map((x) => (x.id === saved.id ? saved : x))
      if (!this.timer) this.saving = 'saved'
      this.saveError = ''
    } catch (e: any) {
      this.saving = 'error'
      this.saveError = e.message
    }
  }

  // updateMeta saves a screen's name, route or role.
  async updateMeta(p: Partial<Pick<Screen, 'name' | 'route' | 'role'>>): Promise<string> {
    if (!this.screen) return ''
    try {
      const saved = await api.updateScreen(this.app.id, this.screen.id, p)
      this.screens = this.screens.map((x) => (x.id === saved.id ? saved : x))
      this.screen.name = saved.name
      this.screen.route = saved.route
      this.screen.role = saved.role
      return ''
    } catch (e: any) {
      return e.message
    }
  }
}

const key = Symbol('designer')
export const setDesigner = (d: Designer) => setContext(key, d)
export const getDesigner = () => getContext<Designer>(key)
