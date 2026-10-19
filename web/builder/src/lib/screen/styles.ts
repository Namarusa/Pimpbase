// Tailwind classes for the size and layout choices builders make. Every
// class is written out in full so Tailwind finds it.

export const gapClass: Record<string, string> = { none: 'gap-0', sm: 'gap-2', md: 'gap-4', lg: 'gap-6', xl: 'gap-10' }
export const padClass: Record<string, string> = { none: 'p-0', sm: 'p-2', md: 'p-4', lg: 'p-6', xl: 'p-10' }
export const alignClass: Record<string, string> = { start: 'items-start', center: 'items-center', end: 'items-end', stretch: 'items-stretch' }
export const justifyClass: Record<string, string> = { start: 'justify-start', center: 'justify-center', end: 'justify-end', between: 'justify-between' }
export const textAlignClass: Record<string, string> = { left: 'text-left', center: 'text-center', right: 'text-right' }
export const bgClass: Record<string, string> = {
  none: '',
  card: 'bg-card text-card-foreground border rounded-xl shadow-xs',
  muted: 'bg-muted/60 rounded-xl',
  primary: 'bg-primary text-primary-foreground rounded-xl',
}
export const maxWidthClass: Record<string, string> = { none: '', sm: 'max-w-xl mx-auto w-full', md: 'max-w-3xl mx-auto w-full', lg: 'max-w-5xl mx-auto w-full', xl: 'max-w-7xl mx-auto w-full' }

// Grid columns collapse to one column when the screen is narrow. They
// follow the width of the screen's container, so the builder's phone and
// tablet previews behave like real devices.
export const gridColsClass: Record<string, string> = {
  '1': 'grid-cols-1',
  '2': 'grid-cols-1 @md:grid-cols-2',
  '3': 'grid-cols-1 @md:grid-cols-2 @xl:grid-cols-3',
  '4': 'grid-cols-1 @md:grid-cols-2 @3xl:grid-cols-4',
  '5': 'grid-cols-1 @md:grid-cols-3 @3xl:grid-cols-5',
  '6': 'grid-cols-2 @md:grid-cols-3 @3xl:grid-cols-6',
}
export const spanClass: Record<string, string> = {
  '1': '',
  '2': '@md:col-span-2',
  '3': '@xl:col-span-3',
  '4': '@3xl:col-span-4',
  full: 'col-span-full',
}

export const sizes = [
  { value: 'none', label: 'None' },
  { value: 'sm', label: 'S' },
  { value: 'md', label: 'M' },
  { value: 'lg', label: 'L' },
  { value: 'xl', label: 'XL' },
]
