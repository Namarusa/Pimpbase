import type { Field, LinkRef } from './api'

// display formats a cell value as text for the grid.
export function display(f: Field, v: any): string {
  if (v === null || v === undefined || v === '') return ''
  const t = f.type === 'formula' ? f.resultType : f.type
  switch (t) {
    case 'number':
    case 'autonumber':
      return typeof v === 'number' ? v.toLocaleString(undefined, { maximumFractionDigits: 10 }) : String(v)
    case 'date':
      return formatDate(String(v))
    case 'boolean':
      return v ? 'Yes' : 'No'
  }
  if (Array.isArray(v)) return v.map((x) => (typeof x === 'object' ? ((x as LinkRef).label ?? x.name) : String(x))).join(', ')
  if (typeof v === 'object' && 'label' in v) return (v as LinkRef).label
  return String(v)
}

export function formatDate(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const dateOnly = iso.endsWith('T00:00:00Z')
  return dateOnly
    ? d.toLocaleDateString(undefined, { timeZone: 'UTC', year: 'numeric', month: 'short', day: 'numeric' })
    : d.toLocaleString(undefined, { year: 'numeric', month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' })
}

// toDateInput turns a stored date into the value of an <input type="date">.
export const toDateInput = (v: any) => (typeof v === 'string' ? v.slice(0, 10) : '')

export const writable = (f: Field) => f.type !== 'formula' && f.type !== 'autonumber'

// fileSize formats a byte count, e.g. 1.2 MB.
export function fileSize(n: number): string {
  if (!n) return ''
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`
  return `${(n / 1024 / 1024).toFixed(1)} MB`
}

export const isPDF = (f: { type?: string; name?: string }) => f.type === 'application/pdf' || /\.pdf$/i.test(f.name ?? '')
