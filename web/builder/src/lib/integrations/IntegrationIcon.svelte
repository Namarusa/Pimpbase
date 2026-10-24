<script lang="ts">
  import type { Component } from 'svelte'
  import CreditCardIcon from '@lucide/svelte/icons/credit-card'
  import MessageSquareIcon from '@lucide/svelte/icons/message-square'
  import GitPullRequestIcon from '@lucide/svelte/icons/git-pull-request'
  import SmartphoneIcon from '@lucide/svelte/icons/smartphone'
  import SheetIcon from '@lucide/svelte/icons/sheet'
  import TableIcon from '@lucide/svelte/icons/table'
  import HardDriveIcon from '@lucide/svelte/icons/hard-drive'
  import PlugIcon from '@lucide/svelte/icons/plug'
  import type { Manifest } from '../api'

  // IntegrationIcon is a service's tile: its colour with a plain icon,
  // not its logo.
  let { manifest, size = 'md' }: { manifest?: Manifest; size?: 'sm' | 'md' | 'lg' } = $props()
  const icons: Record<string, Component> = {
    'credit-card': CreditCardIcon, 'message-square': MessageSquareIcon, 'git-pull-request': GitPullRequestIcon,
    smartphone: SmartphoneIcon, sheet: SheetIcon, table: TableIcon, 'hard-drive': HardDriveIcon,
  }
  const Icon = $derived(icons[manifest?.icon ?? ''] ?? PlugIcon)
  const box = { sm: 'size-6 rounded-md', md: 'size-9 rounded-lg', lg: 'size-11 rounded-xl' }
  const glyph = { sm: 'size-3.5', md: 'size-4.5', lg: 'size-5.5' }
</script>

<span class={['grid shrink-0 place-items-center text-white shadow-xs', box[size]]} style:background-color={manifest?.color ?? '#64748b'} aria-hidden="true">
  <Icon class={[glyph[size], 'text-white']} />
</span>
