// FieldBinding connects a form field component to the form around it, or
// to app state when it stands alone (a search box that filters a table).
import { getForm, getRuntime, getScope, type FieldRule } from './context.svelte'
import { resolve } from './bindings'
import type { Field } from '../api'
import type { Node } from './types'

export class FieldBinding {
  private form = getForm()
  private rt = getRuntime()
  private getP: () => Record<string, any>
  private node: Node

  constructor(node: Node, getP: () => Record<string, any>, kind: string) {
    this.node = node
    this.getP = getP
    const form = this.form
    const scope = getScope()
    // Start from the default value when the form has none.
    const p = getP()
    if (p.defaultValue !== undefined && p.defaultValue !== '' && this.value === undefined) {
      let d = resolve(p.defaultValue, scope())
      if (kind === 'number' && typeof d === 'string') d = d === '' ? null : Number(d)
      if ((kind === 'boolean') && typeof d === 'string') d = d === 'true'
      this.value = d
    }
    $effect(() => {
      if (!form) return
      const rule: FieldRule = {
        label: this.label,
        required: this.required,
        minLength: num(this.getP().minLength),
        maxLength: num(this.getP().maxLength),
        min: num(this.getP().min),
        max: num(this.getP().max),
        pattern: this.getP().pattern || undefined,
        patternMessage: this.getP().patternMessage || undefined,
        type: kind === 'text' ? this.getP().inputType : kind,
      }
      return form.register(this.name, rule)
    })
  }

  get name(): string {
    return this.getP().field || this.node.id
  }
  get field(): Field | undefined {
    return this.form?.table?.fields.find((f) => f.name === this.getP().field)
  }
  get label(): string {
    const l = this.getP().label
    return l === undefined || l === null || l === '' ? this.getP().field || 'Field' : String(l)
  }
  get required(): boolean {
    return !!this.getP().required || !!this.field?.required
  }
  get disabled(): boolean {
    const d = this.getP().disabled
    return d === true || d === 'true' || !!this.form?.submitting
  }
  get id(): string {
    return 'f-' + this.node.id
  }
  get error(): string {
    return this.form?.errors[this.name] ?? ''
  }
  get value(): any {
    if (this.form) return this.form.values[this.name]
    return this.rt.state[this.getP().stateKey || this.name]
  }
  set value(v: any) {
    if (this.form) {
      this.form.values[this.name] = v
      if (this.form.submitted || this.form.errors[this.name]) this.form.check(this.name)
    } else this.rt.state[this.getP().stateKey || this.name] = v
  }
  // touch checks the field when the person leaves it.
  touch() {
    if (this.form && this.value !== undefined) this.form.check(this.name)
  }
  // options are the choices of a select, radio or checkbox group: the
  // field's own options unless the builder typed some.
  get options(): string[] {
    const own = this.getP().options
    if (Array.isArray(own) && own.length) return own.map(String)
    return this.field?.options ?? []
  }
}

const num = (v: any) => (v === undefined || v === null || v === '' || isNaN(Number(v)) ? undefined : Number(v))
