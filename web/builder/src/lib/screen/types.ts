// The shapes of screens, components and their settings.
import type { Component } from 'svelte'
import type { Field, FieldType, Role, Table } from '../api'

// Node is one component placed on a screen.
export interface Node {
  id: string
  type: string
  // name labels the component in the builder's tree.
  name?: string
  props: Record<string, any>
  children: Node[]
}

export interface Screen {
  id: string
  appId: string
  name: string
  route: string
  role: Role
  position: number
  definition: Node
}

export interface Design {
  theme: 'light' | 'dark' | 'system'
  accent: string
  navigation: 'sidebar' | 'top' | 'none'
  title: string
}

// Action is one step run when a button is clicked, a row is chosen or a
// form is saved. String values may hold {{ bindings }}.
export interface Action {
  type: string
  [key: string]: any
}

export type SettingKind =
  | 'text' // one line; may hold bindings
  | 'textarea'
  | 'number'
  | 'boolean'
  | 'select'
  | 'segmented'
  | 'table'
  | 'field'
  | 'fields'
  | 'color'
  | 'icon'
  | 'actions'
  | 'filter'
  | 'options'
  | 'condition'
  | 'dialog'

export interface Setting {
  key: string
  label: string
  kind: SettingKind
  section?: string
  options?: { value: string; label: string }[]
  placeholder?: string
  help?: string
  // fieldTypes limits field pickers to these types.
  fieldTypes?: FieldType[]
  // queries lets a table setting pick an API query's rows too.
  queries?: boolean
  // show hides the setting unless it returns true.
  show?: (props: Record<string, any>) => boolean
}

export type Category = 'Layout' | 'Data' | 'Form' | 'Display' | 'Feedback'

export interface ComponentDef {
  type: string
  label: string
  category: Category
  icon: Component<any>
  description: string
  // container components hold children.
  container?: boolean
  // parents limits where the component may be placed.
  parents?: string[]
  // childTypes limits what may be placed inside.
  childTypes?: string[]
  // tableSource says the component reads rows (for field pickers): either
  // from its own table setting or from the data provider around it.
  tableSource?: boolean
  defaults: () => Record<string, any>
  children?: () => Node[]
  // wrapperClass adds classes to the box around the component.
  wrapperClass?: (props: Record<string, any>) => string
  settings: Setting[]
  component: Component<any>
}

// Scope is what {{ bindings }} can read at a point on the screen.
export interface Scope {
  user: { id?: string; name?: string; email?: string } | null
  app: { id: string; name: string; slug: string }
  url: Record<string, string>
  query: Record<string, string>
  state: Record<string, any>
  data?: { rows: any[]; total: number; loading: boolean; table?: Table }
  row?: Record<string, any>
  index?: number
  form?: Record<string, any>
  result?: any
  [key: string]: any
}

// TableContext is the table a component's field pickers offer.
export interface TableContext {
  table: Table
  fields: Field[]
}
