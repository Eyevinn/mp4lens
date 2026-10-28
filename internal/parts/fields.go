package parts

import (
	"fmt"
	"reflect"
	"strings"
)

// Limits of structLines.
const (
	maxDepth      = 6
	maxSliceItems = 32
	maxHexBytes   = 48
)

// structLines returns Info lines for the exported fields of v, which is a struct or a pointer to one, at nesting
// level indent. A nested struct gets a line with its name and then the lines of its fields, one level deeper.
func structLines(v any, indent int) []string {
	if v == nil {
		return nil
	}
	return valueLines(reflect.ValueOf(v), indent, 0)
}

func valueLines(v reflect.Value, indent, depth int) []string {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct || depth > maxDepth {
		return nil
	}
	var lines []string
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}
		fv := v.Field(i)
		if sf.Anonymous {
			lines = append(lines, valueLines(fv, indent, depth+1)...)
			continue
		}
		lines = append(lines, fieldLines(sf.Name, fv, indent, depth)...)
	}
	return lines
}

// fieldLines returns the lines of the field name with value v.
func fieldLines(name string, v reflect.Value, indent, depth int) []string {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		if s, ok := v.Interface().(fmt.Stringer); ok && v.Elem().Kind() != reflect.Struct {
			return []string{field(indent, name, s.String())}
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Struct:
		sub := valueLines(v, indent+1, depth+1)
		if len(sub) == 0 {
			return nil
		}
		return append([]string{field(indent, name, "")}, sub...)
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() == reflect.Uint8 {
			b := make([]byte, v.Len())
			reflect.Copy(reflect.ValueOf(b), v)
			if len(b) == 0 {
				return nil
			}
			return []string{field(indent, name, hexText(b, maxHexBytes))}
		}
		if v.Len() == 0 {
			return nil
		}
		elem := v.Type().Elem()
		for elem.Kind() == reflect.Pointer {
			elem = elem.Elem()
		}
		if elem.Kind() == reflect.Struct || elem.Kind() == reflect.Slice {
			var lines []string
			for i := 0; i < min(v.Len(), maxSliceItems); i++ {
				lines = append(lines, fieldLines(fmt.Sprintf("%s[%d]", name, i), v.Index(i), indent, depth+1)...)
			}
			if v.Len() > maxSliceItems {
				lines = append(lines, field(indent, name, fmt.Sprintf("… %d more", v.Len()-maxSliceItems)))
			}
			return lines
		}
		items := make([]string, 0, min(v.Len(), maxSliceItems))
		for i := 0; i < min(v.Len(), maxSliceItems); i++ {
			items = append(items, fmt.Sprint(v.Index(i).Interface()))
		}
		s := "[" + strings.Join(items, " ") + "]"
		if v.Len() > maxSliceItems {
			s = fmt.Sprintf("[%s … %d more]", strings.Join(items, " "), v.Len()-maxSliceItems)
		}
		return []string{field(indent, name, s)}
	case reflect.Map, reflect.Func, reflect.Chan:
		return nil
	}
	if !v.CanInterface() {
		return nil
	}
	return []string{field(indent, name, v.Interface())}
}
