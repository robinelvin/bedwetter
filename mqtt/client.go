package mqtt

import (
	"fmt"
	"log"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type MessageHandler func(topic string, payload []byte)

type Client struct {
	client  mqtt.Client
	broker  string
	handlers map[string]MessageHandler
}

func New(broker string, port int, username, password string) *Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("tcp://%s:%d", broker, port))
	opts.SetClientID(fmt.Sprintf("bedwetter-%d", time.Now().UnixNano()))
	opts.SetUsername(username)
	opts.SetPassword(password)
	opts.SetKeepAlive(30 * time.Second)
	opts.SetAutoReconnect(true)
	opts.SetMaxReconnectInterval(30 * time.Second)
	opts.SetConnectionLostHandler(func(c mqtt.Client, err error) {
		log.Printf("MQTT connection lost: %v", err)
	})
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Println("MQTT connected/reconnected")
	})

	client := mqtt.NewClient(opts)
	return &Client{
		client:   client,
		broker:   broker,
		handlers: make(map[string]MessageHandler),
	}
}

func (c *Client) Connect() error {
	token := c.client.Connect()
	token.WaitTimeout(10 * time.Second)
	return token.Error()
}

func (c *Client) Subscribe(topic string, qos byte, handler MessageHandler) error {
	c.handlers[topic] = handler
	token := c.client.Subscribe(topic, qos, func(_ mqtt.Client, msg mqtt.Message) {
		if h, ok := c.handlers[msg.Topic()]; ok {
			h(msg.Topic(), msg.Payload())
		}
	})
	token.WaitTimeout(10 * time.Second)
	return token.Error()
}

func (c *Client) SubscribeMultiple(topics map[string]byte, handler MessageHandler) error {
	filters := make(map[string]byte)
	for t, qos := range topics {
		filters[t] = qos
		c.handlers[t] = handler
	}
	token := c.client.SubscribeMultiple(filters, func(_ mqtt.Client, msg mqtt.Message) {
		if h, ok := c.handlers[msg.Topic()]; ok {
			h(msg.Topic(), msg.Payload())
		}
	})
	token.WaitTimeout(10 * time.Second)
	return token.Error()
}

func (c *Client) Publish(topic string, qos byte, retained bool, payload string) error {
	token := c.client.Publish(topic, qos, retained, payload)
	token.WaitTimeout(5 * time.Second)
	return token.Error()
}

func (c *Client) IsConnected() bool {
	return c.client != nil && c.client.IsConnected()
}

func (c *Client) Unsubscribe(topics ...string) {
	for _, topic := range topics {
		delete(c.handlers, topic)
	}
	c.client.Unsubscribe(topics...).WaitTimeout(5 * time.Second)
}

func (c *Client) Disconnect(quiesce uint) {
	c.client.Disconnect(quiesce)
}
