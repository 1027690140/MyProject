package consumer

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"rpc_service/protocol"
	"strconv"
	"time"

	"github.com/Shopify/sarama"
)

type rpcConsumerHandler struct {
	producer        sarama.SyncProducer
	retryProducer   sarama.SyncProducer
	retryConsumer   sarama.ConsumerGroup
	retryBufferSize int
	MaxRetryCount   int
	retryBuffer     chan *protocol.RPCMsg
	RetryDelay      time.Duration
}

func (h *rpcConsumerHandler) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *rpcConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *rpcConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	h.retryBuffer = make(chan *protocol.RPCMsg, h.retryBufferSize)

	go h.processRetryQueue() // 启动重试队列处理协程

	for message := range claim.Messages() {
		// 解析RPC请求
		var request protocol.RPCMsg
		err := json.Unmarshal(message.Value, &request)
		if err != nil {
			log.Println("Failed to unmarshal RPC request:", err)
			continue
		}

		go func() {
			// 处理RPC请求
			response, err := processRPCRequest(request)
			if err != nil {
				log.Println("RPC request error:", err)

				if message.Headers == nil {
					message.Headers = make(map[string]string)
				}
				retryCount := getRetryCount(message.Headers)

				if retryCount < MaxRetryCount {
					retryCount++
					setRetryCount(message.Headers, retryCount)

					// 构建重试消息
					retryMessage := &protocol.RPCMsg{
						Request:  request,
						Attempt:  retryCount,
						MaxRetry: MaxRetryCount,
					}

					// 发送重试消息到重试队列
					h.retryBuffer <- retryMessage
				} else {
					log.Println("Max retry count reached, discarding message:", message)
				}
			} else {
				// 发送RPC响应到响应主题
				responseBytes, err := json.Marshal(response)
				if err != nil {
					log.Println("Failed to marshal RPC response:", err)
					return
				}

				_, _, err = h.producer.SendMessage(&sarama.ProducerMessage{
					Topic: ResponseTopic,
					Key:   message.Key,
					Value: sarama.ByteEncoder(responseBytes),
				})
				if err != nil {
					log.Println("Failed to send RPC response:", err)
					return
				}
			}
		}()
	}

	return nil
}

func (h *rpcConsumerHandler) processRetryQueue() {
	for retryMsg := range h.retryBuffer {
		// 添加重试延迟
		time.Sleep(h.RetryDelay)

		// 发送重试消息到重试主题
		retryBytes, err := json.Marshal(retryMsg)
		if err != nil {
			log.Println("Failed to marshal retry message:", err)
			continue
		}

		headers := make([]sarama.RecordHeader, 1)
		headers[0] = sarama.RecordHeader{
			Key:   []byte("retryCount"),
			Value: []byte(fmt.Sprintf("%d", retryMsg)),
		}

		_, _, err = h.retryProducer.SendMessage(&sarama.ProducerMessage{
			Topic:   RetryTopic,
			Key:     []byte(retryMsg.Request.ID),
			Value:   sarama.ByteEncoder(retryBytes),
			Headers: headers,
		})
		if err != nil {
			log.Println("Failed to send retry message:", err)
		}
	}
}

type retryConsumerHandler struct {
	producer   sarama.SyncProducer
	bufferSize int
	buffer     chan *sarama.ConsumerMessage
}

func (h *retryConsumerHandler) Setup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *retryConsumerHandler) Cleanup(sarama.ConsumerGroupSession) error {
	return nil
}

func (h *retryConsumerHandler) ConsumeClaim(session sarama.ConsumerGroupSession, claim sarama.ConsumerGroupClaim) error {
	h.buffer = make(chan *sarama.ConsumerMessage, h.bufferSize)

	go h.processRetryMessages() // 启动重试消息处理协程

	for message := range claim.Messages() {
		h.buffer <- message
	}

	return nil
}

func (h *retryConsumerHandler) processRetryMessages() {
	for message := range h.buffer {
		// 解析重试消息
		var request protocol.RPCMsg
		err := json.Unmarshal(message.Value, &request)
		if err != nil {
			log.Println("Failed to unmarshal retry message:", err)
			continue
		}

		go func() {
			// 处理RPC请求
			response, err := (request)
			if err != nil {
				log.Println("Retry request error:", err)

				if message.Headers == nil {
					message.Headers = make(map[string]string)
				}
				retryCount := getRetryCount(message.Headers)

				if retryCount < MaxRetryCount {
					retryCount++
					setRetryCount(message.Headers, retryCount)

					// 构建重试消息
					retryMessage := &protocol.RPCMsg{
						Header:  request.Header,
						Request: request.Request,
						Attempt: retryCount,
					}

					// 发送重试消息到重试队列
					h.buffer <- retryMessage
				} else {
					log.Println("Max retry count reached, discarding retry message:", message)
				}
			} else {
				// 发送RPC响应到响应主题
				responseBytes, err := json.Marshal(response)
				if err != nil {
					log.Println("Failed to marshal RPC response:", err)
					return
				}

				_, _, err = h.producer.SendMessage(&sarama.ProducerMessage{
					Topic: ResponseTopic,
					Key:   message.Key,
					Value: sarama.ByteEncoder(responseBytes),
				})
				if err != nil {
					log.Println("Failed to send RPC response:", err)
					return
				}
			}
		}()
	}
}

func processRPCrequest(request protocol.RPCMsg) (protocol.RPCMsg, error) {
	// 处理RPC请求逻辑
	// .............
	// 返回RPC响应或错误
	return protocol.RPCMsg{
		Header:        request.Header,
		ServiceAppID:  request.ServiceAppID,
		ServiceClass:  request.ServiceClass,
		ServiceMethod: request.ServiceMethod,
	}, nil
}

func getRetryCount(headers []*sarama.RecordHeader) int {
	for _, header := range headers {
		if string(header.Key) == "retryCount" {
			count := string(header.Value)
			retryCount, _ := strconv.Atoi(count)
			return retryCount
		}
	}
	return 0
}

func setRetryCount(headers []*sarama.RecordHeader, count int) {
	for i, header := range headers {
		if string(header.Key) == "retryCount" {
			headers[i].Value = []byte(strconv.Itoa(count))
			return
		}
	}

	headers = append(headers, &sarama.RecordHeader{
		Key:   []byte("retryCount"),
		Value: []byte(strconv.Itoa(count)),
	})
}

func main() {
	// 创建Kafka配置
	config := sarama.NewConfig()
	config.Consumer.Return.Errors = true

	// 创建RPC消费者
	consumer, err := sarama.NewConsumerGroup([]string{"localhost:9092"}, "rpc-group", config)
	if err != nil {
		log.Fatal("Failed to create consumer group:", err)
	}
	defer consumer.Close()

	// 创建RPC生产者
	producer, err := sarama.NewSyncProducer([]string{"localhost:9092"}, config)
	if err != nil {
		log.Fatal("Failed to create producer:", err)
	}
	defer producer.Close()

	// 创建重试消息生产者
	retryProducer, err := sarama.NewSyncProducer([]string{"localhost:9092"}, config)
	if err != nil {
		log.Fatal("Failed to create retry producer:", err)
	}
	defer retryProducer.Close()

	// 创建重试消息消费者
	retryConsumer, err := sarama.NewConsumerGroup([]string{"localhost:9092"}, "retry-group", config)
	if err != nil {
		log.Fatal("Failed to create retry consumer group:", err)
	}
	defer retryConsumer.Close()

	// 创建RPC消费者处理程序
	rpcConsumerHandler := &rpcConsumerHandler{
		producer:        producer,
		retryProducer:   retryProducer,
		retryConsumer:   retryConsumer,
		retryBufferSize: 1000,
	}

	// 创建重试消息消费者处理程序
	retryConsumerHandler := &retryConsumerHandler{
		producer:   producer,
		bufferSize: 1000,
	}

	// 启动RPC消费者
	go func() {
		for {
			err := consumer.Consume(context.Background(), []string{RequestTopic}, rpcConsumerHandler)
			if err != nil {
				log.Println("RPC consumer error:", err)
			}
		}
	}()

	// 启动重试消息消费者
	go func() {
		for {
			err := retryConsumer.Consume(context.Background(), []string{RetryTopic}, retryConsumerHandler)
			if err != nil {
				log.Println("Retry consumer error:", err)
			}
		}
	}()

	// 等待程序退出
	select {}
}
