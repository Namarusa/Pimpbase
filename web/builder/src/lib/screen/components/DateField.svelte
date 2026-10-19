<script lang="ts">
  import * as Popover from '$lib/components/ui/popover'
  import { Calendar } from '$lib/components/ui/calendar'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import CalendarIcon from '@lucide/svelte/icons/calendar'
  import XIcon from '@lucide/svelte/icons/x'
  import { CalendarDate, type DateValue } from '@internationalized/date'
  import FieldShell from './FieldShell.svelte'
  import { FieldBinding } from '../field.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
  const f = new FieldBinding(node, () => p, 'date')
  let open = $state(false)

  // Dates travel as text: 2026-10-19 for a day, or a UTC timestamp with a
  // time of day.
  const parsed = $derived.by(() => {
    const v = typeof f.value === 'string' ? f.value : ''
    if (!v) return null
    const d = new Date(v.length === 10 ? v + 'T00:00:00Z' : v)
    if (isNaN(d.getTime())) return null
    const day = v.length === 10 || v.endsWith('T00:00:00Z')
    return day
      ? { date: new CalendarDate(d.getUTCFullYear(), d.getUTCMonth() + 1, d.getUTCDate()), time: '' }
      : { date: new CalendarDate(d.getFullYear(), d.getMonth() + 1, d.getDate()), time: `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}` }
  })

  function write(date: DateValue | undefined, time: string) {
    if (!date) { f.value = null; return }
    const ymd = `${date.year}-${String(date.month).padStart(2, '0')}-${String(date.day).padStart(2, '0')}`
    f.value = p.includeTime && time ? new Date(`${ymd}T${time}`).toISOString() : ymd
    f.touch()
  }
  const label = $derived(parsed ? parsed.date.toDate('UTC').toLocaleDateString(undefined, { timeZone: 'UTC', year: 'numeric', month: 'long', day: 'numeric' }) : '')
</script>

<FieldShell {f} help={p.help}>
  <div class="flex gap-2">
    <Popover.Root bind:open>
      <Popover.Trigger id={f.id} disabled={f.disabled}>
        {#snippet child({ props })}
          <Button {...props} variant="outline" aria-invalid={!!f.error} class={['flex-1 justify-start font-normal', parsed ? '' : 'text-muted-foreground']}>
            <CalendarIcon />{label || p.placeholder || 'Pick a date'}
          </Button>
        {/snippet}
      </Popover.Trigger>
      <Popover.Content class="w-auto overflow-hidden p-0" align="start">
        <Calendar type="single" captionLayout="dropdown" value={parsed?.date} onValueChange={(d: DateValue | undefined) => { write(d, parsed?.time || '09:00'); open = false }} />
      </Popover.Content>
    </Popover.Root>
    {#if p.includeTime}
      <Input type="time" class="w-28" disabled={f.disabled || !parsed} value={parsed?.time ?? ''} oninput={(e) => write(parsed?.date, e.currentTarget.value)} />
    {/if}
    {#if parsed && !f.required && !f.disabled}
      <Button variant="ghost" size="icon" onclick={() => write(undefined, '')} aria-label="Clear date"><XIcon /></Button>
    {/if}
  </div>
</FieldShell>
