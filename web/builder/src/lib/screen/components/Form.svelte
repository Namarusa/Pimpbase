<script lang="ts">
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import { Skeleton } from '$lib/components/ui/skeleton'
  import { api, rowKey } from '../../api'
  import { writable } from '../../format'
  import Children from '../Children.svelte'
  import { FormState, extendScope, getRuntime, getScope, setForm, shareValues } from '../context.svelte'
  import { runActions } from '../actions'
  import { gapClass } from '../styles'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const rt = getRuntime()
  const scope = getScope()
  const form = new FormState(rt.table(p.table))
  setForm(form)
  extendScope(() => ({ form: form.values }))
  $effect(() => { form.table = rt.table(p.table) })

  const rowId = $derived(rowKey(p.rowId))
  const updating = $derived(p.mode === 'update')
  let loading = $state(false)
  let loadError = $state('')
  let formError = $state('')

  // An update form loads its row, and reloads when the id changes.
  $effect(() => {
    const t = form.table
    const id = rowId
    if (!updating || !t || !id) return
    loading = true
    api.row(rt.appId, t.id, id).then((r) => {
      for (const f of t.fields) form.values[f.name] = structuredClone($state.snapshot(r[f.name]) ?? null)
      loadError = ''
    }).catch((e) => (loadError = e.message)).finally(() => (loading = false))
  })

  function payload(): Record<string, any> {
    const t = form.table!
    const out: Record<string, any> = {}
    for (const name of form.rules.keys()) {
      const f = t.fields.find((x) => x.name === name)
      if (!f || !writable(f)) continue
      let v = form.values[name]
      if (v === undefined) continue
      if (f.type === 'link') v = Array.isArray(v) ? v.map((l: any) => l._id) : v ? v._id : null
      if (f.type === 'number' && v === '') v = null
      out[name] = v
    }
    return out
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault()
    if (rt.editing) return
    formError = ''
    form.submitted = true
    if (!form.validate()) return
    if (form.uploading) { formError = 'Wait until the files finish uploading.'; return }
    form.submitting = true
    try {
      let result: any = null
      if (form.table) {
        result = updating && rowId
          ? await api.updateRow(rt.appId, form.table.id, rowId, payload())
          : await api.createRow(rt.appId, form.table.id, payload())
        rt.reload()
      }
      if (p.successMessage) rt.toast('success', p.successMessage)
      const values = { ...form.values }
      if (!updating && p.resetOnSubmit !== false) {
        for (const k of Object.keys(form.values)) delete form.values[k]
        form.submitted = false
      }
      await runActions(p.onSubmit, rt, scope(), { form: values, result, row: result ?? undefined })
    } catch (err: any) {
      const field = err.body?.field
      if (field && form.rules.has(field)) form.errors[field] = String(err.message).replace(`${field}: `, '')
      else formError = err.message
    } finally {
      form.submitting = false
    }
  }

  function reset() {
    for (const k of Object.keys(form.values)) delete form.values[k]
    for (const k of Object.keys(form.errors)) delete form.errors[k]
    form.submitted = false
    rt.reload()
  }
  // Other components read the form's values; actions clear or check it.
  shareValues(node.id, () => ({ values: form.values, valid: Object.keys(form.errors).length === 0, submitting: form.submitting }), {
    clear: () => { for (const k of Object.keys(form.values)) delete form.values[k]; for (const k of Object.keys(form.errors)) delete form.errors[k]; form.submitted = false },
    validate: () => { form.submitted = true; return form.validate() },
  })
  const cols: Record<string, string> = { '1': 'grid-cols-1', '2': 'grid-cols-1 @md:grid-cols-2', '3': 'grid-cols-1 @md:grid-cols-2 @xl:grid-cols-3' }
</script>

<form class="@container flex flex-col gap-6" onsubmit={submit} novalidate>
  {#if loading}
    <div class="flex flex-col gap-4"><Skeleton class="h-9 w-full" /><Skeleton class="h-9 w-full" /><Skeleton class="h-9 w-2/3" /></div>
  {:else if loadError}
    <p class="text-destructive text-sm">{loadError}</p>
  {:else}
    <div class={['grid', cols[p.columns] ?? cols['1'], gapClass[p.gap ?? 'md'], p.$layout]}>
      <Children {node} empty="Add form fields here" />
    </div>
  {/if}
  {#if formError}<p class="text-destructive bg-destructive/10 rounded-md px-3 py-2 text-sm">{formError}</p>{/if}
  {#if p.showSubmit !== false}
    <div class={['flex gap-2', p.buttonAlign === 'start' ? 'justify-start' : p.buttonAlign === 'stretch' ? '' : 'justify-end']}>
      {#if p.showReset}<Button type="button" variant="outline" onclick={reset} disabled={form.submitting}>{p.resetText || 'Reset'}</Button>{/if}
      <Button type="submit" disabled={form.submitting || loading || form.uploading > 0} class={p.buttonAlign === 'stretch' ? 'flex-1' : ''}>
        {#if form.submitting}<Spinner />{/if}{p.submitText || 'Save'}
      </Button>
    </div>
  {/if}
</form>
