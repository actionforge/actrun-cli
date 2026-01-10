package core

import (
	"io"
	"reflect"
)

type Iterable interface {
	Next() bool
	Key() any
	Value() any
}

type ReaderIterable struct {
	reader io.Reader
	buffer []byte
	pos    int
}

func (r *ReaderIterable) Key() any {
	if r.pos > 0 {
		return r.pos - 1
	}
	return nil
}

func (r *ReaderIterable) Next() bool {
	if r.pos < len(r.buffer) {
		r.pos++
		return true
	}

	buf := make([]byte, 1)
	n, err := r.reader.Read(buf)
	if n > 0 {
		r.buffer = append(r.buffer, buf[0])
		r.pos++
		return true
	}

	closer, ok := r.reader.(io.Closer)
	if ok {
		_ = closer.Close()
	}

	if err != nil {
		if err == io.EOF {
			return false
		}
		panic(err)
	}
	return false
}

func (r *ReaderIterable) Value() any {
	if r.pos > 0 && r.pos <= len(r.buffer) {
		return r.buffer[r.pos-1]
	}
	return nil
}

func (r *ReaderIterable) Reset() {
	r.pos = 0
}

type StringIterable struct {
	data string
	pos  int
}

func (s *StringIterable) Key() any {
	return s.pos
}

func (s *StringIterable) Next() bool {
	if s.pos < len(s.data) {
		s.pos++
		return true
	}
	return false
}

func (s *StringIterable) Value() any {
	return string(s.data[s.pos-1])
}

func (s *StringIterable) Reset() {
	s.pos = 0
}

type SliceIterable struct {
	data reflect.Value
	pos  int
}

func (s *SliceIterable) Key() any {
	return s.pos - 1
}

func (s *SliceIterable) Next() bool {
	if s.pos < s.data.Len() {
		s.pos++
		return true
	}
	return false
}

func (s *SliceIterable) Value() any {
	return s.data.Index(s.pos - 1).Interface()
}

func (s *SliceIterable) Reset() {
	s.pos = 0
}

type MapIterable struct {
	data reflect.Value
	pos  int
}

func (m *MapIterable) Key() any {
	if m.pos > 0 {
		return m.data.MapKeys()[m.pos-1].Interface()
	}
	return nil
}

func (m *MapIterable) Next() bool {
	if m.pos < len(m.data.MapKeys()) {
		m.pos++
		return true
	}
	return false
}

func (m *MapIterable) Value() any {
	if m.pos > 0 && m.pos <= len(m.data.MapKeys()) {
		return m.data.MapIndex(m.data.MapKeys()[m.pos-1]).Interface()
	}
	return nil
}

func (m *MapIterable) Reset() {
	m.pos = 0
}
