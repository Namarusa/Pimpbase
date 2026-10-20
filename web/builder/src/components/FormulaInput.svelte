<script lang="ts">
  import { api, type FormulaError, type FunctionInfo } from '../lib/api'
  import FormulaHelp from '../lib/ai/FormulaHelp.svelte'
  import { isBuilder } from '../lib/session.svelte'

  let {
    value = $bindable(''), app, table, isStatic = false, placeholder = '', multiline = false,
    onSubmit, onCheck,
  }: {
    value: string; app: string; table: string; isStatic?: boolean; placeholder?: string; multiline?: boolean
    onSubmit?: () => void; onCheck?: (ok: boolean, type: string) => void
  } = $props()

  let errors = $state<FormulaError[]>([])
  let type = $state('')
  let showFns = $state(false)
  let fns = $state<FunctionInfo[]>([])
  let fnQuery = $state('')
  let el = $state<HTMLInputElement | HTMLTextAreaElement>()
  let timer: ReturnType<typeof setTimeout>

  $effect(() => {
    const src = value
    const s = isStatic
    clearTimeout(timer)
    if (!src.trim()) { errors = []; type = ''; onCheck?.(true, ''); return }
    timer = setTimeout(async () => {
      try {
        const res = await api.checkFormula(app, table, src, s)
        if (src !== value) return
        errors = res.errors
        type = res.errors.length ? '' : res.type
        onCheck?.(res.errors.length === 0, type)
      } catch { /* the server reports problems on save */ }
    }, 250)
  })

  async function toggleFns() {
    showFns = !showFns
    if (showFns && fns.length === 0) fns = await api.functions()
  }

  function insert(text: string) {
    const start = el?.selectionStart ?? value.length
    const end = el?.selectionEnd ?? value.length
    value = value.slice(0, start) + text + value.slice(end)
    showFns = false
    queueMicrotask(() => { el?.focus(); el?.setSelectionRange(start + text.length - 1, start + text.length - 1) })
  }

  const filtered = $derived(fns.filter((f) => !fnQuery || f.name.includes(fnQuery.toUpperCase()) || f.category.toLowerCase().includes(fnQuery.toLowerCase())))
  const err = $derived(errors[0])
</script>

<div class="formula">
  <div class="row">
    <span class="fx">ƒ</span>
    {#if multiline}
      <textarea bind:this={el} bind:value rows="3" class="mono" class:bad={err} {placeholder} spellcheck="false"></textarea>
    {:else}
      <input bind:this={el} bind:value class="mono" class:bad={err} {placeholder} spellcheck="false"
        onkeydown={(e) => e.key === 'Enter' && !err && onSubmit?.()} />
    {/if}
    <button type="button" class="small" onclick={toggleFns} title="Insert a function">Functions</button>
    {#if isBuilder()}<FormulaHelp {app} {table} purpose={multiline ? 'field' : 'filter'} current={value} onUse={(f) => (value = f)} label="AI" />{/if}
  </div>
  {#if err}
    <div class="msg bad-msg">
      <code>{value.slice(0, err.start)}<mark>{value.slice(err.start, Math.max(err.end, err.start + 1)) || ' '}</mark>{value.slice(Math.max(err.end, err.start + 1))}</code>
      <div>{err.message}</div>
    </div>
  {:else if type}
    <div class="msg muted">Gives a <strong>{type}</strong></div>
  {/if}
  {#if showFns}
    <div class="fns card">
      <input placeholder="Search functions" bind:value={fnQuery} />
      <div class="list">
        {#each filtered as f (f.name)}
          <button type="button" class="fn" onclick={() => insert(f.name + '()')}>
            <code>{f.signature}</code><span class="muted">{f.description}</span>
          </button>
        {/each}
      </div>
    </div>
  {/if}
</div>

<style>
  .formula { position: relative; flex: 1; }
  .fx { color: var(--primary); font-style: italic; font-weight: 600; width: 14px; text-align: center; }
  .bad { border-color: var(--danger); }
  .msg { font-size: 12px; margin: 4px 0 0 22px; }
  .bad-msg { color: var(--danger); }
  .bad-msg code { color: var(--text); white-space: pre-wrap; }
  mark { background: var(--danger-soft); color: var(--danger); text-decoration: underline wavy; }
  .fns { position: absolute; right: 0; top: 40px; z-index: 30; width: 420px; max-width: 90vw; padding: 8px; box-shadow: var(--shadow); }
  .list { max-height: 300px; overflow: auto; margin-top: 6px; }
  .fn { display: flex; flex-direction: column; align-items: flex-start; gap: 2px; width: 100%; border: none; background: none; padding: 6px 8px; white-space: normal; text-align: left; }
  .fn:hover { background: var(--hover); }
  .fn span { font-size: 12px; }
</style>
