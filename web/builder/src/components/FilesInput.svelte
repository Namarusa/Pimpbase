<script lang="ts">
  import { api, type FileRef } from '../lib/api'
  import { fileSize } from '../lib/format'

  // FilesInput edits an attachment field: files upload as soon as they are
  // chosen and are kept when the row is saved.
  let { app, table, value = $bindable() }: { app: string; table: string; value: FileRef[] | null } = $props()

  let busy = $state(0)
  let error = $state('')

  async function add(list: FileList | null) {
    error = ''
    for (const file of list ?? []) {
      busy++
      try {
        const f = await api.uploadFile(app, table, file)
        value = [...(value ?? []), f]
      } catch (e: any) {
        error = `${file.name}: ${e.message}`
      } finally {
        busy--
      }
    }
  }

  const remove = (id: string) => (value = (value ?? []).filter((f) => f.id !== id))
</script>

<div class="files">
  {#each value ?? [] as f (f.id)}
    <div class="file">
      <a href={f.url} target="_blank" rel="noopener">{f.name}</a>
      <span class="muted small">{fileSize(f.size)}</span>
      <button type="button" class="ghost small" onclick={() => remove(f.id)} aria-label={`Remove ${f.name}`}>✕</button>
    </div>
  {/each}
  <label class="btn small add">
    {busy ? 'Uploading…' : '+ Add files'}
    <input type="file" multiple onchange={(e) => { add(e.currentTarget.files); e.currentTarget.value = '' }} />
  </label>
  {#if error}<p class="error small">{error}</p>{/if}
</div>

<style>
  .files { display: flex; flex-direction: column; gap: 4px; align-items: flex-start; }
  .file { display: flex; gap: 8px; align-items: center; }
  .add { position: relative; cursor: pointer; }
  .add input { position: absolute; inset: 0; opacity: 0; cursor: pointer; }
</style>
