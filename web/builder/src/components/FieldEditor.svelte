<script lang="ts">
  import { api, fieldTypes, type Field, type FieldType, type Table } from '../lib/api'
  import Modal from './Modal.svelte'
  import FormulaInput from './FormulaInput.svelte'

  let { app, table, tables, field, onClose, onSaved }: {
    app: string; table: Table; tables: Table[]; field: Field | null
    onClose: () => void; onSaved: (t: Table) => void
  } = $props()

  const editing = field !== null
  let name = $state(field?.name ?? '')
  let type = $state<FieldType>(field?.type ?? 'text')
  let required = $state(field?.required ?? false)
  let options = $state((field?.options ?? []).join('\n'))
  let linkTable = $state(field?.linkTable ?? table.id)
  let linkMany = $state(field?.linkMany ?? false)
  let formula = $state(field?.formula ?? '')
  let formulaMode = $state<'dynamic' | 'static'>(field?.formulaMode ?? 'dynamic')
  let formulaOk = $state(true)
  let indexed = $state(field?.indexed ?? false)
  // Fields kept in their own column can have an index; links get one
  // by themselves.
  const indexable = $derived(['text', 'number', 'boolean', 'date', 'single_select', 'autonumber'].includes(type) || (type === 'formula' && formulaMode === 'static'))
  let error = $state('')
  let busy = $state(false)
  let lossy = $state(false)

  const scalar = ['text', 'longtext', 'number', 'boolean', 'date', 'single_select']
  const typeChoices = $derived(editing
    ? fieldTypes.filter((t) => t.type === field!.type || (scalar.includes(field!.type) && scalar.includes(t.type)))
    : fieldTypes)
  const optionList = $derived(options.split('\n').map((o) => o.trim()).filter(Boolean))

  async function save(force = false) {
    error = ''
    busy = true
    try {
      let t: Table
      if (!editing) {
        t = await api.addField(app, table.id, {
          name, type, required, options: optionList, linkTable, linkMany, formula, formulaMode, indexed: indexable && indexed,
        })
      } else {
        const p: Record<string, unknown> = { name, force }
        if (type !== field!.type) p.type = type
        if (required !== !!field!.required) p.required = required
        if (type === 'single_select' || type === 'multi_select') p.options = optionList
        if (type === 'formula') { p.formula = formula; p.formulaMode = formulaMode }
        if (indexable && indexed !== !!field!.indexed) p.indexed = indexed
        t = await api.updateField(app, table.id, field!.id, p)
      }
      onSaved(t)
    } catch (e: any) {
      error = e.message
      lossy = e.body?.field === 'type' && /force/.test(e.message)
    } finally {
      busy = false
    }
  }

  async function remove() {
    if (!confirm(`Delete the field ${field!.name} and all its values?`)) return
    try { onSaved(await api.deleteField(app, table.id, field!.id)) } catch (e: any) { error = e.message }
  }
</script>

<Modal title={editing ? `Edit ${field!.name}` : 'New field'} {onClose} wide={type === 'formula'}>
  <form id="field-form" onsubmit={(e) => { e.preventDefault(); save() }}>
    {#if error}<p class="error">{error}{#if lossy} <button type="button" class="small" onclick={() => save(true)}>Convert anyway</button>{/if}</p>{/if}
    <div class="row">
      <div class="field grow"><label for="f-name">Name</label><input id="f-name" bind:value={name} required /></div>
      <div class="field"><label for="f-type">Type</label>
        <select id="f-type" bind:value={type} disabled={typeChoices.length === 1}>
          {#each typeChoices as t}<option value={t.type}>{t.icon}  {t.label}</option>{/each}
        </select>
      </div>
    </div>
    {#if type === 'single_select' || type === 'multi_select'}
      <div class="field"><label for="f-opts">Options, one per line</label><textarea id="f-opts" rows="4" bind:value={options}></textarea></div>
    {/if}
    {#if type === 'link'}
      <div class="field"><label for="f-link">Links to</label>
        <select id="f-link" bind:value={linkTable} disabled={editing}>
          {#each tables as t}<option value={t.id}>{t.name}</option>{/each}
        </select>
      </div>
      <label class="check"><input type="checkbox" bind:checked={linkMany} disabled={editing} /> A row can link to many rows</label>
    {/if}
    {#if type === 'formula'}
      <div class="field">
        <label for="f-formula">Formula</label>
        <FormulaInput bind:value={formula} {app} table={table.id} isStatic={formulaMode === 'static'} multiline
          placeholder="[Quantity] * [Unit price]" onCheck={(ok) => (formulaOk = ok)} />
        <small class="muted">Refer to fields as [Field name]. Use Current.User.Email for the person viewing.</small>
      </div>
      <div class="field">
        <label for="f-mode">Computed</label>
        <select id="f-mode" bind:value={formulaMode}>
          <option value="dynamic">When read (always current)</option>
          <option value="static">When the row is saved (stored, fast to filter)</option>
        </select>
      </div>
    {/if}
    {#if type !== 'formula' && type !== 'autonumber'}
      <label class="check"><input type="checkbox" bind:checked={required} /> Required</label>
    {/if}
    {#if indexable}
      <label class="check" title="An index makes filters, sorts and row rules on this field fast on big tables, at a small cost to each save.">
        <input type="checkbox" bind:checked={indexed} /> Indexed <span class="muted">(fast filters and sorts on big tables)</span>
      </label>
    {/if}
  </form>
  {#snippet footer()}
    {#if editing}<button class="danger ghost" onclick={remove}>Delete field</button><div class="grow"></div>{/if}
    <button onclick={onClose}>Cancel</button>
    <button class="primary" form="field-form" disabled={busy || (type === 'formula' && (!formulaOk || !formula.trim()))}>
      {editing ? 'Save' : 'Add field'}
    </button>
  {/snippet}
</Modal>

<style>
  .check { display: flex; gap: 8px; align-items: center; color: var(--text); font-size: 14px; margin-bottom: 14px; }
  select { min-width: 170px; }
</style>
