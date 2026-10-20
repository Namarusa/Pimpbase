<script lang="ts">
  import { untrack } from 'svelte'
  import FileTextIcon from '@lucide/svelte/icons/file-text'
  import FileXIcon from '@lucide/svelte/icons/file-x'
  import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left'
  import ChevronRightIcon from '@lucide/svelte/icons/chevron-right'
  import ZoomInIcon from '@lucide/svelte/icons/zoom-in'
  import ZoomOutIcon from '@lucide/svelte/icons/zoom-out'
  import MoveHorizontalIcon from '@lucide/svelte/icons/move-horizontal'
  import DownloadIcon from '@lucide/svelte/icons/download'
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link'
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import { getRuntime, getScope } from '../context.svelte'
  import { toText } from '../bindings'
  import { downloadURL, openPDF, pdfSources, type PDFDocumentProxy } from '../pdf'
  import type { Node } from '../types'

  // PdfViewer shows a PDF from an attachment field of the current row or
  // from a web address, page by page, with paging, zoom and download.
  let { p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const scope = getScope()

  const sources = $derived(pdfSources(p.source === 'url' ? p.url : p.field ? (scope().row?.[p.field] ?? scope().form?.[p.field]) : null))
  let chosen = $state(0)
  const src = $derived(sources[Math.min(chosen, sources.length - 1)])

  let doc = $state.raw<PDFDocumentProxy | null>(null)
  let sizes = $state.raw<{ w: number; h: number }[]>([])
  let loading = $state(false)
  let failed = $state('')
  let scale = $state(1)
  let fit = $state(true)
  let current = $state(1)
  let box: HTMLDivElement | undefined = $state()
  let width = $state(0)
  const pageEls: HTMLDivElement[] = $state([])
  const canvases: HTMLCanvasElement[] = $state([])

  // Load the document whenever the address changes.
  $effect(() => {
    const url = src?.url
    doc = null
    sizes = []
    failed = ''
    current = 1
    if (!url) return
    let gone = false
    let cancel = () => {}
    loading = true
    openPDF(url).then(async (r) => {
      cancel = r.cancel
      if (gone) return r.cancel()
      const out: { w: number; h: number }[] = []
      for (let i = 1; i <= Math.min(r.doc.numPages, 500); i++) {
        const vp = (await r.doc.getPage(i)).getViewport({ scale: 1 })
        out.push({ w: vp.width, h: vp.height })
      }
      if (gone) return
      sizes = out
      doc = r.doc
      fit = untrack(() => p.zoom ?? 'fit') === 'fit'
      if (!fit) scale = Number(untrack(() => p.zoom)) / 100 || 1
    }).catch((e) => {
      if (!gone) failed = e?.name === 'MissingPDFException' || /404|Missing/.test(String(e?.message)) ? 'The file could not be found.' : 'This PDF could not be opened here.'
    }).finally(() => { if (!gone) loading = false })
    return () => { gone = true; cancel() }
  })

  // Fit the widest page to the frame.
  $effect(() => {
    if (!fit || !sizes.length || !width) return
    const widest = Math.max(...sizes.map((s) => s.w))
    scale = Math.max(0.25, Math.min(4, (width - 32) / widest))
  })

  // Draw pages as they scroll into view, again when the zoom changes.
  const drawn = new Map<number, number>()
  const tasks = new Map<number, { cancel: () => void }>()
  async function draw(i: number) {
    const d = doc, s = scale, canvas = canvases[i]
    if (!d || !canvas || drawn.get(i) === s) return
    drawn.set(i, s)
    tasks.get(i)?.cancel()
    const page = await d.getPage(i + 1)
    const dpr = Math.min(window.devicePixelRatio || 1, 2)
    const vp = page.getViewport({ scale: s * dpr })
    const off = document.createElement('canvas')
    off.width = Math.ceil(vp.width)
    off.height = Math.ceil(vp.height)
    const task = page.render({ canvas: off, viewport: vp })
    tasks.set(i, task)
    try {
      await task.promise
      if (drawn.get(i) !== s) return
      canvas.width = off.width
      canvas.height = off.height
      canvas.getContext('2d')?.drawImage(off, 0, 0)
    } catch {
      if (drawn.get(i) === s) drawn.delete(i)
    } finally {
      if (tasks.get(i) === task) tasks.delete(i)
    }
  }

  $effect(() => {
    const d = doc, root = box
    scale
    if (!d || !root) return
    drawn.clear()
    const io = new IntersectionObserver((entries) => {
      for (const e of entries) if (e.isIntersecting) draw(Number((e.target as HTMLElement).dataset.page))
    }, { root, rootMargin: '400px 0px' })
    for (const el of pageEls) if (el) io.observe(el)
    return () => io.disconnect()
  })

  function onScroll() {
    if (!box) return
    const mark = box.scrollTop + box.clientHeight / 3
    let n = 1
    for (let i = 0; i < pageEls.length; i++) if (pageEls[i] && pageEls[i].offsetTop <= mark) n = i + 1
    current = n
  }

  function goTo(n: number) {
    n = Math.max(1, Math.min(sizes.length, n))
    const el = pageEls[n - 1]
    if (box && el) box.scrollTo({ top: el.offsetTop - 16, behavior: 'smooth' })
    current = n
  }

  function zoom(by: number) {
    fit = false
    scale = Math.max(0.25, Math.min(4, Math.round((scale + by) * 4) / 4))
  }

  const heights: Record<string, string> = { sm: 'h-80', md: 'h-[480px]', lg: 'h-[640px]', xl: 'h-[800px]', screen: 'h-[80vh]' }
  const height = $derived(heights[p.height] ?? heights.lg)
  const title = $derived(toText(p.title) || src?.name || 'Document')
</script>

<div class="bg-card text-card-foreground flex flex-col overflow-hidden rounded-xl border shadow-xs">
  {#if p.toolbar !== false}
    <div class="flex flex-wrap items-center gap-1 border-b px-2 py-1.5 print:hidden">
      <div class="flex min-w-0 flex-1 items-center gap-2 px-1">
        <FileTextIcon class="text-primary size-4 shrink-0" />
        {#if sources.length > 1}
          <select class="hover:bg-muted min-w-0 max-w-full truncate rounded-md bg-transparent px-1 py-1 text-sm font-medium" bind:value={chosen} aria-label="Document">
            {#each sources as s, i}<option value={i}>{s.name}</option>{/each}
          </select>
        {:else}
          <span class="truncate text-sm font-medium" title={title}>{title}</span>
        {/if}
      </div>
      {#if doc}
        <div class="flex items-center">
          <Button variant="ghost" size="icon" class="size-8" disabled={current <= 1} onclick={() => goTo(current - 1)} aria-label="Previous page"><ChevronLeftIcon /></Button>
          <span class="text-muted-foreground min-w-16 text-center text-xs tabular-nums">{current} / {sizes.length}</span>
          <Button variant="ghost" size="icon" class="size-8" disabled={current >= sizes.length} onclick={() => goTo(current + 1)} aria-label="Next page"><ChevronRightIcon /></Button>
        </div>
        <div class="flex items-center max-sm:hidden">
          <Button variant="ghost" size="icon" class="size-8" onclick={() => zoom(-0.25)} aria-label="Zoom out"><ZoomOutIcon /></Button>
          <span class="text-muted-foreground w-11 text-center text-xs tabular-nums">{Math.round(scale * 100)}%</span>
          <Button variant="ghost" size="icon" class="size-8" onclick={() => zoom(0.25)} aria-label="Zoom in"><ZoomInIcon /></Button>
          <Button variant={fit ? 'secondary' : 'ghost'} size="icon" class="size-8" onclick={() => (fit = true)} aria-label="Fit to width" title="Fit to width"><MoveHorizontalIcon /></Button>
        </div>
      {/if}
      {#if src}
        <div class="flex items-center">
          {#if p.allowDownload !== false}
            <Button variant="ghost" size="icon" class="size-8" href={downloadURL(src.url)} download={src.name} aria-label="Download" title="Download"><DownloadIcon /></Button>
          {/if}
          <Button variant="ghost" size="icon" class="size-8" href={src.url} target="_blank" rel="noopener" aria-label="Open in a new tab" title="Open in a new tab"><ExternalLinkIcon /></Button>
        </div>
      {/if}
    </div>
  {/if}

  <div bind:this={box} bind:clientWidth={width} onscroll={onScroll} class={['bg-muted/60 relative overflow-auto print:h-auto print:overflow-visible', height]}>
    {#if doc}
      <div class="flex min-w-fit flex-col items-center gap-4 p-4">
        {#each sizes as s, i (i)}
          <div bind:this={pageEls[i]} data-page={i} class="bg-white shadow-md ring-1 ring-black/5" style:width="{Math.floor(s.w * scale)}px" style:height="{Math.floor(s.h * scale)}px">
            <canvas bind:this={canvases[i]} class="block size-full" aria-label={`Page ${i + 1}`}></canvas>
          </div>
        {/each}
      </div>
    {:else}
      <div class="text-muted-foreground absolute inset-0 grid place-items-center p-6 text-center text-sm">
        {#if loading}
          <span class="flex items-center gap-2"><Spinner />Opening {src?.name ?? 'the PDF'}…</span>
        {:else if failed}
          <div class="flex flex-col items-center gap-2">
            <FileXIcon class="size-8 opacity-60" />
            <p>{failed}</p>
            {#if src}<Button variant="outline" size="sm" href={src.url} target="_blank" rel="noopener"><ExternalLinkIcon />Open it in a new tab</Button>{/if}
          </div>
        {:else}
          <div class="flex flex-col items-center gap-2">
            <FileTextIcon class="size-8 opacity-50" />
            <p>{rt.editing && !p.field && !p.url ? 'Choose an attachment field or a PDF address in the settings.' : toText(p.emptyText) || 'No PDF to show.'}</p>
          </div>
        {/if}
      </div>
    {/if}
  </div>
</div>
