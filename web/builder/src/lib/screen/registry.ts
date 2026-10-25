// Every component builders can place on a screen: its settings, defaults
// and the Svelte component that draws it.
import SquareDashedIcon from '@lucide/svelte/icons/square-dashed'
import LayoutGridIcon from '@lucide/svelte/icons/layout-grid'
import SquareIcon from '@lucide/svelte/icons/square'
import PanelTopIcon from '@lucide/svelte/icons/panel-top'
import ListCollapseIcon from '@lucide/svelte/icons/list-collapse'
import SeparatorHorizontalIcon from '@lucide/svelte/icons/separator-horizontal'
import MoveVerticalIcon from '@lucide/svelte/icons/move-vertical'
import AppWindowIcon from '@lucide/svelte/icons/app-window'
import PanelRightIcon from '@lucide/svelte/icons/panel-right'
import HeadingIcon from '@lucide/svelte/icons/heading'
import PilcrowIcon from '@lucide/svelte/icons/pilcrow'
import TagIcon from '@lucide/svelte/icons/tag'
import ImageIcon from '@lucide/svelte/icons/image'
import LinkIcon from '@lucide/svelte/icons/link'
import MousePointerClickIcon from '@lucide/svelte/icons/mouse-pointer-click'
import MegaphoneIcon from '@lucide/svelte/icons/megaphone'
import LoaderIcon from '@lucide/svelte/icons/loader'
import CircleUserIcon from '@lucide/svelte/icons/circle-user'
import GaugeIcon from '@lucide/svelte/icons/gauge'
import InboxIcon from '@lucide/svelte/icons/inbox'
import DatabaseIcon from '@lucide/svelte/icons/database'
import IdCardIcon from '@lucide/svelte/icons/id-card'
import RepeatIcon from '@lucide/svelte/icons/repeat'
import TableIcon from '@lucide/svelte/icons/table'
import LayoutListIcon from '@lucide/svelte/icons/layout-list'
import WalletCardsIcon from '@lucide/svelte/icons/wallet-cards'
import ChartColumnIcon from '@lucide/svelte/icons/chart-column'
import ClipboardListIcon from '@lucide/svelte/icons/clipboard-list'
import FormIcon from '@lucide/svelte/icons/form'
import TextCursorInputIcon from '@lucide/svelte/icons/text-cursor-input'
import WholeWordIcon from '@lucide/svelte/icons/whole-word'
import HashIcon from '@lucide/svelte/icons/hash'
import ChevronDownIcon from '@lucide/svelte/icons/chevron-down'
import CircleDotIcon from '@lucide/svelte/icons/circle-dot'
import ListChecksIcon from '@lucide/svelte/icons/list-checks'
import CalendarIcon from '@lucide/svelte/icons/calendar'
import SquareCheckIcon from '@lucide/svelte/icons/square-check'
import ToggleRightIcon from '@lucide/svelte/icons/toggle-right'
import SlidersHorizontalIcon from '@lucide/svelte/icons/sliders-horizontal'
import Link2Icon from '@lucide/svelte/icons/link-2'
import FileTextIcon from '@lucide/svelte/icons/file-text'
import PaperclipIcon from '@lucide/svelte/icons/paperclip'

import Container from './components/Container.svelte'
import Grid from './components/Grid.svelte'
import Card from './components/Card.svelte'
import Tabs from './components/Tabs.svelte'
import Tab from './components/Tab.svelte'
import Accordion from './components/Accordion.svelte'
import Divider from './components/Divider.svelte'
import Spacer from './components/Spacer.svelte'
import Dialog from './components/Dialog.svelte'
import SidePanel from './components/SidePanel.svelte'
import Heading from './components/Heading.svelte'
import Text from './components/Text.svelte'
import Badge from './components/Badge.svelte'
import Image from './components/Image.svelte'
import Link from './components/Link.svelte'
import Button from './components/Button.svelte'
import Alert from './components/Alert.svelte'
import Progress from './components/Progress.svelte'
import Avatar from './components/Avatar.svelte'
import Stat from './components/Stat.svelte'
import EmptyState from './components/EmptyState.svelte'
import DataProvider from './components/DataProvider.svelte'
import RowProvider from './components/RowProvider.svelte'
import Repeater from './components/Repeater.svelte'
import DataTable from './components/DataTable.svelte'
import List from './components/List.svelte'
import Cards from './components/Cards.svelte'
import Chart from './components/Chart.svelte'
import Details from './components/Details.svelte'
import Form from './components/Form.svelte'
import TextField from './components/TextField.svelte'
import TextareaField from './components/TextareaField.svelte'
import NumberField from './components/NumberField.svelte'
import SelectField from './components/SelectField.svelte'
import RadioField from './components/RadioField.svelte'
import CheckboxGroupField from './components/CheckboxGroupField.svelte'
import DateField from './components/DateField.svelte'
import CheckboxField from './components/CheckboxField.svelte'
import SwitchField from './components/SwitchField.svelte'
import SliderField from './components/SliderField.svelte'
import LinkField from './components/LinkField.svelte'
import FileField from './components/FileField.svelte'
import PdfViewer from './components/PdfViewer.svelte'

import { sizes } from './styles'
import { chartPalettes } from './theme'
import { aggregateOptions } from './rows.svelte'
import type { ComponentDef, Setting } from './types'

const opts = (...pairs: [string, string][]) => pairs.map(([value, label]) => ({ value, label }))

const gap: Setting = { key: 'gap', label: 'Spacing', kind: 'segmented', options: sizes, section: 'Layout' }

// Settings every component has.
export const commonSettings: Setting[] = [
  { key: 'grow', label: 'Fill the free space in a row', kind: 'boolean', section: 'Layout' },
  { key: 'span', label: 'Columns to span in a grid', kind: 'segmented', options: opts(['1', '1'], ['2', '2'], ['3', '3'], ['4', '4'], ['full', 'All']), section: 'Layout' },
  { key: 'roles', label: 'Visible to', kind: 'roles', section: 'Conditions', help: 'Others never receive the component, or anything inside it.' },
  { key: 'showWhen', label: 'Show when', kind: 'formula', section: 'Conditions', placeholder: 'form.Type = "Business"', help: 'A formula; the component shows while it is true. It only changes what the page shows: use Visible to and row rules to keep data from people.' },
  { key: 'conditions', label: 'Change settings when', kind: 'conditions', section: 'Conditions' },
  { key: 'printHide', label: 'Hide when printing', kind: 'boolean', section: 'Conditions' },
]

// Where a data component's rows come from.
const isQuery = (p: Record<string, any>) => String(p.table ?? '').startsWith('query:')

const source: Setting[] = [
  { key: 'table', label: 'Table', kind: 'table', section: 'Data', queries: true, help: 'Leave empty to use the rows of the data provider around this component.' },
  { key: 'filter', label: 'Filter', kind: 'filter', section: 'Data', show: (p) => !!p.table && !isQuery(p) },
  { key: 'sort', label: 'Sort by', kind: 'field', section: 'Data', show: (p) => !!p.table },
  { key: 'desc', label: 'Largest or newest first', kind: 'boolean', section: 'Data', show: (p) => !!p.table && !!p.sort },
]

const fieldBase = (kinds?: Setting['fieldTypes']): Setting[] => [
  { key: 'field', label: 'Field', kind: 'field', fieldTypes: kinds, help: 'The column of the form’s table this saves to. Outside a form, the value goes to {{ state.<field> }}.' },
  { key: 'label', label: 'Label', kind: 'text' },
  { key: 'placeholder', label: 'Placeholder', kind: 'text' },
  { key: 'help', label: 'Help text', kind: 'text' },
  { key: 'defaultValue', label: 'Default value', kind: 'text' },
  { key: 'required', label: 'Required', kind: 'boolean', section: 'Validation' },
  { key: 'disabled', label: 'Read only', kind: 'boolean', section: 'Validation' },
]

const textRules: Setting[] = [
  { key: 'minLength', label: 'Minimum length', kind: 'number', section: 'Validation' },
  { key: 'maxLength', label: 'Maximum length', kind: 'number', section: 'Validation' },
  { key: 'pattern', label: 'Pattern (regular expression)', kind: 'text', section: 'Validation', placeholder: '[A-Z]{3}-\\d+' },
  { key: 'patternMessage', label: 'Message when the pattern fails', kind: 'text', section: 'Validation' },
]

const numberRules: Setting[] = [
  { key: 'min', label: 'Minimum', kind: 'number', section: 'Validation' },
  { key: 'max', label: 'Maximum', kind: 'number', section: 'Validation' },
]

const choiceOptions: Setting = { key: 'options', label: 'Options', kind: 'options', help: 'Leave empty to use the options of the field.' }

const formParents = ['form', 'container', 'grid', 'card', 'tab', 'accordion_item', 'dialog', 'side_panel', 'row_provider', 'data_provider']

const defs: ComponentDef[] = [
  // Layout
  {
    type: 'container', label: 'Container', category: 'Layout', icon: SquareDashedIcon, container: true, component: Container,
    description: 'Stacks components in a column or a row.',
    defaults: () => ({ direction: 'column', gap: 'md', padding: 'none', align: 'stretch', justify: 'start', background: 'none', maxWidth: 'none' }),
    settings: [
      { key: 'direction', label: 'Direction', kind: 'segmented', options: opts(['column', 'Column'], ['row', 'Row']), section: 'Layout' },
      gap,
      { key: 'padding', label: 'Padding', kind: 'segmented', options: sizes, section: 'Layout' },
      { key: 'align', label: 'Align', kind: 'segmented', options: opts(['start', 'Start'], ['center', 'Center'], ['end', 'End'], ['stretch', 'Fill']), section: 'Layout' },
      { key: 'justify', label: 'Justify', kind: 'segmented', options: opts(['start', 'Start'], ['center', 'Center'], ['end', 'End'], ['between', 'Spread']), section: 'Layout' },
      { key: 'wrap', label: 'Wrap onto new lines', kind: 'boolean', section: 'Layout', show: (p) => p.direction === 'row' },
      { key: 'maxWidth', label: 'Maximum width', kind: 'segmented', options: opts(['none', 'Full'], ['sm', 'S'], ['md', 'M'], ['lg', 'L'], ['xl', 'XL']), section: 'Layout' },
      { key: 'background', label: 'Background', kind: 'select', options: opts(['none', 'None'], ['card', 'Card'], ['muted', 'Muted'], ['primary', 'Accent colour']), section: 'Style' },
    ],
  },
  {
    type: 'grid', label: 'Grid', category: 'Layout', icon: LayoutGridIcon, container: true, component: Grid,
    description: 'Lays components out in columns that stack on phones.',
    defaults: () => ({ columns: '3', gap: 'md' }),
    settings: [{ key: 'columns', label: 'Columns', kind: 'segmented', options: opts(['1', '1'], ['2', '2'], ['3', '3'], ['4', '4'], ['5', '5'], ['6', '6']), section: 'Layout' }, gap],
  },
  {
    type: 'card', label: 'Card', category: 'Layout', icon: SquareIcon, container: true, innerLayout: true, component: Card,
    description: 'A bordered panel with a title.',
    defaults: () => ({ title: 'Card title', description: '', gap: 'md' }),
    settings: [{ key: 'title', label: 'Title', kind: 'text' }, { key: 'description', label: 'Description', kind: 'text' }, gap],
  },
  {
    type: 'tabs', label: 'Tabs', category: 'Layout', icon: PanelTopIcon, container: true, childTypes: ['tab'], component: Tabs,
    description: 'Shows one tab of content at a time.',
    defaults: () => ({ fullWidth: false }),
    children: () => [newNode('tab', { title: 'Overview' }), newNode('tab', { title: 'Details' })],
    settings: [{ key: 'fullWidth', label: 'Stretch the tab bar', kind: 'boolean' }],
  },
  {
    type: 'tab', label: 'Tab', category: 'Layout', icon: PanelTopIcon, container: true, parents: ['tabs'], component: Tab,
    description: 'One tab of a Tabs component.',
    defaults: () => ({ title: 'Tab', gap: 'md' }),
    settings: [{ key: 'title', label: 'Title', kind: 'text' }, gap],
  },
  {
    type: 'accordion', label: 'Accordion', category: 'Layout', icon: ListCollapseIcon, container: true, childTypes: ['accordion_item'], component: Accordion,
    description: 'Sections that open and close.',
    defaults: () => ({ bordered: true }),
    children: () => [newNode('accordion_item', { title: 'First section' }), newNode('accordion_item', { title: 'Second section' })],
    settings: [{ key: 'bordered', label: 'Border', kind: 'boolean' }],
  },
  {
    type: 'accordion_item', label: 'Section', category: 'Layout', icon: ListCollapseIcon, container: true, parents: ['accordion'], component: Tab,
    description: 'One section of an accordion.',
    defaults: () => ({ title: 'Section', gap: 'md' }),
    settings: [{ key: 'title', label: 'Title', kind: 'text' }, gap],
  },
  {
    type: 'divider', label: 'Divider', category: 'Layout', icon: SeparatorHorizontalIcon, component: Divider,
    description: 'A line between parts of a screen.',
    defaults: () => ({ label: '' }),
    settings: [{ key: 'label', label: 'Label', kind: 'text' }],
  },
  {
    type: 'spacer', label: 'Spacer', category: 'Layout', icon: MoveVerticalIcon, component: Spacer,
    description: 'Empty space. In a row, it pushes what follows to the end.',
    defaults: () => ({ size: 'md', grow: false }),
    settings: [{ key: 'size', label: 'Height', kind: 'segmented', options: sizes.slice(1) }],
  },

  // Display
  {
    type: 'heading', label: 'Heading', category: 'Display', icon: HeadingIcon, component: Heading,
    description: 'A title, with an optional subtitle.',
    defaults: () => ({ text: 'Heading', level: 'h2', align: 'left', tone: 'default', subtitle: '' }),
    settings: [
      { key: 'text', label: 'Text', kind: 'text' },
      { key: 'subtitle', label: 'Subtitle', kind: 'text' },
      { key: 'level', label: 'Size', kind: 'segmented', options: opts(['h1', 'XL'], ['h2', 'L'], ['h3', 'M'], ['h4', 'S']) },
      { key: 'align', label: 'Align', kind: 'segmented', options: opts(['left', 'Left'], ['center', 'Center'], ['right', 'Right']), section: 'Style' },
      { key: 'tone', label: 'Colour', kind: 'segmented', options: opts(['default', 'Default'], ['muted', 'Muted'], ['primary', 'Accent']), section: 'Style' },
    ],
  },
  {
    type: 'text', label: 'Text', category: 'Display', icon: PilcrowIcon, component: Text,
    description: 'A paragraph. Use {{ bindings }} to show data.',
    defaults: () => ({ text: 'Some text', size: 'sm', weight: 'normal', tone: 'default', align: 'left' }),
    settings: [
      { key: 'text', label: 'Text', kind: 'textarea' },
      { key: 'size', label: 'Size', kind: 'segmented', options: opts(['xs', 'XS'], ['sm', 'S'], ['base', 'M'], ['lg', 'L'], ['xl', 'XL']), section: 'Style' },
      { key: 'weight', label: 'Weight', kind: 'segmented', options: opts(['normal', 'Normal'], ['medium', 'Medium'], ['bold', 'Bold']), section: 'Style' },
      { key: 'tone', label: 'Colour', kind: 'segmented', options: opts(['default', 'Default'], ['muted', 'Muted'], ['destructive', 'Red']), section: 'Style' },
      { key: 'align', label: 'Align', kind: 'segmented', options: opts(['left', 'Left'], ['center', 'Center'], ['right', 'Right']), section: 'Style' },
    ],
  },
  {
    type: 'badge', label: 'Badge', category: 'Display', icon: TagIcon, component: Badge,
    description: 'A small label, such as a status.',
    defaults: () => ({ text: 'Badge', variant: 'secondary' }),
    settings: [
      { key: 'text', label: 'Text', kind: 'text' },
      { key: 'variant', label: 'Style', kind: 'segmented', options: opts(['default', 'Solid'], ['secondary', 'Soft'], ['outline', 'Outline'], ['destructive', 'Red']) },
    ],
  },
  {
    type: 'image', label: 'Image', category: 'Display', icon: ImageIcon, component: Image,
    description: 'A picture from a web address.',
    defaults: () => ({ src: '', alt: '', height: 'md', fit: 'cover', rounded: true }),
    settings: [
      { key: 'src', label: 'Address', kind: 'text', placeholder: 'https://… or {{ row.Photo }}' },
      { key: 'alt', label: 'Description for screen readers', kind: 'text' },
      { key: 'height', label: 'Height', kind: 'segmented', options: opts(['auto', 'Auto'], ['sm', 'S'], ['md', 'M'], ['lg', 'L'], ['xl', 'XL']), section: 'Style' },
      { key: 'fit', label: 'Fit', kind: 'segmented', options: opts(['cover', 'Fill'], ['contain', 'Fit']), section: 'Style' },
      { key: 'rounded', label: 'Rounded corners', kind: 'boolean', section: 'Style' },
    ],
  },
  {
    type: 'pdf_viewer', label: 'PDF viewer', category: 'Display', icon: FileTextIcon, component: PdfViewer,
    description: 'Shows a PDF from an attachment field or a web address, with pages, zoom and download.',
    defaults: () => ({ source: 'field', field: '', url: '', title: '', height: 'lg', zoom: 'fit', toolbar: true, allowDownload: true, emptyText: 'No PDF to show.' }),
    settings: [
      { key: 'source', label: 'Show', kind: 'segmented', options: opts(['field', 'A file field'], ['url', 'An address']) },
      { key: 'field', label: 'Attachment field', kind: 'field', fieldTypes: ['attachment'], show: (p) => p.source !== 'url', help: 'A field of the row around the viewer (row provider, repeater or form). When it holds several PDFs, the toolbar lets people switch.' },
      { key: 'url', label: 'Address', kind: 'text', placeholder: 'https://…/file.pdf or {{ row.Contract }}', show: (p) => p.source === 'url', help: 'Addresses on other sites must allow cross-origin reads.' },
      { key: 'title', label: 'Title', kind: 'text', help: 'Leave empty to show the file name.' },
      { key: 'emptyText', label: 'Text when there is no PDF', kind: 'text' },
      { key: 'height', label: 'Height', kind: 'segmented', options: opts(['sm', 'S'], ['md', 'M'], ['lg', 'L'], ['xl', 'XL'], ['screen', 'Screen']), section: 'Style' },
      { key: 'zoom', label: 'Zoom', kind: 'select', options: opts(['fit', 'Fit to width'], ['50', '50%'], ['75', '75%'], ['100', '100%'], ['125', '125%'], ['150', '150%'], ['200', '200%']), section: 'Style' },
      { key: 'toolbar', label: 'Toolbar', kind: 'boolean', section: 'Style' },
      { key: 'allowDownload', label: 'Download button', kind: 'boolean', section: 'Style', show: (p) => p.toolbar !== false },
    ],
  },
  {
    type: 'link', label: 'Link', category: 'Display', icon: LinkIcon, component: Link,
    description: 'Text that opens a screen or a web address.',
    defaults: () => ({ text: 'Link', url: '/', newTab: false }),
    settings: [
      { key: 'text', label: 'Text', kind: 'text' },
      { key: 'url', label: 'Screen route or web address', kind: 'text', placeholder: '/orders or https://…' },
      { key: 'newTab', label: 'Open web addresses in a new tab', kind: 'boolean' },
    ],
  },
  {
    type: 'button', label: 'Button', category: 'Display', icon: MousePointerClickIcon, component: Button,
    description: 'Runs actions when clicked: save, delete, open a dialog, go to a screen.',
    defaults: () => ({ text: 'Button', variant: 'default', size: 'default', icon: '', actions: [] }),
    settings: [
      { key: 'text', label: 'Text', kind: 'text' },
      { key: 'actions', label: 'When clicked', kind: 'actions' },
      { key: 'variant', label: 'Style', kind: 'select', options: opts(['default', 'Primary'], ['secondary', 'Secondary'], ['outline', 'Outline'], ['ghost', 'Ghost'], ['destructive', 'Destructive'], ['link', 'Link']), section: 'Style' },
      { key: 'size', label: 'Size', kind: 'segmented', options: opts(['sm', 'S'], ['default', 'M'], ['lg', 'L']), section: 'Style' },
      { key: 'icon', label: 'Icon', kind: 'icon', section: 'Style' },
      { key: 'fullWidth', label: 'Full width', kind: 'boolean', section: 'Style' },
      { key: 'tooltip', label: 'Tooltip', kind: 'text', section: 'Style' },
      { key: 'disabled', label: 'Disabled', kind: 'text', placeholder: 'true, or {{ binding }}', section: 'Style' },
    ],
  },
  {
    type: 'stat', label: 'Stat', category: 'Display', icon: GaugeIcon, component: Stat, tableSource: true,
    description: 'One key number: a count, a total or an average.',
    defaults: () => ({ title: 'Total', mode: 'rows', op: 'count', format: 'number', icon: '', caption: '' }),
    settings: [
      { key: 'title', label: 'Title', kind: 'text' },
      { key: 'mode', label: 'Value from', kind: 'segmented', options: opts(['rows', 'Rows'], ['value', 'Binding']) },
      { key: 'value', label: 'Value', kind: 'text', placeholder: '{{ data.total }}', show: (p) => p.mode === 'value' },
      ...source.map((s) => ({ ...s, show: (p: Record<string, any>) => p.mode !== 'value' && (!s.show || s.show(p)) })),
      { key: 'op', label: 'Calculate', kind: 'select', options: aggregateOptions, section: 'Data', show: (p) => p.mode !== 'value' },
      { key: 'field', label: 'Of field', kind: 'field', fieldTypes: ['number', 'formula', 'autonumber'], section: 'Data', show: (p) => p.mode !== 'value' && p.op !== 'count' },
      { key: 'format', label: 'Format', kind: 'segmented', options: opts(['number', '123'], ['decimal', '1.23'], ['currency', '$'], ['percent', '%']), section: 'Style' },
      { key: 'currency', label: 'Currency code', kind: 'text', placeholder: 'USD', section: 'Style', show: (p) => p.format === 'currency' },
      { key: 'prefix', label: 'Before the number', kind: 'text', section: 'Style' },
      { key: 'suffix', label: 'After the number', kind: 'text', section: 'Style' },
      { key: 'icon', label: 'Icon', kind: 'icon', section: 'Style' },
      { key: 'caption', label: 'Caption', kind: 'text', section: 'Style' },
    ],
  },
  {
    type: 'avatar', label: 'Avatar', category: 'Display', icon: CircleUserIcon, component: Avatar,
    description: 'A picture or initials for a person.',
    defaults: () => ({ name: '{{ user.name }}', image: '', size: 'md', showName: true, caption: '{{ user.email }}' }),
    settings: [
      { key: 'name', label: 'Name', kind: 'text' },
      { key: 'image', label: 'Picture address', kind: 'text' },
      { key: 'showName', label: 'Show the name', kind: 'boolean' },
      { key: 'caption', label: 'Caption', kind: 'text', show: (p) => !!p.showName },
      { key: 'size', label: 'Size', kind: 'segmented', options: opts(['sm', 'S'], ['md', 'M'], ['lg', 'L']), section: 'Style' },
    ],
  },

  // Feedback
  {
    type: 'alert', label: 'Alert', category: 'Feedback', icon: MegaphoneIcon, component: Alert,
    description: 'A highlighted message.',
    defaults: () => ({ title: 'Heads up', text: 'Something worth knowing.', variant: 'info' }),
    settings: [
      { key: 'title', label: 'Title', kind: 'text' },
      { key: 'text', label: 'Message', kind: 'textarea' },
      { key: 'variant', label: 'Kind', kind: 'segmented', options: opts(['info', 'Info'], ['success', 'Success'], ['warning', 'Warning'], ['destructive', 'Error']) },
    ],
  },
  {
    type: 'progress', label: 'Progress', category: 'Feedback', icon: LoaderIcon, component: Progress,
    description: 'A bar showing how far along something is.',
    defaults: () => ({ label: 'Progress', value: '40', max: '100', showValue: true }),
    settings: [
      { key: 'label', label: 'Label', kind: 'text' },
      { key: 'value', label: 'Value', kind: 'text', placeholder: '{{ row.Done }}' },
      { key: 'max', label: 'Maximum', kind: 'text' },
      { key: 'showValue', label: 'Show the percentage', kind: 'boolean' },
    ],
  },
  {
    type: 'empty_state', label: 'Empty state', category: 'Feedback', icon: InboxIcon, container: true, component: EmptyState,
    description: 'A friendly message for when there is nothing to show, with buttons.',
    defaults: () => ({ title: 'Nothing here yet', description: 'Add the first item to get started.', icon: 'folder' }),
    settings: [
      { key: 'title', label: 'Title', kind: 'text' },
      { key: 'description', label: 'Description', kind: 'text' },
      { key: 'icon', label: 'Icon', kind: 'icon' },
    ],
  },
  {
    type: 'dialog', label: 'Dialog', category: 'Feedback', icon: AppWindowIcon, container: true, innerLayout: true, component: Dialog,
    description: 'A window over the screen, opened by a button’s “Open dialog” action.',
    defaults: () => ({ title: 'Dialog', description: '', size: 'md', gap: 'md' }),
    settings: [
      { key: 'title', label: 'Title', kind: 'text' },
      { key: 'description', label: 'Description', kind: 'text' },
      { key: 'size', label: 'Width', kind: 'segmented', options: opts(['sm', 'S'], ['md', 'M'], ['lg', 'L'], ['xl', 'XL']) },
      gap,
    ],
  },
  {
    type: 'side_panel', label: 'Side panel', category: 'Feedback', icon: PanelRightIcon, container: true, innerLayout: true, component: SidePanel,
    description: 'A panel that slides in from the side, opened like a dialog.',
    defaults: () => ({ title: 'Details', description: '', side: 'right', gap: 'md' }),
    settings: [
      { key: 'title', label: 'Title', kind: 'text' },
      { key: 'description', label: 'Description', kind: 'text' },
      { key: 'side', label: 'Side', kind: 'segmented', options: opts(['left', 'Left'], ['right', 'Right']) },
      gap,
    ],
  },

  // Data
  {
    type: 'data_provider', label: 'Data provider', category: 'Data', icon: DatabaseIcon, container: true, component: DataProvider,
    description: 'Loads rows from a table or an API query once, for the tables, lists, charts and stats inside it ({{ data.rows }}, {{ data.total }}).',
    defaults: () => ({ table: '', filter: '', sort: '', desc: false, limit: 100, paginate: false, gap: 'md' }),
    settings: [
      { key: 'table', label: 'Table or query', kind: 'table', section: 'Data', queries: true },
      { key: 'filter', label: 'Filter', kind: 'filter', section: 'Data', show: (p) => !isQuery(p) },
      { key: 'sort', label: 'Sort by', kind: 'field', section: 'Data' },
      { key: 'desc', label: 'Largest or newest first', kind: 'boolean', section: 'Data', show: (p) => !!p.sort },
      { key: 'limit', label: 'Rows to load', kind: 'number', section: 'Data' },
      { key: 'paginate', label: 'Show page buttons', kind: 'boolean', section: 'Data' },
      gap,
    ],
  },
  {
    type: 'row_provider', label: 'Row provider', category: 'Data', icon: IdCardIcon, container: true, component: RowProvider,
    description: 'Loads one row, usually from the screen’s route ({{ url.id }}), for {{ row }} bindings and Details.',
    defaults: () => ({ table: '', rowId: '{{ url.id }}', gap: 'md' }),
    settings: [
      { key: 'table', label: 'Table', kind: 'table', section: 'Data' },
      { key: 'rowId', label: 'Row id', kind: 'text', section: 'Data', placeholder: '{{ url.id }}' },
      gap,
    ],
  },
  {
    type: 'table', label: 'Table', category: 'Data', icon: TableIcon, component: DataTable, tableSource: true,
    description: 'A data grid with sorting, search, paging, selection and export.',
    defaults: () => ({ table: '', filter: '', columns: [], pageSize: '10', search: true, sortable: true, allowDelete: false, allowExport: false, density: 'normal', striped: false, onRowClick: [], emptyText: 'No rows yet' }),
    settings: [
      ...source,
      { key: 'columns', label: 'Columns', kind: 'fields', help: 'Leave empty to show the first eight fields.' },
      { key: 'title', label: 'Title', kind: 'text' },
      { key: 'search', label: 'Search box', kind: 'boolean' },
      { key: 'sortable', label: 'Sort by clicking a column', kind: 'boolean' },
      { key: 'allowDelete', label: 'Select and delete rows', kind: 'boolean' },
      { key: 'allowExport', label: 'Export to CSV', kind: 'boolean' },
      { key: 'onRowClick', label: 'When a row is clicked', kind: 'actions' },
      { key: 'pageSize', label: 'Rows per page', kind: 'segmented', options: opts(['5', '5'], ['10', '10'], ['25', '25'], ['50', '50'], ['100', '100']), section: 'Style' },
      { key: 'density', label: 'Density', kind: 'segmented', options: opts(['compact', 'Compact'], ['normal', 'Normal']), section: 'Style' },
      { key: 'striped', label: 'Striped rows', kind: 'boolean', section: 'Style' },
      { key: 'emptyText', label: 'Text when empty', kind: 'text', section: 'Style' },
    ],
  },
  {
    type: 'list', label: 'List', category: 'Data', icon: LayoutListIcon, component: List, tableSource: true,
    description: 'Rows as a list with a title, subtitle and badge.',
    defaults: () => ({ table: '', titleField: '', subtitleField: '', badgeField: '', metaField: '', showAvatar: true, onRowClick: [], emptyText: 'Nothing here yet', limit: 20 }),
    settings: [
      ...source,
      { key: 'limit', label: 'Rows to show', kind: 'number', section: 'Data', show: (p) => !!p.table },
      { key: 'title', label: 'Title', kind: 'text' },
      { key: 'titleField', label: 'Title field', kind: 'field' },
      { key: 'subtitleField', label: 'Subtitle field', kind: 'field' },
      { key: 'badgeField', label: 'Badge field', kind: 'field' },
      { key: 'metaField', label: 'Right-hand field', kind: 'field' },
      { key: 'showAvatar', label: 'Show initials', kind: 'boolean' },
      { key: 'imageField', label: 'Picture field', kind: 'field', show: (p) => !!p.showAvatar },
      { key: 'onRowClick', label: 'When an item is clicked', kind: 'actions' },
      { key: 'emptyText', label: 'Text when empty', kind: 'text', section: 'Style' },
    ],
  },
  {
    type: 'cards', label: 'Card grid', category: 'Data', icon: WalletCardsIcon, component: Cards, tableSource: true,
    description: 'A card for every row, with a picture, title and chosen fields.',
    defaults: () => ({ table: '', columns: '3', titleField: '', subtitleField: '', bodyField: '', badgeField: '', imageField: '', fields: [], onRowClick: [], limit: 12, emptyText: 'Nothing here yet' }),
    settings: [
      ...source,
      { key: 'limit', label: 'Cards to show', kind: 'number', section: 'Data', show: (p) => !!p.table },
      { key: 'titleField', label: 'Title field', kind: 'field' },
      { key: 'subtitleField', label: 'Subtitle field', kind: 'field' },
      { key: 'bodyField', label: 'Body field', kind: 'field' },
      { key: 'badgeField', label: 'Badge field', kind: 'field' },
      { key: 'imageField', label: 'Picture field', kind: 'field' },
      { key: 'fields', label: 'More fields', kind: 'fields' },
      { key: 'onRowClick', label: 'When a card is clicked', kind: 'actions' },
      { key: 'columns', label: 'Columns', kind: 'segmented', options: opts(['1', '1'], ['2', '2'], ['3', '3'], ['4', '4']), section: 'Style' },
      { key: 'emptyText', label: 'Text when empty', kind: 'text', section: 'Style' },
    ],
  },
  {
    type: 'chart', label: 'Chart', category: 'Data', icon: ChartColumnIcon, component: Chart, tableSource: true,
    description: 'Bar, line, area, pie and donut charts that group and total rows.',
    defaults: () => ({ table: '', title: 'Chart', description: '', chartType: 'bar', labelField: '', op: 'count', valueFields: [], dateGroup: 'month', sortBy: 'label', limitGroups: 0, height: 'md', legend: true, palette: 'app' }),
    settings: [
      ...source,
      { key: 'title', label: 'Title', kind: 'text' },
      { key: 'description', label: 'Description', kind: 'text' },
      { key: 'chartType', label: 'Chart', kind: 'select', options: opts(['bar', 'Bar'], ['horizontal_bar', 'Horizontal bar'], ['line', 'Line'], ['area', 'Area'], ['pie', 'Pie'], ['donut', 'Donut']) },
      { key: 'labelField', label: 'Group by', kind: 'field' },
      { key: 'dateGroup', label: 'Group dates by', kind: 'segmented', options: opts(['day', 'Day'], ['month', 'Month'], ['year', 'Year']) },
      { key: 'op', label: 'Value', kind: 'select', options: aggregateOptions },
      { key: 'valueFields', label: 'Of fields', kind: 'fields', fieldTypes: ['number', 'formula', 'autonumber'], show: (p) => p.op !== 'count', help: 'Several fields draw several series (bar, line and area).' },
      { key: 'sortBy', label: 'Order', kind: 'segmented', options: opts(['label', 'By group'], ['value', 'Largest first']) },
      { key: 'limitGroups', label: 'Show at most (0 shows all)', kind: 'number' },
      { key: 'height', label: 'Height', kind: 'segmented', options: opts(['sm', 'S'], ['md', 'M'], ['lg', 'L'], ['xl', 'XL']), section: 'Style' },
      { key: 'legend', label: 'Legend', kind: 'boolean', section: 'Style' },
      { key: 'palette', label: 'Colours', kind: 'select', section: 'Style', options: [{ value: 'app', label: "The app's chart colours" }, ...chartPalettes.map((x) => ({ value: x.id, label: x.label }))] },
      { key: 'colors', label: 'Colour of each', kind: 'chartColors', section: 'Style' },
    ],
  },
  {
    type: 'repeater', label: 'Repeater', category: 'Data', icon: RepeatIcon, container: true, component: Repeater, tableSource: true,
    description: 'Repeats the components inside it for every row, with {{ row }} bindings.',
    defaults: () => ({ table: '', layout: 'column', columns: '3', gap: 'md', limit: 20, emptyText: 'Nothing here yet' }),
    settings: [
      ...source,
      { key: 'limit', label: 'Rows to show', kind: 'number', section: 'Data', show: (p) => !!p.table },
      { key: 'layout', label: 'Layout', kind: 'segmented', options: opts(['column', 'Column'], ['row', 'Row'], ['grid', 'Grid']), section: 'Layout' },
      { key: 'columns', label: 'Columns', kind: 'segmented', options: opts(['2', '2'], ['3', '3'], ['4', '4']), section: 'Layout', show: (p) => p.layout === 'grid' },
      gap,
      { key: 'emptyText', label: 'Text when empty', kind: 'text' },
    ],
  },
  {
    type: 'details', label: 'Details', category: 'Data', icon: ClipboardListIcon, component: Details,
    description: 'Shows the fields of the current row as labelled values.',
    defaults: () => ({ title: '', fields: [], columns: '2', card: true }),
    settings: [
      { key: 'title', label: 'Title', kind: 'text' },
      { key: 'fields', label: 'Fields', kind: 'fields', help: 'Leave empty to show every field.' },
      { key: 'columns', label: 'Columns', kind: 'segmented', options: opts(['1', '1'], ['2', '2'], ['3', '3']), section: 'Style' },
      { key: 'card', label: 'In a card', kind: 'boolean', section: 'Style' },
    ],
  },

  // Forms
  {
    type: 'form', label: 'Form', category: 'Form', icon: FormIcon, container: true, innerLayout: true, component: Form,
    description: 'Adds or edits a row, checking each field before it saves.',
    defaults: () => ({ table: '', mode: 'create', rowId: '{{ url.id }}', columns: '1', gap: 'md', submitText: 'Save', successMessage: 'Saved', showSubmit: true, showReset: false, resetOnSubmit: true, buttonAlign: 'end', onSubmit: [] }),
    settings: [
      { key: 'table', label: 'Table', kind: 'table', section: 'Data' },
      { key: 'mode', label: 'Form', kind: 'segmented', options: opts(['create', 'Adds a row'], ['update', 'Edits a row']), section: 'Data' },
      { key: 'rowId', label: 'Row id', kind: 'text', section: 'Data', placeholder: '{{ url.id }}', show: (p) => p.mode === 'update' },
      { key: 'submitText', label: 'Button text', kind: 'text' },
      { key: 'successMessage', label: 'Notification after saving', kind: 'text' },
      { key: 'onSubmit', label: 'After saving', kind: 'actions' },
      { key: 'resetOnSubmit', label: 'Clear after adding', kind: 'boolean', show: (p) => p.mode !== 'update' },
      { key: 'showSubmit', label: 'Show the save button', kind: 'boolean' },
      { key: 'showReset', label: 'Show a reset button', kind: 'boolean' },
      { key: 'columns', label: 'Columns', kind: 'segmented', options: opts(['1', '1'], ['2', '2'], ['3', '3']), section: 'Layout' },
      gap,
      { key: 'buttonAlign', label: 'Buttons', kind: 'segmented', options: opts(['start', 'Left'], ['end', 'Right'], ['stretch', 'Full']), section: 'Layout' },
    ],
  },
  {
    type: 'text_input', label: 'Text field', category: 'Form', icon: TextCursorInputIcon, component: TextField, parents: formParents,
    description: 'One line of text, an email, a web address, a phone number or a password.',
    defaults: () => ({ field: '', label: '', placeholder: '', inputType: 'text' }),
    settings: [...fieldBase(['text', 'longtext']), { key: 'inputType', label: 'Kind', kind: 'select', options: opts(['text', 'Text'], ['email', 'Email'], ['url', 'Web address'], ['tel', 'Phone'], ['password', 'Password']) }, ...textRules],
  },
  {
    type: 'textarea_input', label: 'Long text field', category: 'Form', icon: WholeWordIcon, component: TextareaField, parents: formParents,
    description: 'Several lines of text.',
    defaults: () => ({ field: '', label: '', placeholder: '', rows: 4 }),
    settings: [...fieldBase(['text', 'longtext']), { key: 'rows', label: 'Lines', kind: 'number' }, ...textRules],
  },
  {
    type: 'number_input', label: 'Number field', category: 'Form', icon: HashIcon, component: NumberField, parents: formParents,
    description: 'A number, with optional limits.',
    defaults: () => ({ field: '', label: '', placeholder: '', step: '' }),
    settings: [...fieldBase(['number']), { key: 'step', label: 'Step', kind: 'text', placeholder: 'any' }, { key: 'prefix', label: 'Prefix', kind: 'text', placeholder: '$' }, ...numberRules],
  },
  {
    type: 'select_input', label: 'Select', category: 'Form', icon: ChevronDownIcon, component: SelectField, parents: formParents,
    description: 'Choose one option from a dropdown.',
    defaults: () => ({ field: '', label: '', placeholder: '', options: [] }),
    settings: [...fieldBase(['single_select', 'text']), choiceOptions],
  },
  {
    type: 'radio_input', label: 'Radio buttons', category: 'Form', icon: CircleDotIcon, component: RadioField, parents: formParents,
    description: 'Choose one option, all visible at once.',
    defaults: () => ({ field: '', label: '', options: [], direction: 'column' }),
    settings: [...fieldBase(['single_select', 'text']), choiceOptions, { key: 'direction', label: 'Layout', kind: 'segmented', options: opts(['column', 'Column'], ['row', 'Row']) }],
  },
  {
    type: 'checkbox_group_input', label: 'Checkbox group', category: 'Form', icon: ListChecksIcon, component: CheckboxGroupField, parents: formParents,
    description: 'Choose several options.',
    defaults: () => ({ field: '', label: '', options: [], direction: 'column' }),
    settings: [...fieldBase(['multi_select']), choiceOptions, { key: 'direction', label: 'Layout', kind: 'segmented', options: opts(['column', 'Column'], ['row', 'Row']) }],
  },
  {
    type: 'date_input', label: 'Date picker', category: 'Form', icon: CalendarIcon, component: DateField, parents: formParents,
    description: 'A calendar to pick a day, and optionally a time.',
    defaults: () => ({ field: '', label: '', placeholder: '', includeTime: false }),
    settings: [...fieldBase(['date']), { key: 'includeTime', label: 'Include a time', kind: 'boolean' }],
  },
  {
    type: 'checkbox_input', label: 'Checkbox', category: 'Form', icon: SquareCheckIcon, component: CheckboxField, parents: formParents,
    description: 'A yes or no tick box.',
    defaults: () => ({ field: '', label: '' }),
    settings: fieldBase(['boolean']).filter((s) => s.key !== 'placeholder'),
  },
  {
    type: 'switch_input', label: 'Switch', category: 'Form', icon: ToggleRightIcon, component: SwitchField, parents: formParents,
    description: 'A yes or no toggle.',
    defaults: () => ({ field: '', label: '' }),
    settings: fieldBase(['boolean']).filter((s) => s.key !== 'placeholder'),
  },
  {
    type: 'slider_input', label: 'Slider', category: 'Form', icon: SlidersHorizontalIcon, component: SliderField, parents: formParents,
    description: 'Pick a number by sliding.',
    defaults: () => ({ field: '', label: '', min: 0, max: 100, step: 1 }),
    settings: [...fieldBase(['number']).filter((s) => s.key !== 'placeholder'), { key: 'min', label: 'Minimum', kind: 'number' }, { key: 'max', label: 'Maximum', kind: 'number' }, { key: 'step', label: 'Step', kind: 'number' }],
  },
  {
    type: 'file_input', label: 'File upload', category: 'Form', icon: PaperclipIcon, component: FileField, parents: formParents,
    description: 'Upload PDFs, pictures or other files into an attachment field.',
    defaults: () => ({ field: '', label: '', placeholder: '', accept: 'pdf', multiple: true, maxSizeMB: 25 }),
    settings: [
      ...fieldBase(['attachment']).filter((s) => s.key !== 'defaultValue'),
      { key: 'accept', label: 'Accept', kind: 'segmented', options: opts(['pdf', 'PDF'], ['images', 'Pictures'], ['any', 'Any file']) },
      { key: 'multiple', label: 'Several files', kind: 'boolean' },
      { key: 'maxSizeMB', label: 'Largest file (MB, at most 25)', kind: 'number', section: 'Validation' },
    ],
  },
  {
    type: 'link_input', label: 'Relationship picker', category: 'Form', icon: Link2Icon, component: LinkField, parents: formParents,
    description: 'Choose linked rows of another table, with search.',
    defaults: () => ({ field: '', label: '', placeholder: '' }),
    settings: fieldBase(['link']),
  },
]

for (const d of defs) d.settings = [...d.settings, ...commonSettings]

export const registry: Record<string, ComponentDef> = Object.fromEntries(defs.map((d) => [d.type, d]))
export const componentList = defs
export const categories = ['Layout', 'Display', 'Data', 'Form', 'Feedback'] as const

let counter = 0
// newId makes a component id that is unique within a screen.
export function newId(type: string): string {
  counter = (counter + 1) % 1e6
  return `${type.replace(/_/g, '')}-${Date.now().toString(36).slice(-5)}${Math.random().toString(36).slice(2, 6)}${counter}`
}

// newNode makes a component with its default settings and children.
export function newNode(type: string, props: Record<string, any> = {}) {
  const d = registry[type]
  return { id: newId(type), type, props: { ...d?.defaults(), ...props }, children: d?.children?.() ?? [] }
}

// fieldComponent picks the form field that suits a table field.
export function fieldComponent(type: string): string | null {
  switch (type) {
    case 'text': return 'text_input'
    case 'longtext': return 'textarea_input'
    case 'number': return 'number_input'
    case 'boolean': return 'switch_input'
    case 'date': return 'date_input'
    case 'single_select': return 'select_input'
    case 'multi_select': return 'checkbox_group_input'
    case 'link': return 'link_input'
    case 'attachment': return 'file_input'
  }
  return null
}
