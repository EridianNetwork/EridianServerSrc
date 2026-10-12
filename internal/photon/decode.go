package photon

import (
	"encoding/binary"
	"fmt"
	"math"
)

func DecodeValue(data []byte, pos int) (any, int, error) {
	if pos >= len(data) {
		return nil, pos, fmt.Errorf("decode: unexpected end at pos %d", pos)
	}
	tc := data[pos]
	return decodeBody(data, pos+1, tc)
}

func decodeBody(data []byte, pos int, tc byte) (any, int, error) {
	switch tc {
	case TNull:
		return nil, pos, nil
	case TBool:
		if pos >= len(data) {
			return nil, pos, fmt.Errorf("decode bool: short")
		}
		return data[pos] != 0, pos + 1, nil
	case TByte:
		if pos >= len(data) {
			return nil, pos, fmt.Errorf("decode byte: short")
		}
		return data[pos], pos + 1, nil
	case TShort:
		if pos+2 > len(data) {
			return nil, pos, fmt.Errorf("decode short: short")
		}
		return int16(binary.BigEndian.Uint16(data[pos:])), pos + 2, nil
	case TInt:
		if pos+4 > len(data) {
			return nil, pos, fmt.Errorf("decode int: short")
		}
		return int32(binary.BigEndian.Uint32(data[pos:])), pos + 4, nil
	case TLong:
		if pos+8 > len(data) {
			return nil, pos, fmt.Errorf("decode long: short")
		}
		return int64(binary.BigEndian.Uint64(data[pos:])), pos + 8, nil
	case TFloat:
		if pos+4 > len(data) {
			return nil, pos, fmt.Errorf("decode float: short")
		}
		return math.Float32frombits(binary.BigEndian.Uint32(data[pos:])), pos + 4, nil
	case TDouble:
		if pos+8 > len(data) {
			return nil, pos, fmt.Errorf("decode double: short")
		}
		return math.Float64frombits(binary.BigEndian.Uint64(data[pos:])), pos + 8, nil
	case TString:
		if pos+2 > len(data) {
			return nil, pos, fmt.Errorf("decode string length: short")
		}
		slen := int(binary.BigEndian.Uint16(data[pos:]))
		pos += 2
		if pos+slen > len(data) {
			return nil, pos, fmt.Errorf("decode string body: short")
		}
		return string(data[pos : pos+slen]), pos + slen, nil
	case TByteArray:
		if pos+4 > len(data) {
			return nil, pos, fmt.Errorf("decode byte[] length: short")
		}
		blen := int(binary.BigEndian.Uint32(data[pos:]))
		pos += 4
		if pos+blen > len(data) {
			return nil, pos, fmt.Errorf("decode byte[] body: short")
		}
		out := make([]byte, blen)
		copy(out, data[pos:pos+blen])
		return out, pos + blen, nil
	case TCustom:
		if pos+3 > len(data) {
			return nil, pos, fmt.Errorf("decode custom header: short")
		}
		code := data[pos]
		clen := int(binary.BigEndian.Uint16(data[pos+1:]))
		pos += 3
		if pos+clen > len(data) {
			return nil, pos, fmt.Errorf("decode custom body: short")
		}
		body := make([]byte, clen)
		copy(body, data[pos:pos+clen])
		return Custom{Code: code, Data: body}, pos + clen, nil
	case THash:
		if pos+2 > len(data) {
			return nil, pos, fmt.Errorf("decode hashtable count: short")
		}
		count := int(binary.BigEndian.Uint16(data[pos:]))
		pos += 2
		d := make(map[any]any, count)
		for range count {
			k, newPos, err := DecodeValue(data, pos)
			if err != nil {
				return nil, newPos, fmt.Errorf("decode hash key: %w", err)
			}
			pos = newPos
			v, newPos, err := DecodeValue(data, pos)
			if err != nil {
				return nil, newPos, fmt.Errorf("decode hash value: %w", err)
			}
			pos = newPos
			d[k] = v
		}
		return d, pos, nil
	case TDict:
		if pos+4 > len(data) {
			return nil, pos, fmt.Errorf("decode dict header: short")
		}
		keyType := data[pos]
		valType := data[pos+1]
		count := int(binary.BigEndian.Uint16(data[pos+2:]))
		pos += 4
		d := make(map[any]any, count)
		for range count {
			k, newPos, err := decodeBody(data, pos, keyType)
			if err != nil {
				return nil, newPos, fmt.Errorf("decode dict key: %w", err)
			}
			pos = newPos
			v, newPos, err := decodeBody(data, pos, valType)
			if err != nil {
				return nil, newPos, fmt.Errorf("decode dict value: %w", err)
			}
			pos = newPos
			d[k] = v
		}
		return d, pos, nil
	case TArrObj:
		if pos+2 > len(data) {
			return nil, pos, fmt.Errorf("decode object[] count: short")
		}
		count := int(binary.BigEndian.Uint16(data[pos:]))
		pos += 2
		arr := make([]any, 0, count)
		for range count {
			v, newPos, err := DecodeValue(data, pos)
			if err != nil {
				return nil, newPos, fmt.Errorf("decode object[] elem: %w", err)
			}
			pos = newPos
			arr = append(arr, v)
		}
		return arr, pos, nil
	case TArray:
		if pos+2 > len(data) {
			return nil, pos, fmt.Errorf("decode array count: short")
		}
		count := int(binary.BigEndian.Uint16(data[pos:]))
		pos += 2
		if pos >= len(data) {
			return nil, pos, fmt.Errorf("decode array elem type: short")
		}
		elemType := data[pos]
		pos++
		arr := make([]any, 0, count)
		for range count {
			v, newPos, err := decodeBody(data, pos, elemType)
			if err != nil {
				return nil, newPos, fmt.Errorf("decode array elem: %w", err)
			}
			pos = newPos
			arr = append(arr, v)
		}
		return TypedArray{ElementType: elemType, Items: arr}, pos, nil
	case TIntArray:
		if pos+4 > len(data) {
			return nil, pos, fmt.Errorf("decode int[] count: short")
		}
		count := int(binary.BigEndian.Uint32(data[pos:]))
		pos += 4
		if pos+count*4 > len(data) {
			return nil, pos, fmt.Errorf("decode int[] body: short")
		}
		arr := make([]int32, count)
		for i := range count {
			arr[i] = int32(binary.BigEndian.Uint32(data[pos:]))
			pos += 4
		}
		return arr, pos, nil
	default:
		return nil, pos, fmt.Errorf("decode: unknown type code 0x%02x at pos %d", tc, pos-1)
	}
}

func DecodeParams(data []byte, pos int) (map[byte]any, int, error) {
	if pos+2 > len(data) {
		return nil, pos, fmt.Errorf("decode params count: short")
	}
	count := int(binary.BigEndian.Uint16(data[pos:]))
	pos += 2
	params := make(map[byte]any, count)
	for range count {
		if pos >= len(data) {
			return params, pos, fmt.Errorf("decode params: short at code %d", count)
		}
		code := data[pos]
		pos++
		v, newPos, err := DecodeValue(data, pos)
		if err != nil {
			return params, newPos, fmt.Errorf("decode params value %d: %w", code, err)
		}
		pos = newPos
		params[code] = v
	}
	return params, pos, nil
}
