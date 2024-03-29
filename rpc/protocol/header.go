package protocol

import "time"

const (
	HEADER_LEN = 7
)

const (
	magicNumber byte = 0x06
)

type MsgType byte

const (
	Request MsgType = iota
	Response
)

// 压缩方法
type CompressType byte

const (
	None CompressType = iota
	Gzip
)

// 序列化协议
type SerializeType byte

const (
	Gob SerializeType = iota
	JSON
)

type Header [HEADER_LEN]byte

func (h *Header) CheckMagicNumber() bool {
	return h[0] == magicNumber
}

func (h *Header) Version() byte {
	return h[1]
}

func (h *Header) SetVersion(version byte) {
	h[1] = version
}

func (h *Header) MsgType() MsgType {
	return MsgType(h[2])
}

func (h *Header) SetMsgType(msgType MsgType) {
	h[2] = byte(msgType)
}

func (h *Header) CompressType() CompressType {
	return CompressType(h[3])
}

func (h *Header) SetCompressType(compressType CompressType) {
	h[3] = byte(compressType)
}

func (h *Header) SerializeType() SerializeType {
	return SerializeType(h[4])
}

func (h *Header) SetSerializeType(serializeType SerializeType) {
	h[4] = byte(serializeType)
}

// set message id
func (h *Header) SetMessageID(id uint64) {
	h[5] = byte(id)
}

// get message id
func (h *Header) GetMessageID() uint64 {
	return uint64(h[5])
}

// set message id
func (h *Header) SetTimestamp() {
	h[6] = byte(time.Now().Unix())
}

// get message id
func (h *Header) GetTimestamp() uint64 {
	return uint64(h[6])
}
