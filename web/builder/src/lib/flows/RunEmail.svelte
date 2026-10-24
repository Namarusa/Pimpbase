<script lang="ts">
  import { Badge } from '$lib/components/ui/badge'
  import { Button } from '$lib/components/ui/button'
  import MailIcon from '@lucide/svelte/icons/mail'
  import PaperclipIcon from '@lucide/svelte/icons/paperclip'
  import type { EmailLog } from '../api'
  import EmailPreview from '../emails/EmailPreview.svelte'

  // RunEmail shows, in a run's log, the email a Send email step made:
  // who it went to and the email itself.
  let { email, failed = false }: { email: EmailLog; failed?: boolean } = $props()
  let shown = $state(false)
  const list = (a?: string[]) => (a ?? []).join(', ')
</script>

<div class="rounded-md border">
  <div class="flex flex-wrap items-center gap-2 px-2.5 py-2">
    <MailIcon class="text-muted-foreground size-3.5" />
    <span class="min-w-0 flex-1 truncate text-xs"><span class="font-medium">{email.subject || '(no subject)'}</span><span class="text-muted-foreground"> · to {list(email.to)}{email.cc?.length ? `, cc ${list(email.cc)}` : ''}{email.bcc?.length ? `, bcc ${list(email.bcc)}` : ''}</span></span>
    {#if email.template}<Badge variant="secondary" class="h-5 px-1.5 text-[10px]">{email.template} v{email.version}</Badge>{/if}
    {#if email.dryRun}<Badge variant="outline" class="h-5 px-1.5 text-[10px]">preview only, not sent</Badge>{:else if failed}<Badge variant="destructive" class="h-5 px-1.5 text-[10px]">not sent</Badge>{/if}
    <Button variant="ghost" size="sm" class="h-6 px-2 text-xs" onclick={() => (shown = !shown)}>{shown ? 'Hide email' : 'Show email'}</Button>
  </div>
  {#if email.attachments?.length}
    <div class="text-muted-foreground flex items-center gap-1.5 border-t px-2.5 py-1.5 text-xs"><PaperclipIcon class="size-3" />{list(email.attachments)}</div>
  {/if}
  {#if shown}
    <div class="border-t p-2.5">
      {#if email.cut}<p class="text-muted-foreground mb-2 text-xs">The email was too large to keep in the log; its text is shown.</p>{/if}
      <EmailPreview html={email.html} text={email.text} subject={email.subject} from={email.from} to={list(email.to)} warnings={email.warnings ?? []} compact />
    </div>
  {/if}
</div>
