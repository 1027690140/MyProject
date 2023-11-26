package consumer

// kafka 实现异步调用
// 客户端是一个生产者，服务端是一个消费者
import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"io/ioutil"
	"log"
	"rpc_service/protocol"
	"time"

	"github.com/Shopify/sarama"
	"github.com/google/uuid"
	"gopkg.in/yaml.v2"
)

// 定义Kafka主题名称
const (
	RequestTopic  = "rpc_requests"
	ResponseTopic = "rpc_responses"
)

const (
	MaxRetryCount = 3
)

type Config struct {
	KafkaAddress string `yaml:"kafkaAddress"`
}

func readConfigFromFile(filename string) *Config {
	// Read the configuration file
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		log.Fatal("Failed to read config file:", err)
	}

	// Parse the configuration file
	config := &Config{}
	err = yaml.Unmarshal(data, config)
	if err != nil {
		log.Fatal("Failed to parse config file:", err)
	}

	return config
}

// 定义超时时间
const (
	RequestTimeout = 5 * time.Second
)

// string 转 byte
func stringToBytes(s string) byte {
	var b byte
	for i := 0; i < len(s); i++ {
		b += byte(s[i])
	}
	return b
}

// 客户端发送RPC请求
func (cp *RPCClientProxy) sendRPCRequest(client sarama.SyncProducer, payload []byte) ([]byte, error) {
	// 生成唯一请求ID
	requestID := uuid.New().String()

	// 封装RPC请求
	request := protocol.RPCMsg{
		Header:  RPCMsgHeader(stringToBytes(requestID), time.Now()),
		Payload: payload,
	}

	// 将RPC请求转换为JSON格式
	requestBytes, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}

	// 发送RPC请求到请求主题
	_, _, err = client.SendMessage(&sarama.ProducerMessage{
		Topic: RequestTopic,
		Value: sarama.StringEncoder(requestBytes),
	})
	if err != nil {
		return nil, err
	}

	// 等待响应
	response, err := cp.waitForRPCResponse(requestID)
	if err != nil {
		log.Println("RPC request error:", err)
		return nil, err
	}

	return response.Payload, nil
}

// 等待RPC响应
func (cp *RPCClientProxy) waitForRPCResponse(requestID string) (*protocol.RPCMsg, error) {
	// 读取配置文件
	config := readConfigFromFile("config.yaml")

	consumer, err := sarama.NewConsumer([]string{config.KafkaAddress}, nil)
	if err != nil {
		return nil, err
	}
	defer consumer.Close()

	partitionConsumer, err := consumer.ConsumePartition(ResponseTopic, 0, sarama.OffsetNewest)
	if err != nil {
		return nil, err
	}
	defer partitionConsumer.Close()

	// 设置超时时间
	timeout := time.After(RequestTimeout)

	// 循环接收消息，直到获取到匹配的RPC响应或超时
	for {
		select {
		case <-timeout:
			return nil, errors.New("RPC request timed out" + requestID)
		case msg := <-partitionConsumer.Messages():
			// 解析RPC响应
			var response protocol.RPCMsg
			err := json.Unmarshal(msg.Value, &response)
			if err != nil {
				return nil, err
			}

			// 判断是否匹配请求ID
			if string(response.Header[5]) == requestID {
				// 将收到的消息传递给RPCClientProxy的HandleKafkaMessage方法
				cp.HandleKafkaMessage(msg)

				// 等待RPCClientProxy处理完毕，通过管道通知结果
				resultCh := make(chan *protocol.RPCMsg)
				errCh := make(chan error)

				go func() {
					// 在RPCClientProxy的HandleKafkaMessage方法中执行了Call函数后，将结果通过管道传递回来
					result, err := cp.GetResultFromRPCClientProxy()
					if err != nil {
						errCh <- err
					} else {
						resultCh <- result.(*protocol.RPCMsg)
					}
				}()

				// 等待结果或错误
				select {
				case result := <-resultCh:
					return result, nil
				case err := <-errCh:
					return nil, err
				}
			}
		}
	}
}

// 服务端处理RPC请求
func handleRPCRequest(msg *sarama.ConsumerMessage) {
	// 解析RPC请求
	var request protocol.RPCMsg
	err := json.Unmarshal(msg.Value, &request)
	if err != nil {
		log.Println("Failed to unmarshal RPC request:", err)
		return
	}
	// 执行RPC请求处理逻辑
	result, err := processRPCRequest(request)
	if err != nil {
		log.Println("RPC request error:", err)
		return
	}

	// 构建RPC响应
	response := protocol.RPCMsg{
		Header:  RPCMsgHeader(request.Header[5], time.Now()),
		Payload: []byte(result),
	}

	// 将RPC响应转换为JSON格式
	responseBytes, err := json.Marshal(response)
	if err != nil {
		log.Println("Failed to marshal RPC response:", err)
		return
	}

	// 发送RPC响应到响应主题
	producer, err := sarama.NewSyncProducer([]string{"localhost:9092"}, nil)
	if err != nil {
		log.Println("Failed to create Kafka producer:", err)
		return
	}
	defer producer.Close()

	_, _, err = producer.SendMessage(&sarama.ProducerMessage{
		Topic: ResponseTopic,
		Value: sarama.StringEncoder(responseBytes),
	})
	if err != nil {
		log.Println("Failed to produce RPC response:", err)
	}
}

// 处理RPC请求的业务逻辑
func processRPCRequest(request protocol.RPCMsg) (string, error) {
	// 执行相应的业务逻辑处理，例如调用特定的服务方法

	// 处理过程中发生错误
	if request.Error != nil {
		return "", request.Error
	}

	// 将请求的Payload作为响应返回
	return string(request.Payload), nil
}

// RPCMsgHeader creates the header for RPCMsg
func RPCMsgHeader(id byte, timestamp time.Time) *protocol.Header {
	var header *protocol.Header
	header[5] = id
	binary.BigEndian.PutUint32(header[5:], uint32(timestamp.Unix()))
	return header
}
