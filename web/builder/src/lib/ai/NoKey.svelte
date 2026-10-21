<script lang="ts">
  // NoKey explains that the assistant needs a provider set up, and who
  // does it.
  import { Button } from '$lib/components/ui/button'
  import KeyRoundIcon from '@lucide/svelte/icons/key-round'
  import { navigate } from '../router.svelte'
  import { isAdmin } from '../session.svelte'

  let { onNavigate }: { onNavigate?: () => void } = $props()
  const admin = $derived(isAdmin())
</script>

<div class="flex flex-col items-center gap-3 rounded-lg border border-dashed px-6 py-10 text-center">
  <div class="bg-muted flex size-10 items-center justify-center rounded-full"><KeyRoundIcon class="text-muted-foreground size-5" /></div>
  <div class="text-sm font-medium">The assistant needs an AI provider</div>
  <p class="text-muted-foreground max-w-xs text-sm">
    Pick Anthropic, OpenAI, Gemini, Azure OpenAI, Mistral or your own OpenAI-compatible server. Keys stay on the Pimpbase server and are never sent to published apps or browsers.
    {#if !admin}Ask a workspace admin to add one.{/if}
  </p>
  {#if admin}<Button size="sm" onclick={() => { onNavigate?.(); navigate('/users/ai') }}>Set it up</Button>{/if}
</div>
