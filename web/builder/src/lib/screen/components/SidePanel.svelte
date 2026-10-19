<script lang="ts">
  import Dialog from './Dialog.svelte'
  import type { Node } from '../types'

  let { node, p }: { node: Node; p: Record<string, any> } = $props()
</script>

<Dialog {node} {p} panel />
