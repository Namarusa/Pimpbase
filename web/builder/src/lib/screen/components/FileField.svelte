<script lang="ts">
  import UploadIcon from '@lucide/svelte/icons/upload'
  import FileTextIcon from '@lucide/svelte/icons/file-text'
  import FileIcon from '@lucide/svelte/icons/file'
  import XIcon from '@lucide/svelte/icons/x'
  import { Spinner } from '$lib/components/ui/spinner'
  import { api, type FileRef } from '../../api'
  import { fileSize, isPDF } from '../../format'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import { getRuntime } from '../context.svelte'
  import type { Node } from '../types'

  // FileField uploads files into an attachment field of the form's table.
  // Files go to the server as soon as they are chosen; the row keeps them
  // when the form is saved.
  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const f = new FieldBinding(node, () => p, 'attachment')
  const form = f.formState
  const files = $derived<FileRef[]>(Array.isArray(f.value) ? f.value : [])
  const accept = $derived(p.accept === 'pdf' ? 'application/pdf,.pdf' : p.accept === 'images' ? 'image/*' : '')
  const many = $derived(p.multiple !== false)
  let pending = $state<string[]>([])
  let error = $state('')
  let dragging = $state(false)
  let input: HTMLInputElement | undefined = $state()

  function allowed(file: File): string {
    if (p.accept === 'pdf' && !isPDF(file)) return `${file.name} is not a PDF`
    if (p.accept === 'images' && !file.type.startsWith('image/')) return `${file.name} is not a picture`
    const max = Number(p.maxSizeMB) || 25
    if (file.size > max * 1024 * 1024) return `${file.name} is larger than ${max} MB`
    return ''
  }

  async function add(list: FileList | File[] | null | undefined) {
    if (rt.editing || f.disabled) return
    const t = form?.table
    if (!t || !f.field) { error = 'Place this in a form and choose an attachment field.'; return }
    error = ''
    let chosen = [...(list ?? [])]
    if (!many) chosen = chosen.slice(0, 1)
    for (const file of chosen) {
      const why = allowed(file)
      if (why) { error = why; continue }
      pending = [...pending, file.name]
      form.uploading++
      try {
        const saved = await api.uploadFile(rt.appId, t.id, file)
        f.value = many ? [...(Array.isArray(f.value) ? f.value : []), saved] : [saved]
        f.touch()
      } catch (e: any) {
        error = `${file.name}: ${e.message}`
      } finally {
        form.uploading--
        pending = pending.filter((n) => n !== file.name)
      }
    }
  }

  function remove(id: string) {
    f.value = files.filter((x) => x.id !== id)
    f.touch()
  }

  const hint = $derived(p.accept === 'pdf' ? 'PDF' : p.accept === 'images' ? 'Pictures' : 'Any file')
</script>

<FieldShell {f} help={p.help}>
  <div
    role="button"
    tabindex="0"
    aria-disabled={f.disabled}
    class={['border-input hover:bg-muted/50 flex cursor-pointer flex-col items-center justify-center gap-1 rounded-lg border border-dashed px-4 py-6 text-center transition-colors',
      dragging ? 'border-primary bg-primary/5' : '', f.error ? 'border-destructive' : '', f.disabled ? 'pointer-events-none opacity-60' : '']}
    onclick={() => input?.click()}
    onkeydown={(e) => { if (e.key === 'Enter' || e.key === ' ') { e.preventDefault(); input?.click() } }}
    ondragover={(e) => { e.preventDefault(); dragging = true }}
    ondragleave={() => (dragging = false)}
    ondrop={(e) => { e.preventDefault(); dragging = false; add(e.dataTransfer?.files) }}
  >
    <UploadIcon class="text-muted-foreground size-5" />
    <span class="text-sm font-medium">{p.placeholder || (many ? 'Drop files here or click to choose' : 'Drop a file here or click to choose')}</span>
    <span class="text-muted-foreground text-xs">{hint}, up to {Number(p.maxSizeMB) || 25} MB{many ? ' each' : ''}</span>
    <input bind:this={input} id={f.id} type="file" class="hidden" {accept} multiple={many} onchange={(e) => { add(e.currentTarget.files); e.currentTarget.value = '' }} />
  </div>
  {#if files.length || pending.length}
    <ul class="flex flex-col gap-1.5">
      {#each files as file (file.id)}
        <li class="bg-muted/40 flex items-center gap-2 rounded-md border px-3 py-2 text-sm">
          {#if isPDF(file)}<FileTextIcon class="text-primary size-4 shrink-0" />{:else}<FileIcon class="text-muted-foreground size-4 shrink-0" />{/if}
          <a class="min-w-0 flex-1 truncate hover:underline" href={file.url} target="_blank" rel="noopener">{file.name}</a>
          <span class="text-muted-foreground shrink-0 text-xs">{fileSize(file.size)}</span>
          {#if !f.disabled}
            <button type="button" class="text-muted-foreground hover:text-destructive" onclick={() => remove(file.id)} aria-label={`Remove ${file.name}`}><XIcon class="size-4" /></button>
          {/if}
        </li>
      {/each}
      {#each pending as name}
        <li class="text-muted-foreground flex items-center gap-2 rounded-md border border-dashed px-3 py-2 text-sm"><Spinner class="size-4" />{name}</li>
      {/each}
    </ul>
  {/if}
  {#if error}<p class="text-destructive text-sm">{error}</p>{/if}
</FieldShell>
