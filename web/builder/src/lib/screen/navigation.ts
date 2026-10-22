// A "Go to screen" action names a screen, fills the :parts of its route
// and adds extra values to the query string:
//   { type: 'navigate', screen: 'scr_…', params: { id: '{{ row._id }}' }, query: [{ key: 'tab', value: 'notes' }] }
// The target reads them as {{ url.id }} and {{ query.tab }}. Older actions
// hold a typed address instead ({ route: '/orders/{{ row._id }}' }).
import type { Action, Node } from './types'

export interface ScreenRef { id: string; name?: string; route: string; definition?: Node }
export interface QueryValue { key: string; value: string }
export interface NavTarget { screen: string; params: Record<string, string>; query: QueryValue[] }

// routeParams lists the :parts of a route.
export const routeParams = (route: string) => route.split('/').filter((s) => s.startsWith(':')).map((s) => s.slice(1))

// buildPath fills a route with values and adds the query string. enc
// encodes each value; the designer keeps bindings readable with none.
export function buildPath(route: string, params: Record<string, string>, query: QueryValue[], enc: (s: string) => string = encodeURIComponent): string {
  const path = route.split('/').map((s) => (s.startsWith(':') ? enc(params[s.slice(1)] ?? '') : s)).join('/')
  const qs = query.filter((q) => q.key && q.value !== '').map((q) => `${enc(q.key)}=${enc(q.value)}`).join('&')
  return (path || '/') + (qs ? `?${qs}` : '')
}

// parseAddress reads a typed address as a screen and its values, when it
// points at one of the screens: /orders/{{ row._id }}?tab=notes.
export function parseAddress(text: string | undefined, screens: ScreenRef[]): NavTarget | null {
  if (!text) return null
  const at = text.indexOf('?')
  const path = at < 0 ? text : text.slice(0, at)
  const segs = path.split('/').filter(Boolean)
  const query: QueryValue[] = []
  if (at >= 0) {
    for (const part of text.slice(at + 1).split('&')) {
      if (!part) continue
      const eq = part.indexOf('=')
      query.push({ key: eq < 0 ? part : part.slice(0, eq), value: eq < 0 ? '' : part.slice(eq + 1) })
    }
  }
  // A screen whose route matches with fixed parts wins over one that
  // matches through :parts only.
  let best: { s: ScreenRef; params: Record<string, string>; fixed: number } | null = null
  for (const s of screens) {
    const r = s.route.split('/').filter(Boolean)
    if (r.length !== segs.length) continue
    const params: Record<string, string> = {}
    let fixed = 0
    let ok = true
    for (let i = 0; i < r.length && ok; i++) {
      if (r[i].startsWith(':')) params[r[i].slice(1)] = segs[i]
      else if (r[i] === segs[i]) fixed++
      else ok = false
    }
    if (ok && (!best || fixed > best.fixed)) best = { s, params, fixed }
  }
  return best ? { screen: best.s.id, params: best.params, query } : null
}

// navTarget is what a navigate action points at, read from either form.
export function navTarget(a: Action, screens: ScreenRef[]): NavTarget | null {
  if (a.screen) return { screen: a.screen, params: a.params ?? {}, query: a.query ?? [] }
  if (a.address) return null
  return parseAddress(a.route, screens)
}

// actionsOf lists the actions held in a component tree.
export function actionsOf(root: Node | undefined): { node: Node; action: Action }[] {
  const out: { node: Node; action: Action }[] = []
  const visit = (n: Node) => {
    for (const v of Object.values(n.props ?? {})) {
      if (Array.isArray(v)) for (const a of v) if (a && typeof a === 'object' && typeof a.type === 'string') out.push({ node: n, action: a })
    }
    for (const c of n.children ?? []) visit(c)
  }
  if (root) visit(root)
  return out
}

// incoming lists the values other screens send to a screen: its route
// parts and the query string keys their Go to screen actions add.
export function incoming(target: ScreenRef, screens: ScreenRef[]): { path: string; from: string[] }[] {
  const out = routeParams(target.route).map((p) => ({ path: `url.${p}`, from: [] as string[] }))
  for (const s of screens) {
    for (const { action } of actionsOf(s.definition)) {
      if (action.type !== 'navigate') continue
      const t = navTarget(action, screens)
      if (!t || t.screen !== target.id) continue
      const keys = [...Object.keys(t.params).filter((k) => t.params[k]).map((k) => `url.${k}`), ...t.query.filter((q) => q.key).map((q) => `query.${q.key}`)]
      for (const k of keys) {
        let e = out.find((x) => x.path === k)
        if (!e) out.push((e = { path: k, from: [] }))
        const name = s.name || s.route
        if (!e.from.includes(name)) e.from.push(name)
      }
    }
  }
  return out
}
