<script lang="ts">
  import type { Snippet } from 'svelte'
  import { extendScope } from '../context.svelte'
  import type { Table } from '../../api'

  // RowScope lets everything inside bind to {{ row }} and {{ index }}.
  let { row, index = 0, table, children }: { row: Record<string, any>; index?: number; table?: Table; children: Snippet } = $props()
  extendScope(() => ({ row, index, rowTable: table }))
</script>

{@render children()}
