<script lang="ts">
  import { Textarea } from '$lib/components/ui/textarea'

  let { value, onChange, id }: { value: string[]; onChange: (v: string[]) => void; id?: string } = $props()
  // The text is kept as typed so blank lines can be added while typing.
  let text = $state((Array.isArray(value) ? value : []).join('\n'))
</script>

<Textarea {id} rows={3} class="text-sm" placeholder="One option per line" value={text}
  oninput={(e) => { text = e.currentTarget.value; onChange(text.split('\n').map((s) => s.trim()).filter(Boolean)) }} />
