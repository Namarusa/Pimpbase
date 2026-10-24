// What email templates are made of: block types, new blocks, starter
// templates, and the bindings a template's texts can read.
import type { Component } from 'svelte'
import HeadingIcon from '@lucide/svelte/icons/heading'
import PilcrowIcon from '@lucide/svelte/icons/pilcrow'
import MousePointerClickIcon from '@lucide/svelte/icons/mouse-pointer-click'
import ImageIcon from '@lucide/svelte/icons/image'
import MinusIcon from '@lucide/svelte/icons/minus'
import MoveVerticalIcon from '@lucide/svelte/icons/move-vertical'
import TableIcon from '@lucide/svelte/icons/table'
import CodeIcon from '@lucide/svelte/icons/code'
import type { EmailBlock, EmailBlockType, EmailParam, EmailTemplate } from '../api'
import type { BindingGroup } from '../designer/context'

export interface BlockInfo { type: EmailBlockType; label: string; icon: Component; description: string }

export const blockTypes: BlockInfo[] = [
  { type: 'heading', label: 'Heading', icon: HeadingIcon, description: 'A title' },
  { type: 'text', label: 'Text', icon: PilcrowIcon, description: 'Paragraphs, bold, links, lists' },
  { type: 'button', label: 'Button', icon: MousePointerClickIcon, description: 'A link that stands out' },
  { type: 'image', label: 'Image', icon: ImageIcon, description: 'A picture from an address' },
  { type: 'table', label: 'Table', icon: TableIcon, description: 'One row per item of a list' },
  { type: 'divider', label: 'Divider', icon: MinusIcon, description: 'A thin line' },
  { type: 'spacer', label: 'Space', icon: MoveVerticalIcon, description: 'Empty room' },
  { type: 'html', label: 'HTML', icon: CodeIcon, description: 'Your own markup' },
]
export const blockInfo = (t: string) => blockTypes.find((b) => b.type === t) ?? blockTypes[1]

const uid = () => 'b' + Math.random().toString(36).slice(2, 8)

export function newBlock(type: EmailBlockType): EmailBlock {
  const b: EmailBlock = { id: uid(), type }
  switch (type) {
    case 'heading': b.text = 'A heading'; b.level = 1; break
    case 'text': b.text = 'Write something. **Bold**, *italic* and [links](https://example.com) work, and lines starting with "- " make a list.'; break
    case 'button': b.text = 'Open'; b.url = 'https://'; break
    case 'image': b.url = ''; b.alt = ''; b.align = 'center'; break
    case 'table': b.items = ''; b.columns = [{ label: 'Item', value: '{{ loop.item.Name }}' }]; break
    case 'spacer': b.height = 16; break
    case 'html': b.text = '<p style="margin:0 0 14px">Hello {{ customer.Name }}</p>'; break
  }
  return b
}

// summary is a block's one line in the list.
export function summary(b: EmailBlock): string {
  const strip = (s = '') => s.replace(/\s+/g, ' ').trim()
  switch (b.type) {
    case 'heading': case 'text': return strip(b.text) || 'Empty'
    case 'button': return `${strip(b.text) || 'Button'} → ${b.url || '…'}`
    case 'image': return b.url || 'No address yet'
    case 'table': return `${(b.columns ?? []).map((c) => c.label).join(' · ') || 'No columns'}${b.items ? ` for each of ${b.items}` : ''}`
    case 'spacer': return `${b.height ?? 16} px`
    case 'divider': return 'Line'
    case 'html': return strip(b.text).slice(0, 80) || 'Empty'
  }
  return b.type
}

// sampleScope is what a template's bindings read in the editor: the
// fields' samples and the app.
export function sampleScope(t: Pick<EmailTemplate, 'params'>, app: { name: string; slug: string } | null): Record<string, any> {
  const out: Record<string, any> = {}
  for (const p of t.params ?? []) if (p.sample !== undefined) out[p.name] = p.sample
  out.app = app ? { name: app.name, slug: app.slug } : { name: 'App', slug: 'app' }
  return out
}

// fieldPaths lists a sample's keys as binding paths, a level or two deep.
function paths(prefix: string, v: any, depth: number, out: { path: string; hint?: string }[]) {
  if (Array.isArray(v)) {
    out.push({ path: prefix, hint: `list of ${v.length}` })
    return
  }
  if (v && typeof v === 'object') {
    if (depth === 0) { out.push({ path: prefix, hint: 'object' }); return }
    for (const [k, x] of Object.entries(v)) {
      const key = /^[A-Za-z_]\w*$/.test(k) ? `${prefix}.${k}` : `${prefix}[${JSON.stringify(k)}]`
      paths(key, x, depth - 1, out)
    }
    return
  }
  out.push({ path: prefix, hint: v === undefined || v === null ? '' : typeof v })
}

export function templateGroups(t: Pick<EmailTemplate, 'params'>, item?: any): BindingGroup[] {
  const fields: { path: string; hint?: string }[] = []
  for (const p of t.params ?? []) {
    if (p.sample !== undefined && p.type === 'object') paths(p.name, p.sample, 1, fields)
    else fields.push({ path: p.name, hint: p.type })
  }
  const groups: BindingGroup[] = []
  if (item !== undefined) {
    const items: { path: string; hint?: string }[] = []
    if (item && typeof item === 'object' && !Array.isArray(item)) paths('loop.item', item, 1, items)
    else items.push({ path: 'loop.item' })
    items.push({ path: 'loop.number', hint: 'from 1' })
    groups.push({ label: 'Current row', items, open: true, note: 'The item of the list this row shows.' })
  }
  groups.push({ label: 'Fields', items: fields, open: true, note: fields.length ? undefined : 'Add fields in the Fields tab; flows fill them.' })
  groups.push({ label: 'App', items: [{ path: 'app.name' }, { path: 'app.slug' }] })
  return groups
}

export const emailHelp = 'Pipes: | upper, | number:2, | default:"x", | raw (keeps HTML). Formulas: {{ = order.Total * 1.2 }}. Repeat: {{#each lines}}{{ loop.item.Name }}{{/each}}; branch: {{#if order.Paid}}…{{else}}…{{/if}}.'

export function blankTemplate(name: string): Partial<EmailTemplate> {
  return { name, subject: '', preheader: '', mode: 'blocks', blocks: [], style: {}, html: '', text: '', params: [] }
}

const p = (name: string, type: EmailParam['type'], sample: any, description = ''): EmailParam => ({ name, type, sample, description })

export interface Starter { key: string; label: string; description: string; make: (name: string) => Partial<EmailTemplate> }

export const starters: Starter[] = [
  { key: 'blank', label: 'Blank', description: 'Start from nothing.', make: blankTemplate },
  {
    key: 'order', label: 'Order confirmation', description: 'A thank-you with the order lines and a button.',
    make: (name) => ({
      ...blankTemplate(name),
      subject: 'Your order {{ order.Number }} is confirmed',
      preheader: 'Thanks {{ customer.Name }}, here is what you ordered.',
      style: { brand: '#2563eb', footer: '{{ app.name }} · You get this email because you ordered from us.' },
      params: [
        p('customer', 'object', { Name: 'Ada Lovelace', Email: 'ada@example.com' }, 'the customer row'),
        p('order', 'object', { Number: 'A-1042', Total: 86.5, Paid: true }, 'the order row'),
        p('lines', 'list', [{ Product: 'Green tea', Qty: 2, Price: 12.5 }, { Product: 'Teapot', Qty: 1, Price: 61.5 }], 'the order lines'),
      ],
      blocks: [
        { id: 'b1', type: 'heading', level: 1, text: 'Thanks for your order, {{ customer.Name }}!' },
        { id: 'b2', type: 'text', text: 'We received order **{{ order.Number }}** and are getting it ready.' },
        { id: 'b3', type: 'table', items: '{{ lines }}', columns: [{ label: 'Product', value: '{{ loop.item.Product }}' }, { label: 'Qty', value: '{{ loop.item.Qty }}', align: 'right' }, { label: 'Price', value: '{{ loop.item.Price | number:2 }}', align: 'right' }] },
        { id: 'b4', type: 'text', text: '**Total: {{ order.Total | number:2 }}**', align: 'right' },
        { id: 'b5', type: 'text', text: 'Payment received, thank you.', showWhen: '{{ order.Paid }}' },
        { id: 'b6', type: 'button', text: 'See your order', url: 'https://example.com/orders/{{ order.Number }}' },
        { id: 'b7', type: 'divider' },
        { id: 'b8', type: 'text', text: 'Questions? Just reply to this email.' },
      ],
    }),
  },
  {
    key: 'notice', label: 'Notification', description: 'A short message with one action.',
    make: (name) => ({
      ...blankTemplate(name),
      subject: '{{ title }}',
      preheader: '{{ message }}',
      params: [p('name', 'text', 'Kim', 'who gets it'), p('title', 'text', 'Your request was approved'), p('message', 'text', 'Bo approved your leave from 3 to 7 March.'), p('link', 'text', 'https://example.com/requests/12')],
      blocks: [
        { id: 'b1', type: 'heading', level: 2, text: '{{ title }}' },
        { id: 'b2', type: 'text', text: 'Hi {{ name }},\n\n{{ message }}' },
        { id: 'b3', type: 'button', text: 'Open it', url: '{{ link }}' },
      ],
    }),
  },
  {
    key: 'digest', label: 'Weekly summary', description: 'Counts and a table of rows, for scheduled flows.',
    make: (name) => ({
      ...blankTemplate(name),
      subject: 'Your week: {{ count }} new tickets',
      params: [p('count', 'number', 3), p('rows', 'list', [{ Title: 'Printer jam', Status: 'Open', Owner: 'Bo' }, { Title: 'VPN down', Status: 'Closed', Owner: 'Kim' }, { Title: 'New laptop', Status: 'Open', Owner: 'Lena' }], 'rows from Find rows')],
      style: { brand: '#7c3aed' },
      blocks: [
        { id: 'b1', type: 'heading', level: 1, text: 'This week' },
        { id: 'b2', type: 'text', text: '{{ count }} tickets came in. Here they are:' },
        { id: 'b3', type: 'table', items: '{{ rows }}', columns: [{ label: 'Ticket', value: '{{ loop.item.Title }}' }, { label: 'Status', value: '{{ loop.item.Status }}' }, { label: 'Owner', value: '{{ loop.item.Owner }}' }] },
        { id: 'b4', type: 'button', text: 'Open the helpdesk', url: 'https://example.com/app/helpdesk' },
      ],
    }),
  },
]
