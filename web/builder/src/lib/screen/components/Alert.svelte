<script lang="ts">
  import * as Alert from '$lib/components/ui/alert'
  import InfoIcon from '@lucide/svelte/icons/info'
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check'
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert'
  import CircleAlertIcon from '@lucide/svelte/icons/circle-alert'
  import { toText } from '../bindings'
  import type { Node } from '../types'

  let { p }: { node: Node; p: Record<string, any> } = $props()
  const tone: Record<string, string> = {
    info: '',
    success: 'border-emerald-500/40 text-emerald-700 dark:text-emerald-400 *:data-[slot=alert-description]:text-emerald-700/80 dark:*:data-[slot=alert-description]:text-emerald-400/80',
    warning: 'border-amber-500/40 text-amber-700 dark:text-amber-400 *:data-[slot=alert-description]:text-amber-700/80 dark:*:data-[slot=alert-description]:text-amber-400/80',
    destructive: '',
  }
  const Icon = $derived(p.variant === 'success' ? CircleCheckIcon : p.variant === 'warning' ? TriangleAlertIcon : p.variant === 'destructive' ? CircleAlertIcon : InfoIcon)
</script>

<Alert.Root variant={p.variant === 'destructive' ? 'destructive' : 'default'} class={tone[p.variant]}>
  <Icon />
  {#if p.title}<Alert.Title>{toText(p.title)}</Alert.Title>{/if}
  {#if p.text}<Alert.Description>{toText(p.text)}</Alert.Description>{/if}
</Alert.Root>
