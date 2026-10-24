<script lang="ts">
  import * as ToggleGroup from '$lib/components/ui/toggle-group'
  import MonitorIcon from '@lucide/svelte/icons/monitor'
  import SmartphoneIcon from '@lucide/svelte/icons/smartphone'
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert'
  import LoaderCircleIcon from '@lucide/svelte/icons/loader-circle'

  // EmailPreview shows a rendered email as a mail app would: the inbox
  // line (sender, subject, preview text), then the body at desktop or
  // phone width, or its plain-text version. With selectable, a click on
  // a block of the body selects it (the server tags blocks with
  // data-pb-block in editor previews).
  let { html = '', text = '', subject = '', preheader = '', from = '', to = '', warnings = [], error = '', loading = false,
    selected = $bindable(''), selectable = false, compact = false, badge = '' }: {
    html?: string; text?: string; subject?: string; preheader?: string; from?: string; to?: string; warnings?: string[]; error?: string
    loading?: boolean; selected?: string; selectable?: boolean; compact?: boolean; badge?: string
  } = $props()
  // "Name" <a@b.c> reads as Name <a@b.c>.
  const sender = $derived(from.replace(/^"([^"]*)"/, '$1'))
  let width = $state<'desktop' | 'phone'>('desktop')
  let view = $state<'email' | 'text'>('email')
  let frame = $state<HTMLIFrameElement | null>(null)
  let height = $state(480)

  const marks = `<style>[data-pb-block]{cursor:pointer;border-radius:4px;transition:outline-color .1s}[data-pb-block]:hover{outline:1px dashed #93c5fd;outline-offset:3px}[data-pb-block].pb-on{outline:2px solid #2563eb;outline-offset:3px}</style>`
  const doc = $derived(selectable ? html.replace('</head>', marks + '</head>') : html)

  function fit() {
    const d = frame?.contentDocument
    if (!d?.body) return
    height = Math.max(compact ? 200 : 360, d.documentElement.scrollHeight)
  }
  function wire() {
    const d = frame?.contentDocument
    if (!d) return
    fit()
    d.addEventListener('click', (e) => {
      const a = (e.target as HTMLElement).closest('a')
      if (a) e.preventDefault()
      if (!selectable) return
      const el = (e.target as HTMLElement).closest('[data-pb-block]') as HTMLElement | null
      if (el) selected = el.dataset.pbBlock ?? ''
    })
    mark()
    // Images load late and change the height.
    d.querySelectorAll('img').forEach((img) => img.addEventListener('load', fit))
  }
  function mark() {
    const d = frame?.contentDocument
    if (!d) return
    d.querySelectorAll('[data-pb-block]').forEach((el) => el.classList.toggle('pb-on', (el as HTMLElement).dataset.pbBlock === selected))
  }
  $effect(() => { void selected; mark() })
  $effect(() => { void width; requestAnimationFrame(fit) })
  // A text-only email opens on its text.
  $effect(() => { if (!html && text) view = 'text' })
</script>

<div class={['flex min-h-0 flex-col', compact ? 'gap-2' : 'gap-3']}>
  <div class="bg-card overflow-hidden rounded-lg border">
    <div class="flex items-start gap-3 px-4 py-3">
      <div class="bg-primary/10 text-primary grid size-9 shrink-0 place-items-center rounded-full text-sm font-semibold">{(sender || 'P').replace(/^"?([^"<]).*$/, '$1').toUpperCase()}</div>
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2 text-sm">
          <span class="truncate font-medium">{sender || 'Your sender'}</span>
          {#if badge}<span class="bg-muted text-muted-foreground rounded px-1.5 py-0.5 text-[10px] font-medium tracking-wide uppercase">{badge}</span>{/if}
          {#if loading}<LoaderCircleIcon class="text-muted-foreground ml-auto size-3.5 animate-spin" />{/if}
        </div>
        <div class="truncate text-sm font-semibold" data-testid="email-subject">{subject || '(no subject)'}</div>
        {#if preheader}<div class="text-muted-foreground truncate text-xs">{preheader}</div>{/if}
        {#if to}<div class="text-muted-foreground mt-0.5 truncate text-xs">to {to}</div>{/if}
      </div>
    </div>
  </div>
  {#if error}
    <div class="border-destructive/40 bg-destructive/5 text-destructive rounded-lg border px-3 py-2 text-sm">{error}</div>
  {/if}
  {#if warnings.length}
    <div class="flex items-start gap-2 rounded-lg border border-amber-500/40 bg-amber-500/8 px-3 py-2 text-xs text-amber-800 dark:text-amber-300">
      <TriangleAlertIcon class="mt-0.5 size-3.5 shrink-0" /><span>{warnings.join('; ')}. Empty fields show as blanks.</span>
    </div>
  {/if}
  <div class="flex items-center gap-2">
    <ToggleGroup.Root type="single" size="sm" variant="outline" value={view} onValueChange={(v) => v && (view = v as typeof view)}>
      <ToggleGroup.Item value="email" class="px-3 text-xs">Email</ToggleGroup.Item>
      <ToggleGroup.Item value="text" class="px-3 text-xs">Plain text</ToggleGroup.Item>
    </ToggleGroup.Root>
    {#if view === 'email'}
      <ToggleGroup.Root type="single" size="sm" variant="outline" class="ml-auto" value={width} onValueChange={(v) => v && (width = v as typeof width)}>
        <ToggleGroup.Item value="desktop" aria-label="Desktop width"><MonitorIcon class="size-3.5" /></ToggleGroup.Item>
        <ToggleGroup.Item value="phone" aria-label="Phone width"><SmartphoneIcon class="size-3.5" /></ToggleGroup.Item>
      </ToggleGroup.Root>
    {/if}
  </div>
  {#if view === 'email'}
    <div class={['mx-auto w-full overflow-hidden rounded-lg border bg-white shadow-sm transition-[max-width]', width === 'phone' ? 'max-w-[390px]' : 'max-w-none']}>
      {#if html}
        <iframe bind:this={frame} title="Email preview" sandbox="allow-same-origin" srcdoc={doc} onload={wire} class="block w-full border-0" style={`height:${height}px`}></iframe>
      {:else}
        <div class="text-muted-foreground grid h-60 place-items-center text-sm">Nothing to show yet.</div>
      {/if}
    </div>
  {:else}
    <pre class="bg-card min-h-40 overflow-auto rounded-lg border p-4 font-mono text-xs leading-relaxed whitespace-pre-wrap">{text || '(empty)'}</pre>
  {/if}
</div>
