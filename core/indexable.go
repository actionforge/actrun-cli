package core

import (
	"reflect"
)

type Indexable interface {
	Index(int) any
	Len() int
	Append(any) error
	AppendValue(reflect.Value) error
	GetType() reflect.Type
	GetData() reflect.Value
}

type IndexableImpl struct {
	Data reflect.Value
}

func (i *IndexableImpl) Index(index int) any {
	return i.Data.Index(index).Interface()
}

func (i *IndexableImpl) Len() int {
	return i.Data.Len()
}

func (i *IndexableImpl) AppendValue(value reflect.Value) error {
	if i.Data.Kind() == reflect.Slice {
		s, err := ConvertValue(nil, value, i.Data.Type().Elem())
		if err != nil {
			return err
		}

		i.Data = reflect.Append(i.Data, reflect.ValueOf(s))
	} else if i.Data.Kind() == reflect.String {
		s, err := ConvertToString(nil, value)
		if err != nil {
			return err
		}

		i.Data = reflect.ValueOf(i.Data.String() + s)
	} else {
		return CreateErr(nil, nil, "cannot append to type %v", i.Data.Kind())
	}
	return nil
}

func (i *IndexableImpl) Append(value any) error {
	return i.AppendValue(reflect.ValueOf(value))
}

func (i *IndexableImpl) GetType() reflect.Type {
	return i.Data.Type()
}

func (i *IndexableImpl) GetData() reflect.Value {
	return i.Data
}
