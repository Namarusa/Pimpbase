// Binding paths, as the server reads them (internal/interp SplitPath):
// names joined by dots, with list positions and odd keys in brackets.
//
//   steps.Api.rows.0.name  =  steps.Api.rows[0].name  =  steps.Api.rows[0]["name"]
//   loop.item["first name"]
//
// formatPath writes the plain form back: dots where a key is a plain
// name or a position, brackets with quotes for anything else.

export function parsePath(src: string): string[] | null {
  const out: string[] = []
  let cur = ''
  const flush = () => { const t = cur.trim(); if (t) out.push(t); cur = '' }
  for (let i = 0; i < src.length; i++) {
    const c = src[i]
    if (c === '.') { flush(); continue }
    if (c !== '[') { cur += c; continue }
    flush()
    let j = i + 1
    while (src[j] === ' ') j++
    if (src[j] === '"' || src[j] === "'") {
      const q = src[j]
      let key = ''
      let k = j + 1
      for (; k < src.length && src[k] !== q; k++) {
        if (src[k] === '\\' && k + 1 < src.length) k++
        key += src[k]
      }
      if (k >= src.length) return null
      k++
      while (src[k] === ' ') k++
      if (src[k] !== ']') return null
      out.push(key)
      i = k
      continue
    }
    const end = src.indexOf(']', i)
    if (end < 0) return null
    const key = src.slice(i + 1, end).trim()
    if (!key) return null
    out.push(key)
    i = end
  }
  flush()
  return out
}

const plainKey = /^[A-Za-z_@#$][\w@#$-]*$|^\d+$/

export function formatPath(segs: string[]): string {
  let out = ''
  for (const s of segs) {
    if (plainKey.test(s)) out += (out ? '.' : '') + s
    else out += `[${JSON.stringify(s)}]`
  }
  return out
}

// walk follows a path the way the server does: map keys match exactly or
// else ignoring case, list items are numbered from 0. found is false when
// the path leaves the data.
export function walk(v: any, segs: string[]): { value: any; found: boolean } {
  let cur = v
  for (const key of segs) {
    if (Array.isArray(cur)) {
      if (!/^\d+$/.test(key) || Number(key) >= cur.length) return { value: undefined, found: false }
      cur = cur[Number(key)]
    } else if (cur !== null && typeof cur === 'object') {
      if (key in cur) cur = cur[key]
      else {
        const k = Object.keys(cur).find((x) => x.toLowerCase() === key.toLowerCase())
        if (k === undefined) return { value: undefined, found: false }
        cur = cur[k]
      }
    } else return { value: undefined, found: false }
  }
  return { value: cur, found: true }
}
