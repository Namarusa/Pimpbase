// FieldBinding connects a form field component to the form around it, or
// to app state when it stands alone (a search box that filters a table).
import { getForm, getRuntime, getScope, shareValues, type FieldRule } from './context.svelte'
import { resolve } from './bindings'
import { runActions } from './actions'
import type { Field } from '../api'
import type { Node } from './types'

export class FieldBinding {
  private form = getForm()
  private rt = getRuntime()
  private getP: () => Record<string, any>
  private node: Node
  private kind: string
  private scope: ReturnType<typeof getScope>
  private timer: ReturnType<typeof setTimeout> | undefined
  private pending = false
  // el is the field's input, for the Focus action.
  el: HTMLElement | null = null

  constructor(node: Node, getP: () => Record<string, any>, kind: string) {
    this.node = node
    this.getP = getP
    this.kind = kind
    const form = this.form
    const scope = getScope()
    this.scope = scope
    // Start from the default value when the form has none.
    if (this.value === undefined) {
      const d = this.defaultValue()
      if (d !== undefined) this.store(d)
    }
    // Other components read this field's value as
    // {{ components["Name"].value }}; actions can set or clear it.
    shareValues(node.id, () => ({ value: this.value ?? null }), {
      setValue: (v) => this.store(this.coerce(v)),
      clear: () => this.store(this.defaultValue() ?? null),
      focus: () => this.el?.focus(),
    })
    $effect(() => () => clearTimeout(this.timer))
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
  // formState is the form around the field, if any.
  get formState() {
    return this.form
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
    this.store(v)
    this.changed()
  }
  // store keeps a value without running the field's change actions.
  private store(v: any) {
    if (this.form) {
      this.form.values[this.name] = v
      if (this.form.submitted || this.form.errors[this.name]) this.form.check(this.name)
    } else this.rt.state[this.getP().stateKey || this.name] = v
  }
  // touch checks the field when the person leaves it, and runs the
  // change actions a typed value was waiting for.
  touch() {
    if (this.form && this.value !== undefined) this.form.check(this.name)
    this.commit()
  }

  private get typed() {
    return this.kind === 'text' || this.kind === 'longtext' || this.kind === 'number'
  }

  // changed runs the "When the value changes" actions: at once for a
  // choice, after a pause in typing for text (or when the person leaves
  // the field or presses Enter).
  private changed() {
    if (!this.getP().onChange?.length) return
    this.pending = true
    clearTimeout(this.timer)
    const wait = this.typed ? 500 : 0
    if (wait) this.timer = setTimeout(() => this.commit(), wait)
    else this.commit()
  }

  commit() {
    clearTimeout(this.timer)
    if (!this.pending) return
    this.pending = false
    void runActions(this.getP().onChange, this.rt, this.scope(), this.actionScope())
  }

  // enter runs the "When Enter is pressed" actions of a text field.
  enter() {
    this.commit()
    const actions = this.getP().onEnter
    if (actions?.length) void runActions(actions, this.rt, this.scope(), this.actionScope())
  }

  private actionScope() {
    return this.form ? { value: this.value, form: this.form.values } : { value: this.value }
  }

  private defaultValue(): any {
    const p = this.getP()
    if (p.defaultValue === undefined || p.defaultValue === '') return undefined
    return this.coerce(resolve(p.defaultValue, this.scope()))
  }

  private coerce(d: any): any {
    if (this.kind === 'number' && typeof d === 'string') return d === '' ? null : Number(d)
    if (this.kind === 'boolean' && typeof d === 'string') return d === 'true' || d === 'Yes'
    return d
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
