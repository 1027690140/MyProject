package protocol

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"

	"rpc_service/util"
)

const (
	SPLIT_LEN = 4
)

// RPCMsg is  message format for rpc
type RPCMsg struct {
	*Header                         // 指针类型，占用 Header 的空间
	Error         error             // 错误信息
	ServiceAppID  string            // 服务应用ID
	ServiceClass  string            // 服务类名
	ServiceMethod string            // 服务方法名
	Payload       []byte            // 请求/响应的数据
	Metadata      map[string]string // 元数据
}

func NewRPCMsg() *RPCMsg {
	header := Header([HEADER_LEN]byte{})
	header[0] = magicNumber
	return &RPCMsg{
		Header: &header,
	}
}

func (msg *RPCMsg) Send(writer io.Writer) error {
	//send header
	_, err := writer.Write(msg.Header[:])
	if err != nil {
		return err
	}

	//write body total len :4 byte
	dataLen := SPLIT_LEN + len(msg.ServiceClass) + SPLIT_LEN + len(msg.ServiceMethod) + SPLIT_LEN + len(msg.Payload)
	err = binary.Write(writer, binary.BigEndian, uint32(dataLen)) //4
	if err != nil {
		return err
	}

	// write ServiceAppID method len :4 byte
	err = binary.Write(writer, binary.BigEndian, uint32(len(msg.ServiceAppID)))
	if err != nil {
		return err
	}

	// write ServiceAppID method
	err = binary.Write(writer, binary.BigEndian, util.StringToByte(msg.ServiceAppID))
	if err != nil {
		return err
	}

	//write service class len :4 byte
	err = binary.Write(writer, binary.BigEndian, uint32(len(msg.ServiceClass)))
	if err != nil {
		return err
	}

	//write service class
	err = binary.Write(writer, binary.BigEndian, util.StringToByte(msg.ServiceClass))
	if err != nil {
		return err
	}

	//write service method len :4 byte
	err = binary.Write(writer, binary.BigEndian, uint32(len(msg.ServiceMethod)))
	if err != nil {
		return err
	}

	//write service method
	err = binary.Write(writer, binary.BigEndian, util.StringToByte(msg.ServiceMethod))
	if err != nil {
		return err
	}

	//write payload len :4 byte
	err = binary.Write(writer, binary.BigEndian, uint32(len(msg.Payload)))
	if err != nil {
		return err
	}

	//write payload
	//err = binary.Write(writer, binary.BigEndian, msg.Payload)
	_, err = writer.Write(msg.Payload)
	if err != nil {
		return err
	}

	// write metadata len :4 byte
	err = binary.Write(writer, binary.BigEndian, uint32(len(msg.Metadata)))
	if err != nil {
		return err
	}

	// write metadata
	// err = binary.Write(writer, binary.BigEndian, msg.Payload)
	metada, _ := json.Marshal(msg.Metadata)
	_, err = writer.Write(metada)
	if err != nil {
		return err
	}

	return nil
}

func (msg *RPCMsg) Decode(r io.Reader) error {
	//read header
	_, err := io.ReadFull(r, msg.Header[:])
	if !msg.Header.CheckMagicNumber() { //magicNumber
		return fmt.Errorf("magic number error: %v", msg.Header[0])
	}

	//total body len
	headerByte := make([]byte, 4)
	_, err = io.ReadFull(r, headerByte)
	if err != nil {
		return err
	}
	bodyLen := binary.BigEndian.Uint32(headerByte)

	//read all body
	data := make([]byte, bodyLen)
	_, err = io.ReadFull(r, data)

	//Service AppID len
	start := 0
	end := start + SPLIT_LEN
	ServiceAppID := binary.BigEndian.Uint32(data[start:end]) //0,4

	//service ServiceAppID
	start = end
	end = start + int(ServiceAppID)
	msg.ServiceAppID = util.ByteToString(data[start:end]) //x+4, x+4+y

	//service class len
	start = 0
	end = start + SPLIT_LEN
	classLen := binary.BigEndian.Uint32(data[start:end]) //0,4

	//service class
	start = end
	end = start + int(classLen)
	msg.ServiceClass = util.ByteToString(data[start:end]) //4,x

	//service method len
	start = end
	end = start + SPLIT_LEN
	methodLen := binary.BigEndian.Uint32(data[start:end]) //x,x+4

	//service method
	start = end
	end = start + int(methodLen)
	msg.ServiceMethod = util.ByteToString(data[start:end]) //x+4, x+4+y

	//payload len
	start = end
	end = start + SPLIT_LEN
	payloadLen := binary.BigEndian.Uint32(data[start:end]) //x+4+y, x+y+8 payloadLen

	//payload
	start = end
	end = start + +int(payloadLen)
	msg.Payload = data[start:end]

	//metadata len
	start = end
	end = start + SPLIT_LEN
	binary.BigEndian.Uint32(data[start:end]) //x+4+y, x+y+8 payloadLen

	//metadata
	start = end
	Metadata := data[start:]
	// 把Metadata json decode 到 map[string]string
	json.Unmarshal(Metadata, &msg.Metadata)

	return nil
}

func Read(r io.Reader) (*RPCMsg, error) {
	msg := NewRPCMsg()
	err := msg.Decode(r)
	if err != nil {
		return nil, err
	}
	return msg, nil
}
