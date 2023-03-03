package global

import (
	"io"
	"rpc_service/codec"
	"rpc_service/protocol"
)

type Number byte

const (
	magicNumber Number = 0x06
)

var Codecs = map[protocol.SerializeType]codec.Codec{
	protocol.JSON: &codec.JSONCodec{},
	protocol.Gob:  &codec.GobCodec{},
}

type NewCodecFunc func(io.ReadWriteCloser) codec.Codec

var NewCodecFuncMap map[protocol.SerializeType]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[protocol.SerializeType]NewCodecFunc)
	NewCodecFuncMap[protocol.Gob] = codec.NewGobCodec
	NewCodecFuncMap[protocol.JSON] = codec.NewJSONCodec
}
