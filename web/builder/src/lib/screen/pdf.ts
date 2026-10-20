/// <reference types="vite/client" />
// PDF rendering with pdf.js, loaded only when a screen shows a PDF. Browsers
// differ in how (and whether) they show PDFs in a frame, and phones mostly
// do not, so the viewer draws the pages itself.
// The legacy build carries polyfills for browsers a year or two old.
import type { PDFDocumentProxy } from 'pdfjs-dist/legacy/build/pdf.mjs'
import workerUrl from 'pdfjs-dist/legacy/build/pdf.worker.min.mjs?url'

let lib: Promise<typeof import('pdfjs-dist/legacy/build/pdf.mjs')> | null = null

function pdfjs() {
  lib ??= import('pdfjs-dist/legacy/build/pdf.mjs').then((m) => {
    m.GlobalWorkerOptions.workerSrc = workerUrl
    return m
  })
  return lib
}

export type { PDFDocumentProxy }

// openPDF loads a document. Files of this server are fetched with the
// person's session; other sites must allow cross-origin reads.
export async function openPDF(url: string): Promise<{ doc: PDFDocumentProxy; cancel: () => void }> {
  const m = await pdfjs()
  const sameOrigin = new URL(url, location.href).origin === location.origin
  const task = m.getDocument({ url, withCredentials: sameOrigin })
  return { doc: await task.promise, cancel: () => task.destroy() }
}

// FileLike is an attachment file or a bare address.
export interface PdfSource { url: string; name: string }

// pdfSources picks the PDFs out of a value: an attachment field's files, one
// file, or a web address.
export function pdfSources(v: any): PdfSource[] {
  if (v === null || v === undefined || v === '') return []
  if (Array.isArray(v)) return v.flatMap(pdfSources)
  if (typeof v === 'object') {
    if (typeof v.url !== 'string') return []
    const isPdf = v.type === 'application/pdf' || /\.pdf$/i.test(v.name ?? '') || /\.pdf($|[?#])/i.test(v.url)
    return isPdf ? [{ url: v.url, name: v.name || nameOf(v.url) }] : []
  }
  const url = String(v).trim()
  return /^(https?:\/\/|\/)/.test(url) ? [{ url, name: nameOf(url) }] : []
}

function nameOf(url: string) {
  try {
    const last = new URL(url, location.href).pathname.split('/').filter(Boolean).pop() ?? ''
    return decodeURIComponent(last) || 'Document'
  } catch {
    return 'Document'
  }
}

// downloadURL is the address that saves the file instead of showing it.
export function downloadURL(url: string) {
  if (!url.startsWith('/api/')) return url
  return url + (url.includes('?') ? '&' : '?') + 'download=1'
}
