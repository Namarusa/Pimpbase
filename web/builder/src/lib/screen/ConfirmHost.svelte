<script lang="ts">
  import * as AlertDialog from '$lib/components/ui/alert-dialog'

  // ConfirmHost answers the "Ask to confirm" action.
  let open = $state(false)
  let title = $state('')
  let message = $state('')
  let resolve: ((ok: boolean) => void) | null = null

  export function ask(t: string, m: string): Promise<boolean> {
    title = t
    message = m
    open = true
    return new Promise((r) => (resolve = r))
  }
  function done(ok: boolean) {
    open = false
    resolve?.(ok)
    resolve = null
  }
</script>

<AlertDialog.Root bind:open onOpenChange={(v) => { if (!v) done(false) }}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{title}</AlertDialog.Title>
      {#if message}<AlertDialog.Description>{message}</AlertDialog.Description>{/if}
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel onclick={() => done(false)}>Cancel</AlertDialog.Cancel>
      <AlertDialog.Action onclick={() => done(true)}>Continue</AlertDialog.Action>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
