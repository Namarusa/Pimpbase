<script lang="ts">
  import * as Select from '$lib/components/ui/select'
  import { Button } from '$lib/components/ui/button'
  import { Input } from '$lib/components/ui/input'
  import PlusIcon from '@lucide/svelte/icons/plus'
  import XIcon from '@lucide/svelte/icons/x'
  import BindingInput from '../designer/BindingInput.svelte'
  import type { BindingGroup } from '../designer/context'
  import { flowHelp, type AIQuestion } from './steps'

  // QuestionsEditor edits a System One step's questions: each is a choice
  // among options, a score on levels, or a yes/no (noul), and is answered
  // with probabilities.
  let { questions, step, groups, scope, onChange }: {
    questions: AIQuestion[]; step: string; groups: BindingGroup[]; scope: Record<string, any> | null; onChange: (q: AIQuestion[]) => void
  } = $props()

  const types = [
    { value: 'choice', label: 'Choice', hint: 'picks one option' },
    { value: 'score', label: 'Score', hint: 'rates on levels' },
    { value: 'noul', label: 'Yes / no', hint: 'how likely it is true' },
  ]
  const nameOk = (n: string) => !n || /^[A-Za-z_][A-Za-z0-9_]{0,62}$/.test(n)
  const put = (k: number, patch: Partial<AIQuestion>) => onChange(questions.map((q, m) => (m === k ? { ...q, ...patch } : q)))
  function retype(k: number, type: AIQuestion['type']) {
    const q = questions[k]
    put(k, {
      type,
      options: type === 'choice' ? (q.options?.length ? q.options : [{ name: '' }, { name: '' }]) : undefined,
      levels: type === 'score' ? (q.levels?.length ? q.levels : ['', '', '']) : undefined,
      true: type === 'noul' ? q.true : undefined, false: type === 'noul' ? q.false : undefined,
    })
  }
  const reads: Record<string, string> = { choice: 'choice', score: 'score', noul: 'noul' }
</script>

<div class="flex flex-col gap-2">
  {#each questions as q, k (k)}
    <div class="flex flex-col gap-1.5 rounded-md border p-2" data-testid="so-question">
      <div class="flex items-center gap-1.5">
        <Input class="h-7 w-[38%] shrink-0 font-mono text-xs" aria-invalid={!nameOk(q.name)} value={q.name} placeholder="team" oninput={(e) => put(k, { name: e.currentTarget.value.trim() })} />
        <Select.Root type="single" value={q.type} onValueChange={(v) => retype(k, v as AIQuestion['type'])}>
          <Select.Trigger size="sm" class="h-7 min-w-0 flex-1 text-xs">{types.find((t) => t.value === q.type)?.label}</Select.Trigger>
          <Select.Content>{#each types as t (t.value)}<Select.Item value={t.value} label={t.label}>{t.label}<span class="text-muted-foreground ml-auto text-[10px]">{t.hint}</span></Select.Item>{/each}</Select.Content>
        </Select.Root>
        <Button variant="ghost" size="icon" class="size-7 shrink-0" aria-label="Remove question" onclick={() => onChange(questions.filter((_, m) => m !== k))}><XIcon class="size-3.5" /></Button>
      </div>
      <BindingInput value={q.instructions ?? ''} {groups} {scope} showPreview={false} help={flowHelp} placeholder={q.type === 'noul' ? 'Is the customer reporting a defect?' : q.type === 'score' ? 'How urgent is this ticket?' : 'Which team should own this ticket?'} onChange={(v) => put(k, { instructions: v })} />
      {#if q.type === 'choice'}
        {#each q.options ?? [] as o, i (i)}
          <div class="flex items-center gap-1.5 pl-3">
            <Input class="h-7 w-[34%] shrink-0 font-mono text-xs" value={o.name} placeholder={['billing', 'bug', 'account'][i] ?? 'option'} oninput={(e) => put(k, { options: q.options!.map((x, j) => (j === i ? { ...x, name: e.currentTarget.value } : x)) })} />
            <Input class="h-7 min-w-0 flex-1 text-xs" value={o.description ?? ''} placeholder="what it means (optional)" oninput={(e) => put(k, { options: q.options!.map((x, j) => (j === i ? { ...x, description: e.currentTarget.value } : x)) })} />
            <Button variant="ghost" size="icon" class="size-7 shrink-0" aria-label="Remove option" onclick={() => put(k, { options: q.options!.filter((_, j) => j !== i) })}><XIcon class="size-3" /></Button>
          </div>
        {/each}
        <Button variant="ghost" size="sm" class="text-muted-foreground ml-2 h-6 self-start text-[11px]" onclick={() => put(k, { options: [...(q.options ?? []), { name: '' }] })}><PlusIcon class="size-3" />Option</Button>
      {:else if q.type === 'score'}
        {#each q.levels ?? [] as l, i (i)}
          <div class="flex items-center gap-1.5 pl-3">
            <span class="text-muted-foreground w-4 shrink-0 text-right font-mono text-xs">{i}</span>
            <Input class="h-7 min-w-0 flex-1 text-xs" value={l} placeholder={['Can wait', 'This week', 'Right now'][i] ?? 'level'} oninput={(e) => put(k, { levels: q.levels!.map((x, j) => (j === i ? e.currentTarget.value : x)) })} />
            <Button variant="ghost" size="icon" class="size-7 shrink-0" aria-label="Remove level" onclick={() => put(k, { levels: q.levels!.filter((_, j) => j !== i) })}><XIcon class="size-3" /></Button>
          </div>
        {/each}
        {#if (q.levels?.length ?? 0) < 10}<Button variant="ghost" size="sm" class="text-muted-foreground ml-2 h-6 self-start text-[11px]" onclick={() => put(k, { levels: [...(q.levels ?? []), ''] })}><PlusIcon class="size-3" />Level</Button>{/if}
      {:else}
        <div class="grid grid-cols-2 gap-1.5 pl-3">
          <Input class="h-7 text-xs" value={q.true ?? ''} placeholder="Yes means… (optional)" oninput={(e) => put(k, { true: e.currentTarget.value })} />
          <Input class="h-7 text-xs" value={q.false ?? ''} placeholder="No means… (optional)" oninput={(e) => put(k, { false: e.currentTarget.value })} />
        </div>
      {/if}
      <p class="text-muted-foreground pl-1 text-[11px]">
        <code class="font-mono">{`steps.${step}.data.${q.name || 'name'}.${reads[q.type]}`}</code>
        {#if q.type === 'choice'}· the option, with <code class="font-mono">.confidence</code> and <code class="font-mono">.probabilities</code>
        {:else if q.type === 'score'}· the expected level from 0, may be between levels, with <code class="font-mono">.confidence</code>
        {:else}· from 0 to 1, how likely it is true{/if}
      </p>
    </div>
  {/each}
  <Button variant="outline" size="sm" class="h-7 self-start border-dashed text-xs" onclick={() => onChange([...questions, { name: '', type: 'choice', instructions: '', options: [{ name: '' }, { name: '' }] }])}><PlusIcon />Add question</Button>
</div>
