// Package tables implements the built-in tables of the data studio: their
// schema, the SQL tables behind them, and row reads and writes with formula
// fields and permission checks.
package tables

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bubibase/bubibase/internal/formula"
	"github.com/bubibase/bubibase/internal/rbac"
)

// FieldType is the kind of a field.
type FieldType string

const (
	TypeText         FieldType = "text"
	TypeLongText     FieldType = "longtext"
	TypeNumber       FieldType = "number"
	TypeBoolean      FieldType = "boolean"
	TypeDate         FieldType = "date"
	TypeSingleSelect FieldType = "single_select"
	TypeMultiSelect  FieldType = "multi_select"
	TypeLink         FieldType = "link"
	TypeAutoNumber   FieldType = "autonumber"
	TypeFormula      FieldType = "formula"
)

var allTypes = []FieldType{TypeText, TypeLongText, TypeNumber, TypeBoolean, TypeDate, TypeSingleSelect, TypeMultiSelect, TypeLink, TypeAutoNumber, TypeFormula}

// scalar types can be converted into each other in place.
func (t FieldType) scalar() bool {
	switch t {
	case TypeText, TypeLongText, TypeNumber, TypeBoolean, TypeDate, TypeSingleSelect:
		return true
	}
	return false
}

// Formula modes.
const (
	Dynamic = "dynamic" // computed when read
	Static  = "static"  // computed when the row is written, and stored
)

// Field is one column of a table.
type Field struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Type     FieldType `json:"type"`
	Required bool      `json:"required,omitempty"`
	// Options are the choices of a select field.
	Options []string `json:"options,omitempty"`
	// Link fields point at rows of another table in the same app.
	LinkTable string `json:"linkTable,omitempty"`
	LinkMany  bool   `json:"linkMany,omitempty"`
	// Formula fields.
	Formula     string       `json:"formula,omitempty"`
	FormulaMode string       `json:"formulaMode,omitempty"`
	ResultType  formula.Type `json:"resultType,omitempty"`
	// Column is the SQL column (or join table suffix) holding the data.
	Column string `json:"column,omitempty"`
}

// stored reports whether the field has its own column.
func (f *Field) stored() bool {
	switch f.Type {
	case TypeFormula:
		return f.FormulaMode == Static
	case TypeLink:
		return !f.LinkMany
	}
	return true
}

// writable reports whether rows may set the field.
func (f *Field) writable() bool {
	return f.Type != TypeFormula && f.Type != TypeAutoNumber
}

// Permissions say which app role may read and write a table's rows.
type Permissions struct {
	Read  rbac.Role `json:"read"`
	Write rbac.Role `json:"write"`
}

// Table is a built-in table.
type Table struct {
	ID           string      `json:"id"`
	AppID        string      `json:"appId"`
	Name         string      `json:"name"`
	Fields       []*Field    `json:"fields"`
	PrimaryField string      `json:"primaryField,omitempty"` // field id shown as the row label
	Permissions  Permissions `json:"permissions"`
	CreatedAt    time.Time   `json:"createdAt"`
	UpdatedAt    time.Time   `json:"updatedAt"`
}

func (t *Table) sqlName() string { return "t_" + strings.TrimPrefix(t.ID, "tbl_") }

func joinName(f *Field) string { return "j_" + f.Column }

// FieldByName finds a field by name, ignoring case.
func (t *Table) FieldByName(name string) *Field {
	for _, f := range t.Fields {
		if strings.EqualFold(f.Name, name) {
			return f
		}
	}
	return nil
}

func (t *Table) FieldByID(id string) *Field {
	for _, f := range t.Fields {
		if f.ID == id {
			return f
		}
	}
	return nil
}

func (t *Table) primary() *Field {
	if f := t.FieldByID(t.PrimaryField); f != nil {
		return f
	}
	return nil
}

// Errors returned to callers. ValidationError messages are safe to show.
var ErrNotFound = errors.New("not found")

type ValidationError struct {
	Field   string           `json:"field,omitempty"`
	Message string           `json:"message"`
	Formula []*formula.Error `json:"formulaErrors,omitempty"`
}

func (e *ValidationError) Error() string {
	if e.Field != "" {
		return e.Field + ": " + e.Message
	}
	return e.Message
}

func invalid(field, format string, args ...any) error {
	return &ValidationError{Field: field, Message: fmt.Sprintf(format, args...)}
}

func validName(kind, name string) error {
	name = strings.TrimSpace(name)
	switch {
	case name == "":
		return invalid("name", "%s name is required", kind)
	case len(name) > 100:
		return invalid("name", "%s name is too long", kind)
	case strings.ContainsAny(name, "[]"):
		return invalid("name", "%s name cannot contain [ or ]", kind)
	case strings.HasPrefix(name, "_"):
		return invalid("name", "%s name cannot start with _", kind)
	}
	return nil
}

// formulaType maps a field to the type formulas see.
func formulaType(f *Field) formula.Type {
	switch f.Type {
	case TypeText, TypeLongText, TypeSingleSelect:
		return formula.Text
	case TypeNumber, TypeAutoNumber:
		return formula.Number
	case TypeBoolean:
		return formula.Bool
	case TypeDate:
		return formula.Date
	case TypeMultiSelect:
		return formula.List
	case TypeLink:
		if f.LinkMany {
			return formula.List
		}
		return formula.Text
	case TypeFormula:
		return f.ResultType
	}
	return formula.Any
}
