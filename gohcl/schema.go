// Copyright IBM Corp. 2014, 2025
// SPDX-License-Identifier: MPL-2.0

package gohcl

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
)

// ImpliedBodySchema produces a hcl.BodySchema derived from the type of the
// given value, which must be a struct value or a pointer to one. If an
// inappropriate value is passed, this function will panic.
//
// The second return argument indicates whether the given struct includes
// a "remain" field, and thus the returned schema is non-exhaustive.
//
// This uses the tags on the fields of the struct to discover how each
// field's value should be expressed within configuration. If an invalid
// mapping is attempted, this function will panic.
func ImpliedBodySchema(val interface{}) (schema *hcl.BodySchema, partial bool) {
	ty := reflect.TypeOf(val)

	if ty.Kind() == reflect.Ptr {
		ty = ty.Elem()
	}

	if ty.Kind() != reflect.Struct {
		panic(fmt.Sprintf("given value must be struct, not %T", val))
	}

	var attrSchemas []hcl.AttributeSchema
	var blockSchemas []hcl.BlockHeaderSchema

	tags := getFieldTags(ty)

	attrNames := make([]string, 0, len(tags.Attributes))
	for n := range tags.Attributes {
		attrNames = append(attrNames, n)
	}
	sort.Strings(attrNames)
	for _, n := range attrNames {
		idx := tags.Attributes[n]
		optional := tags.Optional[n]
		field := typeByIndex(ty, idx)

		var required bool

		switch {
		case field.Type.AssignableTo(exprType):
			// If we're decoding to hcl.Expression then absense can be
			// indicated via a null value, so we don't specify that
			// the field is required during decoding.
			required = false
		case field.Type.Kind() != reflect.Ptr && !optional:
			required = true
		default:
			required = false
		}

		attrSchemas = append(attrSchemas, hcl.AttributeSchema{
			Name:     n,
			Required: required,
		})
	}

	blockNames := make([]string, 0, len(tags.Blocks))
	for n := range tags.Blocks {
		blockNames = append(blockNames, n)
	}
	sort.Strings(blockNames)
	for _, n := range blockNames {
		idx := tags.Blocks[n]
		field := typeByIndex(ty, idx)
		fty := field.Type
		if fty.Kind() == reflect.Slice {
			fty = fty.Elem()
		}
		if fty.Kind() == reflect.Ptr {
			fty = fty.Elem()
		}
		if fty.Kind() != reflect.Struct {
			panic(fmt.Sprintf(
				"hcl 'block' tag kind cannot be applied to %s field %s: struct required", field.Type.String(), field.Name,
			))
		}
		ftags := getFieldTags(fty)
		var labelNames []string
		if len(ftags.Labels) > 0 {
			labelNames = make([]string, len(ftags.Labels))
			for i, l := range ftags.Labels {
				labelNames[i] = l.Name
			}
		}

		blockSchemas = append(blockSchemas, hcl.BlockHeaderSchema{
			Type:       n,
			LabelNames: labelNames,
		})
	}

	partial = tags.Remain != nil
	schema = &hcl.BodySchema{
		Attributes: attrSchemas,
		Blocks:     blockSchemas,
	}
	return schema, partial
}

type fieldTags struct {
	Attributes map[string][]int
	Blocks     map[string][]int
	Labels     []labelField
	Remain     []int
	Body       []int
	Optional   map[string]bool

	AttributeRange      map[string][]int
	AttributeNameRange  map[string][]int
	AttributeValueRange map[string][]int

	DefRange   []int
	TypeRange  []int
	LabelRange map[string][]int
}

type labelField struct {
	FieldIndex []int
	Name       string
}

func getFieldTags(ty reflect.Type) *fieldTags {
	ret := &fieldTags{
		Attributes:          map[string][]int{},
		Blocks:              map[string][]int{},
		Optional:            map[string]bool{},
		AttributeRange:      map[string][]int{},
		AttributeNameRange:  map[string][]int{},
		AttributeValueRange: map[string][]int{},
		LabelRange:          map[string][]int{},
	}
	walkFieldTags(ret, ty, nil, map[reflect.Type]struct{}{})
	return ret
}

// walkFieldTags records hcl tags on ty. Exported embedded structs with no
// hcl tag of their own contribute their fields, matching encoding/json.
// A shallower field wins when two fields share a name.
func walkFieldTags(ret *fieldTags, ty reflect.Type, prefix []int, seen map[reflect.Type]struct{}) {
	if ty.Kind() == reflect.Pointer {
		ty = ty.Elem()
	}
	if ty.Kind() != reflect.Struct {
		return
	}
	if _, ok := seen[ty]; ok {
		return
	}
	seen[ty] = struct{}{}
	defer delete(seen, ty)

	for i := 0; i < ty.NumField(); i++ {
		field := ty.Field(i)
		idx := append(append([]int{}, prefix...), i)
		tag := field.Tag.Get("hcl")
		if tag == "" {
			if field.Anonymous && field.IsExported() {
				embedTy := field.Type
				if embedTy.Kind() == reflect.Pointer {
					embedTy = embedTy.Elem()
				}
				if embedTy.Kind() == reflect.Struct {
					walkFieldTags(ret, embedTy, idx, seen)
				}
			}
			continue
		}

		comma := strings.Index(tag, ",")
		var name, kind string
		if comma != -1 {
			name = tag[:comma]
			kind = tag[comma+1:]
		} else {
			name = tag
			kind = "attr"
		}

		switch kind {
		case "attr":
			ret.setNamed(ret.Attributes, name, idx)
		case "block":
			ret.setNamed(ret.Blocks, name, idx)
		case "label":
			replaced := false
			for li := range ret.Labels {
				if ret.Labels[li].Name != name {
					continue
				}
				replaced = true
				if shallower(idx, ret.Labels[li].FieldIndex) {
					ret.Labels[li].FieldIndex = idx
				}
			}
			if !replaced {
				ret.Labels = append(ret.Labels, labelField{FieldIndex: idx, Name: name})
			}
		case "remain":
			if ret.Remain != nil && !shallower(idx, ret.Remain) {
				if len(idx) == len(ret.Remain) {
					panic("only one 'remain' tag is permitted")
				}
				break
			}
			if ret.Remain != nil && len(idx) == len(ret.Remain) {
				panic("only one 'remain' tag is permitted")
			}
			ret.Remain = idx
		case "body":
			if ret.Body != nil && len(idx) >= len(ret.Body) {
				if len(idx) == len(ret.Body) {
					panic("only one 'body' tag is permitted")
				}
				break
			}
			ret.Body = idx
		case "optional":
			ret.setNamed(ret.Attributes, name, idx)
			if _, kept := ret.Attributes[name]; kept && indexEqual(ret.Attributes[name], idx) {
				ret.Optional[name] = true
			}
		case "def_range":
			ret.setSingle(&ret.DefRange, idx, "def_range")
		case "type_range":
			ret.setSingle(&ret.TypeRange, idx, "type_range")
		case "label_range":
			ret.setNamed(ret.LabelRange, name, idx)
		case "attr_range":
			ret.setNamed(ret.AttributeRange, name, idx)
		case "attr_name_range":
			ret.setNamed(ret.AttributeNameRange, name, idx)
		case "attr_value_range":
			ret.setNamed(ret.AttributeValueRange, name, idx)
		default:
			panic(fmt.Sprintf("invalid hcl field tag kind %q on %s %q", kind, field.Type.String(), field.Name))
		}
	}
}

// setNamed keeps the shallower of two fields that share a name.
func (ret *fieldTags) setNamed(dst map[string][]int, name string, idx []int) {
	if prev, ok := dst[name]; ok && !shallower(idx, prev) {
		return
	}
	dst[name] = idx
}

func (ret *fieldTags) setSingle(dst *[]int, idx []int, kind string) {
	if *dst != nil && !shallower(idx, *dst) {
		if len(idx) == len(*dst) {
			panic(fmt.Sprintf("only one %q tag is permitted", kind))
		}
		return
	}
	if *dst != nil && len(idx) == len(*dst) {
		panic(fmt.Sprintf("only one %q tag is permitted", kind))
	}
	*dst = idx
}

func shallower(next, prev []int) bool {
	return len(next) < len(prev)
}

func indexEqual(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func indexLess(a, b []int) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return len(a) < len(b)
}

func typeByIndex(ty reflect.Type, index []int) reflect.StructField {
	var field reflect.StructField
	for n, i := range index {
		if ty.Kind() == reflect.Pointer {
			ty = ty.Elem()
		}
		field = ty.Field(i)
		if n < len(index)-1 {
			ty = field.Type
		}
	}
	return field
}

// valueByIndex walks index. Nil pointer embedded structs are allocated when
// allocNil is set so a decoded value can be stored. The field itself is not
// allocated when it is a pointer. An invalid Value means a nil embedded
// pointer was left unset.
func valueByIndex(v reflect.Value, index []int, allocNil bool) reflect.Value {
	for _, i := range index {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				if !allocNil || !v.CanSet() {
					return reflect.Value{}
				}
				v.Set(reflect.New(v.Type().Elem()))
			}
			v = v.Elem()
		}
		v = v.Field(i)
	}
	return v
}
