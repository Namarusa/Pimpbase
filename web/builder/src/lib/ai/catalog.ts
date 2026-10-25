// What the assistant may build with: the builder's own component and step
// registries, described for the model, plus example screens made by the
// builder's templates.
import { componentList, registry } from '../screen/registry'
import { styleProps } from '../screen/style'
import { actionTypes } from '../screen/actions'
import { stepTypes, triggerTypes } from '../flows/steps'
import { detailScreen, listScreen } from '../designer/templates'
import type { DraftKind, Table } from '../api'
import type { Node } from '../screen/types'

const options = (o?: { value: string }[]) => (o?.length ? o.map((x) => x.value) : undefined)

export function catalog(kind: DraftKind, tables: Table[]): any {
  if (kind === 'screen') {
    const sample = tables[0]
    return {
      components: componentList.map((c) => ({
        type: c.type, label: c.label, category: c.category, description: c.description,
        container: !!c.container, parents: c.parents, childTypes: c.childTypes,
        defaults: c.defaults(),
        settings: c.settings.map((s) => ({ key: s.key, kind: s.kind, label: s.label, options: options(s.options), help: s.help })),
      })),
      commonSettings: ['grow', 'span', 'roles', 'showWhen', 'conditions', 'printHide', 'style'],
      // props.style holds CSS by layer; values may hold {{ bindings }},
      // such as {{ theme.primary }}.
      style: {
        layers: ['base', 'tablet', 'phone', 'hover', 'focus', 'active'],
        example: { base: { padding: '16px', 'border-radius': '12px', 'background-color': '{{ theme.muted }}' }, phone: { padding: '8px' }, hover: { 'box-shadow': 'md' } },
        properties: styleProps.map((p) => p.key),
      },
      // Drafts go to other screens by address; the editor shows it as the
      // screen it points at.
      actions: actionTypes.map((a) => ({ type: a.type, label: a.label, fields: a.type === 'navigate' ? [{ key: 'route', kind: 'text', label: 'Address' }] : a.fields.map((f) => ({ key: f.key, kind: f.kind, label: f.label })) })),
      examples: sample ? [listScreen(sample), detailScreen(sample)].map((s) => ({ ...s, definition: compact(s.definition) })) : [],
    }
  }
  if (kind === 'flow') {
    return {
      triggers: triggerTypes.map((t) => ({ type: t.type, description: t.description })),
      steps: stepTypes.map((s) => ({
        type: s.type, label: s.label, description: s.description,
        inputs: s.inputs.map((i) => ({ key: i.key, kind: i.kind, label: i.label, options: options(i.options), default: i.default, placeholder: i.placeholder, help: i.help })),
      })),
    }
  }
  return null
}

// compact drops default settings from example trees, to keep them short.
function compact(n: Node): any {
  const d = registry[n.type]?.defaults() ?? {}
  const props = Object.fromEntries(Object.entries(n.props).filter(([k, v]) => JSON.stringify(d[k]) !== JSON.stringify(v)))
  return { id: n.id, type: n.type, props, children: n.children.map(compact) }
}

// fillDefaults gives every component of a drafted tree its default
// settings and children lists, as the designer would.
export function fillDefaults(n: any): Node {
  const d = registry[n.type]
  return {
    id: String(n.id), type: n.type, ...(n.name ? { name: n.name } : {}),
    props: { ...(d?.defaults() ?? {}), ...(n.props ?? {}) },
    children: (n.children ?? []).map(fillDefaults),
  }
}
