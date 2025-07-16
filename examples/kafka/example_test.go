package kafka_test

import (
	"fmt"

	broker "github.com/joshua-temple/nexus"
	"github.com/joshua-temple/nexus/examples/kafka"
)

func ExampleNewPublisher() {
	// Create a publisher
	brokers := []string{"localhost:19092"}
	pub, err := kafka.NewPublisher(brokers)
	if err != nil {
		fmt.Printf("Failed to create publisher: %v\n", err)
		return
	}
	defer func() { _ = pub.Close() }()

	// In a real application, you would create an envelope and publish like this:
	// envelope := &broker.Envelope{
	//     ID:               "example-1",
	//     RootID:           "root-example",
	//     SourceTopic:      "example-service",
	//     DestinationTopic: "example.topic",
	//     Payload:          map[string]string{"message": "Hello, RedPanda!"},
	//     CreatedAt:        time.Now().UnixNano(),
	// }
	// ctx := context.Background()
	// if err := pub.Publish(ctx, envelope); err != nil {
	//     log.Fatalf("Failed to publish: %v", err)
	// }

	fmt.Printf("Publisher created for brokers: %v\n", brokers)
	// Output: Publisher created for brokers: [localhost:19092]
}

func ExampleNewSubscriber() {
	// Create a subscriber
	brokers := []string{"localhost:19092"}
	topics := []string{"example.topic"}
	consumerGroup := "example-group"

	_, err := kafka.NewSubscriber(brokers, topics, consumerGroup, nil)
	if err != nil {
		fmt.Printf("Failed to create subscriber: %v\n", err)
		return
	}

	// In a real application, you would start consuming like this:
	// sub, err := kafka.NewSubscriber(brokers, topics, consumerGroup, nil)
	// if err != nil {
	//     log.Fatalf("Failed to create subscriber: %v", err)
	// }
	// ctx := context.Background()
	// msgChan, err := sub.Start(ctx)
	// if err != nil {
	//     log.Fatalf("Failed to start subscriber: %v", err)
	// }
	// defer sub.Stop()
	//
	// // Process messages
	// go func() {
	//     for msg := range msgChan {
	//         // Process the message
	//         fmt.Printf("Received message: %s\n", msg.ID)
	//
	//         // Handle payload
	//         if payload, ok := msg.Payload.(map[string]interface{}); ok {
	//             fmt.Printf("Payload: %v\n", payload)
	//         }
	//     }
	// }()

	fmt.Printf("Subscriber created for topics: %v, group: %s\n", topics, consumerGroup)
	// Output: Subscriber created for topics: [example.topic], group: example-group
}

// Example of using RedPanda with the broker framework  
func Example_brokerIntegration() {
	brokers := []string{"localhost:19092"}

	// Create publisher and subscriber using compatibility wrappers
	// These wrappers maintain the original interface while using franz-go underneath
	pub := kafka.NewPublisherCompat(brokers)
	defer func() { _ = pub.Close() }()

	topics := []string{"orders.process.cmd.v1", "orders.validate.cmd.v1"}
	sub := kafka.NewSubscriberCompat(brokers, topics, "order-service", nil)

	// Create broker
	b := broker.New(4, pub, sub, nil)

	// Define topics
	processTopic, _ := broker.NewTopic("orders", "process", 1)
	processTopic = processTopic.WithPurpose("command").WithTopicType(broker.TopicTypeCommand)

	validateTopic, _ := broker.NewTopic("orders", "validate", 1)
	validateTopic = validateTopic.WithPurpose("command").WithTopicType(broker.TopicTypeCommand)

	// Register handlers
	_ = b.Register(&orderProcessor{
		processTopic:  *processTopic,
		validateTopic: *validateTopic,
	})
	_ = b.Register(&orderValidator{
		topic: *validateTopic,
	})

	fmt.Printf("Broker configured with %d workers for topics: %v\n", 4, topics)
	// Output: Broker configured with 4 workers for topics: [orders.process.cmd.v1 orders.validate.cmd.v1]
}

// Example handlers
type orderProcessor struct {
	processTopic  broker.Topic
	validateTopic broker.Topic
}

func (h *orderProcessor) Topics() []broker.Topic {
	return []broker.Topic{h.processTopic}
}

func (h *orderProcessor) Handle(ctx *broker.Context) error {
	var order map[string]interface{}
	if err := ctx.Unmarshal(&order); err != nil {
		return err
	}

	// Dispatch to validator
	validationResp := broker.DispatchTyped[map[string]bool](
		ctx,
		h.validateTopic,
		order,
		broker.WithRequired(),
	)

	// Wait for validation
	if err := ctx.Wait(); err != nil {
		return err
	}

	// Get validation result
	result, err := validationResp.Value()
	if err != nil {
		return err
	}

	// Process based on validation
	if result["valid"] {
		return ctx.Reply(map[string]string{
			"status": "processed",
			"id":     order["id"].(string),
		})
	}

	return ctx.Reply(map[string]string{
		"status": "rejected",
		"reason": "validation failed",
	})
}

type orderValidator struct {
	topic broker.Topic
}

func (h *orderValidator) Topics() []broker.Topic {
	return []broker.Topic{h.topic}
}

func (h *orderValidator) Handle(ctx *broker.Context) error {
	var order map[string]interface{}
	if err := ctx.Unmarshal(&order); err != nil {
		return err
	}

	// Simple validation
	valid := true
	if amount, ok := order["amount"].(float64); ok {
		if amount <= 0 {
			valid = false
		}
	} else {
		valid = false
	}

	return ctx.Reply(map[string]bool{
		"valid": valid,
	})
}
