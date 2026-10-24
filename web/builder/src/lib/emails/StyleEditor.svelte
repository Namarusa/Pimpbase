<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import BindingInput from '../designer/BindingInput.svelte'
  import type { EmailStyle } from '../api'
  import type { BindingGroup } from '../designer/context'
  import { emailHelp } from './blocks'

  // StyleEditor sets the look of a blocks template.
  let { style, groups, scope, onChange }: { style: EmailStyle; groups: BindingGroup[]; scope: Record<string, any>; onChange: () => void } = $props()
  const set = (k: keyof EmailStyle, v: any) => { (style as any)[k] = v === '' ? undefined : v; onChange() }
  const colors: { key: keyof EmailStyle; label: string; def: string }[] = [
    { key: 'brand', label: 'Brand (buttons, links)', def: '#2563eb' },
    { key: 'background', label: 'Background', def: '#f4f4f5' },
    { key: 'panel', label: 'Panel', def: '#ffffff' },
    { key: 'textColor', label: 'Text', def: '#18181b' },
  ]
  const presets = ['#2563eb', '#16a34a', '#7c3aed', '#db2777', '#ea580c', '#0f172a']
</script>

<div class="flex flex-col gap-4">
  <div class="flex flex-col gap-1.5">
    <Label class="text-xs">Brand presets</Label>
    <div class="flex gap-1.5">
      {#each presets as c (c)}
        <button type="button" class={['size-7 rounded-full border-2', (style.brand ?? '#2563eb') === c ? 'border-foreground' : 'border-transparent']} style={`background:${c}`} aria-label={`Brand ${c}`} onclick={() => set('brand', c)}></button>
      {/each}
    </div>
  </div>
  <div class="grid grid-cols-2 gap-3">
    {#each colors as c (c.key)}
      <div class="flex flex-col gap-1.5">
        <Label class="text-xs">{c.label}</Label>
        <div class="flex items-center gap-1.5">
          <input type="color" class="size-8 shrink-0 cursor-pointer rounded border" value={(style[c.key] as string) || c.def} oninput={(e) => set(c.key, e.currentTarget.value)} aria-label={c.label} />
          <Input class="h-8 font-mono text-xs" placeholder={c.def} value={(style[c.key] as string) ?? ''} oninput={(e) => set(c.key, e.currentTarget.value)} />
        </div>
      </div>
    {/each}
  </div>
  <div class="grid grid-cols-2 gap-3">
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Font</Label>
      <Select.Root type="single" value={style.font || 'sans'} onValueChange={(v) => set('font', v === 'sans' ? '' : v)}>
        <Select.Trigger size="sm" class="w-full">{style.font === 'serif' ? 'Serif' : 'Sans serif'}</Select.Trigger>
        <Select.Content><Select.Item value="sans" label="Sans serif">Sans serif</Select.Item><Select.Item value="serif" label="Serif">Serif</Select.Item></Select.Content>
      </Select.Root>
    </div>
    <div class="flex flex-col gap-1.5">
      <Label class="text-xs">Button corners (px)</Label>
      <Input class="h-8" type="number" min="0" max="40" value={style.radius ?? 8} oninput={(e) => set('radius', e.currentTarget.value === '' ? undefined : +e.currentTarget.value)} />
    </div>
  </div>
  <div class="flex flex-col gap-1.5">
    <Label class="text-xs">Logo address</Label>
    <BindingInput value={style.logo ?? ''} mono {groups} {scope} help={emailHelp} placeholder="https://example.com/logo.png" onChange={(v) => set('logo', v)} />
  </div>
  {#if style.logo}
    <div class="flex flex-col gap-1.5"><Label class="text-xs">Logo width (px)</Label><Input class="h-8" type="number" min="20" max="600" value={style.logoWidth ?? 120} oninput={(e) => set('logoWidth', +e.currentTarget.value || undefined)} /></div>
  {/if}
  <div class="flex flex-col gap-1.5">
    <Label class="text-xs">Footer</Label>
    <BindingInput multiline value={style.footer ?? ''} {groups} {scope} help={emailHelp} placeholder="Your company · address · why they get this email" onChange={(v) => set('footer', v)} />
  </div>
</div>
