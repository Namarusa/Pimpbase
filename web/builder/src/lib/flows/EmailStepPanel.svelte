<script lang="ts">
  import { toast } from 'svelte-sonner'
  import * as Dialog from '$lib/components/ui/dialog'
  import { Button } from '$lib/components/ui/button'
  import { Spinner } from '$lib/components/ui/spinner'
  import EyeIcon from '@lucide/svelte/icons/eye'
  import SendIcon from '@lucide/svelte/icons/send'
  import { api, type EmailLog, type FlowStep } from '../api'
  import EmailPreview from '../emails/EmailPreview.svelte'
  import { getEditor } from './editor.svelte'

  // EmailStepPanel previews what a Send email step sends, filled with the
  // sample data the editor shows (the latest run, or fetched samples),
  // and mails that to the builder as a test.
  let { step }: { step: FlowStep } = $props()
  const ed = getEditor()
  let open = $state(false)
  let busy = $state<'' | 'preview' | 'send'>('')
  let mail = $state<EmailLog | null>(null)
  let error = $state('')

  async function run(send: boolean) {
    if (!ed.flow) return
    busy = send ? 'send' : 'preview'
    try {
      const res = await api.previewFlowEmail(ed.flow.appId, ed.flow.id, $state.snapshot(step.inputs), $state.snapshot(ed.scope(step.id) ?? {}), send)
      mail = res.email
      error = res.error ?? ''
      if (send && res.sent) toast.success(`Test sent to ${res.sent.to}`, { description: res.sent.receipt.note })
      if (!send || error) open = true
    } catch (e: any) {
      if (send) toast.error(e.message)
      else { error = e.message; mail = null; open = true }
    } finally { busy = '' }
  }
  const list = (a?: string[]) => (a ?? []).join(', ')
</script>

<div class="bg-muted/40 flex flex-col gap-2 rounded-lg border p-3">
  <p class="text-muted-foreground text-xs">See the email with this flow's sample data{ed.samples.run ? ' from its latest run' : ''}, or mail it to yourself. Nothing goes to the real recipients.</p>
  <div class="flex gap-2">
    <Button variant="outline" size="sm" class="h-7 text-xs" onclick={() => run(false)} disabled={!!busy}>{#if busy === 'preview'}<Spinner />{:else}<EyeIcon />{/if}Preview</Button>
    <Button variant="outline" size="sm" class="h-7 text-xs" onclick={() => run(true)} disabled={!!busy}>{#if busy === 'send'}<Spinner />{:else}<SendIcon />{/if}Send me a test</Button>
  </div>
</div>

<Dialog.Root bind:open>
  <Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-3xl">
    <Dialog.Header>
      <Dialog.Title>{step.name}: the email it sends</Dialog.Title>
      <Dialog.Description>
        Filled with sample data{mail?.template ? ` from the template ${mail.template} (version ${mail.version})` : ''}.
        {#if mail}To {list(mail.to) || 'nobody yet'}{mail.cc?.length ? `, cc ${list(mail.cc)}` : ''}{mail.bcc?.length ? `, bcc ${list(mail.bcc)}` : ''}{mail.attachments?.length ? ` · attached: ${list(mail.attachments)}` : ''}.{/if}
      </Dialog.Description>
    </Dialog.Header>
    <EmailPreview html={mail?.html} text={mail?.text} subject={mail?.subject} from={mail?.from || ed.app?.name} to={list(mail?.to)} warnings={mail?.warnings ?? []} {error} compact badge="sample data" />
  </Dialog.Content>
</Dialog.Root>
