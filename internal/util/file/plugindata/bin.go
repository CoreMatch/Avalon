package plugindata

import (
	"errors"
	"fmt"

	gameio "rwhps/internal/io"
	"rwhps/internal/struct/orderedmap"
	"rwhps/internal/util/compression"
)

// errTooManyEntries 防御性上限, 避免损坏文件把 count 读成天文数字后长时间空转
const maxEntries = 1 << 20

// decode 对应 MachineFriendlySerializers.read (Gzip 分支)
//
// 布局: gzip( int32 count, { string key, byte tag, payload } * count )
func decode(data []byte) (*orderedmap.Map[*Value], error) {
	raw, err := compression.GzipDecode(data)
	if err != nil {
		return nil, err
	}
	in := gameio.NewGameInputStreamFromBytes(raw, 0)

	count := in.ReadInt()
	if count < 0 || count > maxEntries {
		return nil, fmt.Errorf("plugindata: bad entry count %d", count)
	}

	out := orderedmap.New[*Value]()
	for i := int32(0); i < count; i++ {
		key := in.ReadString()
		if key == "" {
			return nil, errors.New("plugindata: empty key")
		}
		tag := in.ReadUnsignedByte()
		switch Kind(tag) {
		case KindBool:
			out.Put(key, &Value{Kind: KindBool, Bool: in.ReadBoolean()})
		case KindInt:
			out.Put(key, &Value{Kind: KindInt, Int: in.ReadInt()})
		case KindLong:
			out.Put(key, &Value{Kind: KindLong, Long: in.ReadLong()})
		case KindFloat:
			out.Put(key, &Value{Kind: KindFloat, Float: in.ReadFloat()})
		case KindString:
			out.Put(key, &Value{Kind: KindString, Str: in.ReadString()})
		case KindRaw:
			class := in.ReadString()
			length := in.ReadInt()
			if length < 0 || int(length) > len(raw) {
				return nil, fmt.Errorf("plugindata: bad raw length %d for %q", length, key)
			}
			out.Put(key, &Value{Kind: KindRaw, Raw: Raw{Class: class, Bytes: in.ReadNBytes(int(length))}})
		default:
			return nil, fmt.Errorf("plugindata: unexpected tag %d for %q", tag, key)
		}
	}
	return out, nil
}

// encode 对应 MachineFriendlySerializers.save (Gzip 分支)
func encode(data *orderedmap.Map[*Value]) []byte {
	o := gameio.NewGameOutputStream()
	o.WriteInt(int32(data.Size()))

	data.EachAll(func(key string, v *Value) {
		if v == nil {
			return
		}
		o.WriteString(key)
		o.WriteByte(byte(v.Kind))
		switch v.Kind {
		case KindBool:
			o.WriteBoolean(v.Bool)
		case KindInt:
			o.WriteInt(v.Int)
		case KindLong:
			o.WriteLong(v.Long)
		case KindFloat:
			o.WriteFloat(v.Float)
		case KindString:
			o.WriteString(v.Str)
		case KindRaw:
			o.WriteString(v.Raw.Class)
			o.WriteInt(int32(len(v.Raw.Bytes)))
			o.WriteBytes(v.Raw.Bytes)
		}
	})

	return compression.GzipEncode(o.GetByteArray())
}
