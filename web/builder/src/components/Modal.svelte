<script lang="ts">
  import type { Snippet } from 'svelte'
  let { title, onClose, children, footer, wide = false }: {
    title: string; onClose: () => void; children: Snippet; footer?: Snippet; wide?: boolean
  } = $props()
</script>

<svelte:window onkeydown={(e) => e.key === 'Escape' && onClose()} />
<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_static_element_interactions -->
<div class="backdrop" onclick={(e) => e.target === e.currentTarget && onClose()}>
  <div class="modal card" class:wide role="dialog" aria-modal="true" aria-label={title}>
    <div class="head"><h2>{title}</h2><button class="ghost small" onclick={onClose} aria-label="Close">✕</button></div>
    <div class="body">{@render children()}</div>
    {#if footer}<div class="foot">{@render footer()}</div>{/if}
  </div>
</div>

<style>
  .backdrop { position: fixed; inset: 0; background: rgba(10, 12, 20, 0.35); display: grid; place-items: center; z-index: 50; padding: 16px; }
  .modal { width: 100%; max-width: 460px; max-height: calc(100vh - 32px); display: flex; flex-direction: column; box-shadow: var(--shadow); }
  .modal.wide { max-width: 640px; }
  .head { display: flex; align-items: center; justify-content: space-between; padding: 16px 20px 8px; }
  .body { padding: 8px 20px; overflow: auto; }
  .foot { display: flex; gap: 8px; justify-content: flex-end; padding: 12px 20px 16px; }
</style>
