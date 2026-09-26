package tasks

import (
	"fmt"
	"reflect"
)

// reflectToList converts a typed slice ([]string, []int, etc.) into a
// generic []interface{}. Returns an error when v is not a slice or array.
func reflectToList(v interface{}) ([]interface{}, error) {
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil, fmt.Errorf("loop expression returned %T; expected a list", v)
	}
	out := make([]interface{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		out[i] = rv.Index(i).Interface()
	}
	return out, nil
}
