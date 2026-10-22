// A tiny history router: the server returns the builder for every path.

// path is the address without its query string, which search holds
// ("?tab=notes", or "").
export const route = $state({ path: location.pathname, search: location.search })

export function navigate(to: string) {
  const at = to.indexOf('?')
  const path = at < 0 ? to : to.slice(0, at)
  const search = at < 0 || at === to.length - 1 ? '' : to.slice(at)
  if (path + search !== location.pathname + location.search) history.pushState(null, '', path + search)
  route.path = path
  route.search = search
}

addEventListener('popstate', () => {
  route.path = location.pathname
  route.search = location.search
})

// match returns the named parts of path for a pattern such as /apps/:app.
export function match(pattern: string, path: string): Record<string, string> | null {
  const p = pattern.split('/').filter(Boolean)
  const s = path.split('/').filter(Boolean)
  if (p.length !== s.length) return null
  const out: Record<string, string> = {}
  for (let i = 0; i < p.length; i++) {
    if (p[i].startsWith(':')) out[p[i].slice(1)] = decodeURIComponent(s[i])
    else if (p[i] !== s[i]) return null
  }
  return out
}
