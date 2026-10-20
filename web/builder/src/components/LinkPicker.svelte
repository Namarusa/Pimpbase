<script lang="ts">
  import { linkLabel } from '../lib/format'
  import { api, type LinkRef, type Table } from '../lib/api'

  let { app, target, many, value = $bindable() }: {
    app: string; target: Table; many: boolean; value: LinkRef | LinkRef[] | null
  } = $props()

  let query = $state('')
  let results = $state<LinkRef[]>([])
  let error = $state('')
  const primary = $derived(target.fields.find((f) => f.id === target.primaryField))
  const chosen = $derived<LinkRef[]>(Array.isArray(value) ? value : value ? [value] : [])

  $effect(() => {
    const q = query.trim()
    const p = primary
    const handle = setTimeout(async () => {
      try {
        const filter = q && p ? `CONTAINS(LOWER(TEXT([${p.name}])), LOWER("${q.replaceAll('"', '""')}"))` : ''
        const page = await api.rows(app, target.id, { filter, limit: 20 })
        results = page.rows.map((r) => ({ _id: r._id, label: p ? String(r[p.name] ?? '') || `#${r._id}` : `#${r._id}` }))
        error = ''
      } catch (e: any) { error = e.message }
    }, 200)
    return () => clearTimeout(handle)
  })

  function toggle(r: LinkRef) {
    if (!many) { value = chosen[0]?._id === r._id ? null : r; return }
    value = chosen.some((c) => c._id === r._id) ? chosen.filter((c) => c._id !== r._id) : [...chosen, r]
  }
</script>

<div class="picker">
  <div class="chips">
    {#each chosen as c (c._id)}
      <span class="chip">{linkLabel(c)}<button type="button" class="ghost small" onclick={() => toggle(c)} aria-label="Remove">✕</button></span>
    {:else}
      <span class="muted">Nothing linked</span>
    {/each}
  </div>
  <input placeholder={`Search ${target.name}`} bind:value={query} />
  {#if error}<p class="error">{error}</p>{/if}
  <div class="results">
    {#each results as r (r._id)}
      <button type="button" class="ghost" class:on={chosen.some((c) => c._id === r._id)} onclick={() => toggle(r)}>
        <span class="tick">{chosen.some((c) => c._id === r._id) ? '✓' : ''}</span>{r.label}
      </button>
    {/each}
  </div>
</div>

<style>
  .chips { display: flex; flex-wrap: wrap; gap: 4px; margin-bottom: 8px; min-height: 26px; align-items: center; }
  .chip { display: inline-flex; align-items: center; background: var(--accent-soft); color: var(--primary); border-radius: 99px; padding: 0 0 0 10px; font-size: 13px; }
  .chip button { color: inherit; padding: 0 8px; }
  .results { max-height: 200px; overflow: auto; margin-top: 6px; border: 1px solid var(--line); border-radius: var(--radius); }
  .results button { width: 100%; border-radius: 0; justify-content: flex-start; }
  .results button.on { background: var(--accent-soft); }
  .tick { width: 14px; color: var(--primary); }
</style>
