<script lang="ts">
  // NoKey explains that the assistant needs an API key, and who adds it.
  import { Button } from '$lib/components/ui/button'
  import KeyRoundIcon from '@lucide/svelte/icons/key-round'
  import { navigate } from '../router.svelte'
  import { isAdmin } from '../session.svelte'

  let { onNavigate }: { onNavigate?: () => void } = $props()
  const admin = $derived(isAdmin())
</script>

<div class="flex flex-col items-center gap-3 rounded-lg border border-dashed px-6 py-10 text-center">
  <div class="bg-muted flex size-10 items-center justify-center rounded-full"><KeyRoundIcon class="text-muted-foreground size-5" /></div>
  <div class="text-sm font-medium">The assistant needs an Anthropic API key</div>
  <p class="text-muted-foreground max-w-xs text-sm">
    The key stays on the Bubibase server and is never sent to published apps or browsers.
    {#if !admin}Ask a workspace admin to add one.{/if}
  </p>
  {#if admin}<Button size="sm" onclick={() => { onNavigate?.(); navigate('/users/ai') }}>Add a key</Button>{/if}
</div>
