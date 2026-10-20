<script lang="ts">
  let { color, class: cls = '' }: { color: string; class?: string } = $props()
</script>

<span class={['inline-block size-2 shrink-0 rounded-full', cls]} style:background={color}></span>
