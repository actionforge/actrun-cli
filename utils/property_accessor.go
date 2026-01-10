package utils

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

var (
	onceReArrayNotation sync.Once
	reArrayNotation     *regexp.Regexp
)

var anyType = reflect.TypeOf((*any)(nil)).Elem()
var stringType = reflect.TypeOf("")

func getArrayNotationRegex() *regexp.Regexp {
	onceReArrayNotation.Do(func() {
		reArrayNotation = regexp.MustCompile(`\[(.*?)\]`)
	})
	return reArrayNotation
}

// GetTypedPropertyByPath retrieves an item or subitem from a map.
// Especially used to retrieve items from yaml or json interface maps.
func GetTypedPropertyByPath[T any](i map[string]any, path string) (T, error) {
	var empty T
	v, err := GetPropertyByPath(i, path)
	if err != nil {
		return empty, err
	}

	value, ok := v.(T)
	if !ok {
		return empty, fmt.Errorf("cannot convert value to type %T", empty)
	}

	return value, nil
}

func GetPropertyByPath(data any, path string) (any, error) {
	if path == "" {
		return nil, fmt.Errorf("path cannot be empty")
	}

	accessors, err := buildAccessorChain(path)
	if err != nil {
		return nil, err
	}

	current := reflect.ValueOf(data)
	v, err := getPathRecursive(current, accessors)
	if err != nil {
		return nil, err
	}

	return v.Interface(), nil
}

func SetPropertyByPath(data any, path string, value any) (any, error) {
	if path == "" {
		return nil, fmt.Errorf("path cannot be empty")
	}

	accessors, err := buildAccessorChain(path)
	if err != nil {
		return nil, err
	}

	current := reflect.ValueOf(data)
	replaceWithThis := reflect.ValueOf(value)
	v, err := setPathRecursive(current, accessors, replaceWithThis)
	return v.Interface(), err
}

type ErrPropertyNotFound struct {
	FieldName string
}

func (e *ErrPropertyNotFound) Error() string {
	return fmt.Sprintf("key not found: %s", e.FieldName)
}

func (e *ErrPropertyNotFound) Is(target error) bool {
	_, ok := target.(*ErrPropertyNotFound)
	return ok
}

func getPathRecursive(current reflect.Value, accessors []Accessor) (reflect.Value, error) {
	if len(accessors) == 0 {
		return current, nil
	}

	if current.Kind() == reflect.Interface {
		current = current.Elem()
	}

	accessor := accessors[0]
	var next reflect.Value

	switch accessor.Type {
	case Accessor_Field:
		if current.Kind() != reflect.Map {
			return reflect.Value{}, fmt.Errorf("expected map at path")
		}
		key := reflect.ValueOf(accessor.FieldName)
		next = current.MapIndex(key)
		if !next.IsValid() {
			return reflect.Value{}, &ErrPropertyNotFound{
				FieldName: accessor.FieldName,
			}
		}

	case Accessor_Index:
		if current.Kind() != reflect.Slice {
			return reflect.Value{}, fmt.Errorf("expected array at path")
		}
		if current.Len() <= accessor.Index {
			return reflect.Value{}, fmt.Errorf("index %d out of range", accessor.Index)
		}
		next = current.Index(accessor.Index)
		if next.Kind() == reflect.Invalid {
			return reflect.Value{}, fmt.Errorf("invalid value at index %d", accessor.Index)
		}
	}

	return getPathRecursive(next, accessors[1:])
}

func setPathRecursive(current reflect.Value, accessors []Accessor, replaceWithThis reflect.Value) (reflect.Value, error) {
	if len(accessors) == 0 {
		return replaceWithThis, nil
	}

	if current.Kind() == reflect.Interface {
		current = current.Elem()
	}

	accessor := accessors[0]
	var next reflect.Value

	switch accessor.Type {
	case Accessor_Field:
		if current.Kind() != reflect.Map {
			current = createAnyMap()
		}
		key := reflect.ValueOf(accessor.FieldName)
		if current.MapIndex(key).IsValid() {
			next = current.MapIndex(key)
		} else {
			next = createAnyMap()
			current.SetMapIndex(key, next)
		}

	case Accessor_Index:
		if current.Kind() != reflect.Slice {
			current = createAnySlice(accessor.Index + 1)
		} else if current.Len() <= accessor.Index {
			newSlice := createAnySlice(accessor.Index + 1)
			reflect.Copy(newSlice, current)
			current = newSlice
		}
		next = current.Index(accessor.Index)
		if next.Kind() == reflect.Invalid {
			next = reflect.ValueOf(nil)
		}
	}

	next, err := setPathRecursive(next, accessors[1:], replaceWithThis)
	if err != nil {
		return reflect.Value{}, err
	}

	switch accessor.Type {
	case Accessor_Field:
		current.SetMapIndex(reflect.ValueOf(accessor.FieldName), next)
	case Accessor_Index:
		current.Index(accessor.Index).Set(next)
	}

	return current, nil
}

func extractNumbers(key string) (string, []int, error) {
	matches := getArrayNotationRegex().FindAllStringSubmatch(key, -1)

	var numbers []int

	for _, match := range matches {
		if match[1] == "" {
			return "", nil, fmt.Errorf("invalid array index: %s", key)
		}

		num, err := strconv.Atoi(match[1])
		if err != nil {
			return "", nil, err
		}
		numbers = append(numbers, num)
	}

	// if no array notation found, we just return the entire key.
	// might return corrupt accessors like `FOO[`
	if matches == nil {
		return key, nil, nil
	} else {
		key = strings.Split(key, "[")[0]
		return key, numbers, nil
	}
}

type AccessorType int

const (
	Accessor_Field AccessorType = iota
	Accessor_Index
)

type Accessor struct {
	Type      AccessorType
	FieldName string
	Index     int
}

// buildAccessorChain builds a chain of accessors from a path string.
// The path string is a dot-separated string that can contain array indices.
// For example, the path "a.b[0][4].c" would be split into the accessors:
// - Field{FieldName: "a"}
// - Field{FieldName: "b"}
// - Index{Index: 0}
// - Index{Index: 4}
// - Field{FieldName: "c"}
func buildAccessorChain(path string) ([]Accessor, error) {

	accessors := []Accessor{}

	for _, part := range strings.Split(path, ".") {

		key, arrIndeces, err := extractNumbers(part)
		if err != nil {
			return nil, err
		}

		if key != "" {
			accessors = append(accessors, Accessor{
				Type:      Accessor_Field,
				FieldName: key,
			})
		}

		for _, index := range arrIndeces {
			accessors = append(accessors, Accessor{
				Type:  Accessor_Index,
				Index: index,
			})
		}
	}

	return accessors, nil
}

func createAnySlice(length int) reflect.Value {
	return reflect.MakeSlice(reflect.SliceOf(anyType), length, length)
}

func createAnyMap() reflect.Value {
	return reflect.MakeMap(reflect.MapOf(stringType, anyType))
}
