<script lang="ts">
  import * as Popover from '$lib/components/ui/popover'
  import * as ToggleGroup from '$lib/components/ui/toggle-group'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import { Label } from '$lib/components/ui/label'
  import PaletteIcon from '@lucide/svelte/icons/palette'
  import CheckIcon from '@lucide/svelte/icons/check'
  import { api } from '../api'
  import { accentChoices } from '../screen/theme'
  import { getDesigner } from './state.svelte'
  import type { Design } from '../screen/types'

  // ThemeMenu sets the published app's theme, colour and navigation.
  const d = getDesigner()
  let error = $state('')
  let title = $state(d.app.design.title)

  async function save(p: Partial<Design>) {
    const design = { ...d.app.design, ...p }
    d.app.design = design
    try {
      d.app = await api.updateApp(d.app.id, { design })
      error = ''
    } catch (e: any) { error = e.message }
  }
</script>

<Popover.Root>
  <Popover.Trigger>
    {#snippet child({ props })}<Button {...props} variant="outline" size="sm"><PaletteIcon />Theme</Button>{/snippet}
  </Popover.Trigger>
  <Popover.Content class="flex w-80 flex-col gap-4" align="end">
    <div class="flex flex-col gap-2">
      <Label class="text-xs">Theme</Label>
      <ToggleGroup.Root type="single" variant="outline" size="sm" class="w-full" value={d.app.design.theme} onValueChange={(v) => v && save({ theme: v as Design['theme'] })}>
        <ToggleGroup.Item value="light" class="flex-1 text-xs">Light</ToggleGroup.Item>
        <ToggleGroup.Item value="dark" class="flex-1 text-xs">Dark</ToggleGroup.Item>
        <ToggleGroup.Item value="system" class="flex-1 text-xs">Device</ToggleGroup.Item>
      </ToggleGroup.Root>
    </div>
    <div class="flex flex-col gap-2">
      <Label class="text-xs">Accent colour</Label>
      <div class="flex flex-wrap gap-2">
        {#each accentChoices as c (c)}
          <button type="button" class="grid size-7 place-items-center rounded-full ring-offset-2 ring-offset-background" style={`background: ${c}`}
            class:ring-2={d.app.design.accent === c} aria-label={c} onclick={() => save({ accent: c })}>
            {#if d.app.design.accent === c}<CheckIcon class="size-3.5 text-white" />{/if}
          </button>
        {/each}
        <Input type="color" class="size-7 cursor-pointer rounded-full p-0" value={d.app.design.accent || '#4f46e5'} onchange={(e) => save({ accent: e.currentTarget.value })} aria-label="Another colour" />
      </div>
    </div>
    <div class="flex flex-col gap-2">
      <Label class="text-xs">Navigation</Label>
      <ToggleGroup.Root type="single" variant="outline" size="sm" class="w-full" value={d.app.design.navigation} onValueChange={(v) => v && save({ navigation: v as Design['navigation'] })}>
        <ToggleGroup.Item value="sidebar" class="flex-1 text-xs">Sidebar</ToggleGroup.Item>
        <ToggleGroup.Item value="top" class="flex-1 text-xs">Top bar</ToggleGroup.Item>
        <ToggleGroup.Item value="none" class="flex-1 text-xs">None</ToggleGroup.Item>
      </ToggleGroup.Root>
    </div>
    <div class="flex flex-col gap-2">
      <Label for="app-title" class="text-xs">Title in the app</Label>
      <Input id="app-title" class="h-8" placeholder={d.app.name} bind:value={title} onblur={() => title !== d.app.design.title && save({ title })} />
    </div>
    {#if error}<p class="text-destructive text-xs">{error}</p>{/if}
  </Popover.Content>
</Popover.Root>
