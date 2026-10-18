// A tiny history router: the server returns the builder for every path.

export const route = $state({ path: location.pathname })

export function navigate(path: string) {
  if (path !== location.pathname) history.pushState(null, '', path)
  route.path = path
}

addEventListener('popstate', () => (route.path = location.pathname))

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
