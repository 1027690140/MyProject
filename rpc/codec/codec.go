package codec

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"encoding/json"
	msg "rpc_service/protocol"

	"io"
	"log"
)

type Codec interface {
	Encode(i interface{}) ([]byte, error)
	Decode(data []byte, i interface{}) error
}

type JSONCodec struct{}

func (c JSONCodec) Encode(i interface{}) ([]byte, error) {
	return json.Marshal(i)
}

func (c JSONCodec) Decode(data []byte, i interface{}) error {
	decode := json.NewDecoder(bytes.NewBuffer(data))
	//	decode.UseNumber()
	return decode.Decode(i)

}
func NewJSONCodec(conn io.ReadWriteCloser) Codec {
	return &JSONCodec{}
}

var _ Codec = (*GobCodec)(nil)

type GobCodec struct {
	conn io.ReadWriteCloser
	buf  *bufio.Writer
	dec  *gob.Decoder
	enc  *gob.Encoder
}

func (c GobCodec) Encode(i interface{}) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := gob.NewEncoder(&buffer)
	if err := encoder.Encode(i); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (c GobCodec) Decode(data []byte, i interface{}) error {
	buffer := bytes.NewBuffer(data)
	decoder := gob.NewDecoder(buffer)
	return decoder.Decode(i)
}

func NewGobCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &GobCodec{
		conn: conn,
		buf:  buf,
		dec:  gob.NewDecoder(conn),
		enc:  gob.NewEncoder(buf),
	}
}

func (c *GobCodec) ReadHeader(h *msg.RPCMsg) error {
	return c.dec.Decode(h)
}

func (c *GobCodec) ReadBody(body interface{}) error {
	return c.dec.Decode(body)
}

func (c *GobCodec) Write(h *msg.RPCMsg, body interface{}) (err error) {
	defer func() {
		_ = c.buf.Flush()
		if err != nil {
			_ = c.Close()
		}
	}()
	if err = c.enc.Encode(h); err != nil {
		log.Println("rpc: gob error encoding header:", err)
		return
	}
	if err = c.enc.Encode(body); err != nil {
		log.Println("rpc: gob error encoding body:", err)
		return
	}
	return
}

func (c *GobCodec) Close() error {
	return c.conn.Close()
}
