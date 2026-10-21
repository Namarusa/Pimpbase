<script lang="ts">
  import { api, type App, type ImportPlan } from '../lib/api'
  import Modal from './Modal.svelte'

  // ImportApp reads a .bubiapp file: as a new app, or, with target, as a
  // new version of that app. It always shows the plan first.
  let { target, onClose, onDone }: { target?: App; onClose: () => void; onDone: (plan: ImportPlan) => void } = $props()

  let file = $state<File | null>(null)
  let name = $state('')
  let prune = $state(false)
  let enableFlows = $state(false)
  let plan = $state<ImportPlan | null>(null)
  let error = $state('')
  let busy = $state(false)

  const opts = () => ({ target: target?.id, prune, enableFlows, name: target ? undefined : name.trim() || undefined })

  async function preview() {
    if (!file) return
    error = ''
    busy = true
    try {
      plan = await api.importApp(file, { ...opts(), dryRun: true })
    } catch (e: any) {
      error = e.message
      plan = null
    } finally {
      busy = false
    }
  }

  async function apply() {
    if (!file) return
    error = ''
    busy = true
    try {
      onDone(await api.importApp(file, opts()))
    } catch (e: any) {
      error = e.message
    } finally {
      busy = false
    }
  }

  const visible = $derived((plan?.changes ?? []).filter((c) => c.action !== 'keep'))
  const kept = $derived((plan?.changes ?? []).filter((c) => c.action === 'keep'))
  const actionLabel: Record<string, string> = { add: 'Add', change: 'Change', remove: 'Remove', keep: 'Keep' }
</script>

<Modal title={target ? `Update ${target.name} from a file` : 'Import an app'} {onClose} wide>
  {#if error}<p class="error">{error}</p>{/if}
  <div class="field">
    <label for="imp-file">App file (.bubiapp)</label>
    <input id="imp-file" type="file" accept=".bubiapp,.zip,application/zip"
      onchange={(e) => { file = (e.currentTarget as HTMLInputElement).files?.[0] ?? null; plan = null; preview() }} />
  </div>
  {#if !target}
    <div class="field"><label for="imp-name">Name of the new app</label><input id="imp-name" bind:value={name} placeholder={plan?.manifest.app.name ?? "Keep the file's name"} onchange={() => plan && preview()} /></div>
    <label class="check"><input type="checkbox" bind:checked={enableFlows} /><span>Switch on flows that were on in the file <span class="muted">(off by default, so a copy never sends a schedule's emails twice)</span></span></label>
  {:else}
    <label class="check"><input type="checkbox" bind:checked={prune} onchange={() => plan && preview()} /><span>Also remove tables, fields, queries, data sources and flows the file does not have</span></label>
    <p class="muted small">Rows, secret values and variables already set here are kept. Screens and the design are replaced by the file's.</p>
  {/if}

  {#if busy && !plan}<p class="muted">Reading the file…</p>{/if}
  {#if plan}
    <div class="plan card">
      <div class="row">
        <strong class="grow">{plan.manifest.app.name}</strong>
        <span class="muted small">exported {new Date(plan.manifest.exportedAt).toLocaleString()}{plan.manifest.server ? ` from Pimpbase ${plan.manifest.server}` : ''}</span>
      </div>
      <p class="muted small">
        {plan.manifest.tables} {plan.manifest.tables === 1 ? 'table' : 'tables'}{plan.manifest.data ? `, ${plan.manifest.rows.toLocaleString()} rows and ${plan.manifest.files} files` : ', no rows'}{plan.manifest.secrets ? ', with secrets' : ', secrets left out'}.
        {#if plan.mode === 'new'}Becomes the app <span class="mono">/{plan.app.slug}</span>.{/if}
      </p>
      {#each plan.warnings as w}<p class="warn small">{w}</p>{/each}
      {#if plan.mode === 'update'}
        {#if visible.length === 0}
          <p class="muted">The file holds nothing new for this app.</p>
        {:else}
          <ul class="changes">
            {#each visible as c}
              <li><span class={['tag', c.action]}>{actionLabel[c.action]}</span> <span class="kind">{c.kind}</span> <strong>{c.name}</strong>{#if c.detail}<span class="muted detail">{c.detail}</span>{/if}</li>
            {/each}
          </ul>
        {/if}
        {#if kept.length}<p class="muted small">{kept.length} kept as they are here: {kept.map((c) => c.name).join(', ')}.</p>{/if}
      {/if}
    </div>
  {/if}
  {#snippet footer()}
    <button onclick={onClose}>Cancel</button>
    <button class="primary" disabled={!plan || busy} onclick={apply}>{target ? 'Apply these changes' : 'Create app'}</button>
  {/snippet}
</Modal>

<style>
  .check { display: flex; gap: 8px; align-items: baseline; color: var(--text); font-size: 14px; margin-bottom: 12px; }
  .small { font-size: 12.5px; }
  .plan { padding: 12px 14px; margin: 8px 0 4px; display: grid; gap: 6px; }
  .changes { list-style: none; margin: 4px 0 0; padding: 0; display: grid; gap: 4px; max-height: 260px; overflow: auto; }
  .changes li { font-size: 13px; }
  .kind { color: var(--muted-foreground); }
  .detail::before { content: '·'; margin: 0 6px; }
  .tag { display: inline-block; min-width: 56px; text-align: center; font-size: 11px; font-weight: 600; padding: 1px 6px; border-radius: 99px; background: var(--hover); }
  .tag.add { background: color-mix(in oklch, var(--ok) 15%, transparent); color: var(--ok); }
  .tag.change { background: var(--accent-soft); color: var(--primary); }
  .tag.remove { background: var(--danger-soft); color: var(--danger); }
  .warn { color: oklch(0.55 0.14 70); }
</style>
