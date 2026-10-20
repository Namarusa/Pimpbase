import type { Field } from '../api'

const opLabel: Record<string, string> = { count: 'Count', sum: 'Total', avg: 'Average', min: 'Minimum', max: 'Maximum' }

// seriesLabel names the series a chart draws for one value field.
export function seriesLabel(op: string | undefined, field: string): string {
  return (op ?? 'count') === 'count' ? 'Rows' : `${opLabel[op ?? ''] ?? ''} ${field}`.trim()
}

// colorItems lists what a chart's colours can be set for, as far as the
// settings alone tell: the slices of a pie or donut grouped by a select
// or yes/no field, or the series of the other charts.
export function colorItems(p: Record<string, any>, fields: Field[]): string[] {
  if (p.chartType === 'pie' || p.chartType === 'donut') {
    const f = fields.find((x) => x.name === p.labelField)
    if (f?.type === 'single_select' || f?.type === 'multi_select') return f.options ?? []
    if (f?.type === 'boolean') return ['Yes', 'No']
    return []
  }
  const values: string[] = Array.isArray(p.valueFields) && p.valueFields.length ? p.valueFields : ['']
  return values.map((f) => seriesLabel(p.op, f))
}
