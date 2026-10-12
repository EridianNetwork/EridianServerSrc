package photon

import (
	"encoding/binary"
	"fmt"
	"math"
	"reflect"
)


type Byte byte

type Short int16

type Double float64

type Custom struct {
	Code byte
	Data []byte
}

type IntArray struct {
	Items []int32
}

type TypedArray struct {
	ElementType byte
	Items       []any
}

type DictIntInt map[int]int32

func encodeValue(v any) ([]byte, error) {
	if v == nil {
		return []byte{TNull}, nil
	}

	switch val := v.(type) {
	case bool:
		if val {
			return []byte{TBool, 1}, nil
		}
		return []byte{TBool, 0}, nil
	case Byte:
		return []byte{TByte, byte(val)}, nil
	case Short:
		return binary.BigEndian.AppendUint16([]byte{TShort}, uint16(val)), nil
	case int16:
		return binary.BigEndian.AppendUint16([]byte{TShort}, uint16(val)), nil
	case int8:
		return []byte{TByte, byte(val)}, nil
	case uint8:
		return []byte{TByte, val}, nil
	case int32:
		b := make([]byte, 5)
		b[0] = TInt
		binary.BigEndian.PutUint32(b[1:], uint32(val))
		return b, nil
	case int:
		b := make([]byte, 5)
		b[0] = TInt
		binary.BigEndian.PutUint32(b[1:], uint32(val))
		return b, nil
	case int64:
		b := make([]byte, 9)
		b[0] = TLong
		binary.BigEndian.PutUint64(b[1:], uint64(val))
		return b, nil
	case float32:
		b := make([]byte, 5)
		b[0] = TFloat
		binary.BigEndian.PutUint32(b[1:], math.Float32bits(val))
		return b, nil
	case float64:
		b := make([]byte, 9)
		b[0] = TDouble
		binary.BigEndian.PutUint64(b[1:], math.Float64bits(val))
		return b, nil
	case Double:
		b := make([]byte, 9)
		b[0] = TDouble
		binary.BigEndian.PutUint64(b[1:], math.Float64bits(float64(val)))
		return b, nil
	case string:
		s := []byte(val)
		b := make([]byte, 3+len(s))
		b[0] = TString
		binary.BigEndian.PutUint16(b[1:], uint16(len(s)))
		copy(b[3:], s)
		return b, nil
	case []byte:
		b := make([]byte, 5+len(val))
		b[0] = TByteArray
		binary.BigEndian.PutUint32(b[1:], uint32(len(val)))
		copy(b[5:], val)
		return b, nil
	case Custom:
		b := make([]byte, 4+len(val.Data))
		b[0] = TCustom
		b[1] = val.Code
		binary.BigEndian.PutUint16(b[2:], uint16(len(val.Data)))
		copy(b[4:], val.Data)
		return b, nil
	case map[int]any:
		return encodeHashTable(val, false)
	case map[any]any:
		return encodeHashTableAny(val, false)
	case IntArray:
		b := make([]byte, 5+len(val.Items)*4)
		b[0] = TIntArray
		binary.BigEndian.PutUint32(b[1:], uint32(len(val.Items)))
		for i, item := range val.Items {
			binary.BigEndian.PutUint32(b[5+i*4:], uint32(item))
		}
		return b, nil
	case DictIntInt:
		out := []byte{TDict, TInt, TInt}
		out = binary.BigEndian.AppendUint16(out, uint16(len(val)))
		for k, v := range val {
			out = binary.BigEndian.AppendUint32(out, uint32(k))
			out = binary.BigEndian.AppendUint32(out, uint32(v))
		}
		return out, nil
	case TypedArray:
		return encodeTypedArray(val)
	case []any:
		return encodeObjectArray(val)
	default:
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Map {
			out := make([]byte, 0)
			out = append(out, THash)
			out = binary.BigEndian.AppendUint16(out, uint16(rv.Len()))
			for _, key := range rv.MapKeys() {
				kEnc, err := encodeValue(key.Interface())
				if err != nil {
					return nil, err
				}
				out = append(out, kEnc...)
				vEnc, err := encodeValue(rv.MapIndex(key).Interface())
				if err != nil {
					return nil, err
				}
				out = append(out, vEnc...)
			}
			return out, nil
		}
		return nil, fmt.Errorf("photon encode: unsupported type %T", v)
	}
}

func encodeHashTable(d map[int]any, skipType bool) ([]byte, error) {
	if skipType {
		out := binary.BigEndian.AppendUint16(nil, uint16(len(d)))
		for k, v := range d {
			kEnc, err := encodeValue(k)
			if err != nil {
				return nil, err
			}
			out = append(out, kEnc...)
			vEnc, err := encodeValue(v)
			if err != nil {
				return nil, err
			}
			out = append(out, vEnc...)
		}
		return out, nil
	}
	out := []byte{THash}
	out = binary.BigEndian.AppendUint16(out, uint16(len(d)))
	for k, v := range d {
		kEnc, err := encodeValue(k)
		if err != nil {
			return nil, err
		}
		out = append(out, kEnc...)
		vEnc, err := encodeValue(v)
		if err != nil {
			return nil, err
		}
		out = append(out, vEnc...)
	}
	return out, nil
}

func encodeHashTableAny(d map[any]any, skipType bool) ([]byte, error) {
	if skipType {
		out := binary.BigEndian.AppendUint16(nil, uint16(len(d)))
		for k, v := range d {
			kEnc, err := encodeValue(k)
			if err != nil {
				return nil, err
			}
			out = append(out, kEnc...)
			vEnc, err := encodeValue(v)
			if err != nil {
				return nil, err
			}
			out = append(out, vEnc...)
		}
		return out, nil
	}
	out := []byte{THash}
	out = binary.BigEndian.AppendUint16(out, uint16(len(d)))
	for k, v := range d {
		kEnc, err := encodeValue(k)
		if err != nil {
			return nil, err
		}
		out = append(out, kEnc...)
		vEnc, err := encodeValue(v)
		if err != nil {
			return nil, err
		}
		out = append(out, vEnc...)
	}
	return out, nil
}

func encodeTypedArray(ta TypedArray) ([]byte, error) {
	b := make([]byte, 4)
	b[0] = TArray // 121
	binary.BigEndian.PutUint16(b[1:], uint16(len(ta.Items)))
	b[3] = ta.ElementType
	out := b
	for _, item := range ta.Items {
		switch ta.ElementType {
		case THash:
			if m, ok := item.(map[int]any); ok {
				ht, err := encodeHashTable(m, true)
				if err != nil {
					return nil, err
				}
				out = append(out, ht...)
			} else if m, ok := item.(map[any]any); ok {
				ht, err := encodeHashTableAny(m, true)
				if err != nil {
					return nil, err
				}
				out = append(out, ht...)
			} else {
				return nil, fmt.Errorf("typedarray: expected map for THash element, got %T", item)
			}
		default:
			enc, err := encodeValue(item)
			if err != nil {
				return nil, err
			}
			out = append(out, enc...)
		}
	}
	return out, nil
}

func encodeObjectArray(arr []any) ([]byte, error) {
	out := make([]byte, 3)
	out[0] = TArrObj
	binary.BigEndian.PutUint16(out[1:], uint16(len(arr)))
	for _, item := range arr {
		enc, err := encodeValue(item)
		if err != nil {
			return nil, err
		}
		out = append(out, enc...)
	}
	return out, nil
}

func EncodeParams(params map[byte]any) ([]byte, error) {
	out := binary.BigEndian.AppendUint16(nil, uint16(len(params)))
	for code, val := range params {
		out = append(out, code)
		enc, err := encodeValue(val)
		if err != nil {
			return nil, fmt.Errorf("encode param %d: %w", code, err)
		}
		out = append(out, enc...)
	}
	return out, nil
}
