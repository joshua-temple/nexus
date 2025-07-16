package examples

import (
	"fmt"
	"time"

	broker "github.com/joshua-temple/nexus"
)

// Example types for an order processing system
type Order struct {
	ID         string  `json:"id"`
	CustomerID string  `json:"customer_id"`
	Items      []Item  `json:"items"`
	Total      float64 `json:"total"`
}

type Item struct {
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
}

type InventoryResponse struct {
	Available     bool   `json:"available"`
	ReservationID string `json:"reservation_id"`
	QuantityLeft  int    `json:"quantity_left"`
}

type PaymentRequest struct {
	CustomerID string  `json:"customer_id"`
	Amount     float64 `json:"amount"`
}

type PaymentResponse struct {
	Status        string  `json:"status"`
	TransactionID string  `json:"transaction_id"`
	Amount        float64 `json:"amount"`
	Reason        string  `json:"reason,omitempty"`
}

type ShippingResponse struct {
	Cost              float64 `json:"cost"`
	EstimatedDelivery string  `json:"estimated_delivery"`
}

type OrderConfirmation struct {
	OrderID           string `json:"order_id"`
	Status            string `json:"status"`
	ReservationID     string `json:"reservation_id"`
	PaymentID         string `json:"payment_id"`
	EstimatedDelivery string `json:"estimated_delivery,omitempty"`
}

type OrderNotification struct {
	OrderID    string `json:"order_id"`
	CustomerID string `json:"customer_id"`
	Message    string `json:"message"`
}

// createTopic is a helper for creating example topics
func createTopic(team, domain string, version int) *broker.Topic {
	t, err := broker.NewTopic(team, domain, version)
	if err != nil {
		panic(err)
	}
	return t
}

// Topics used in the example
var (
	TopicCreateOrder       = createTopic("orders", "handler", 1).WithPurpose("create").WithTopicType(broker.TopicTypeCommand)
	TopicValidateInventory = createTopic("inventory", "handler", 1).WithPurpose("validate").WithTopicType(broker.TopicTypeCommand)
	TopicChargePayment     = createTopic("payment", "handler", 1).WithPurpose("charge").WithTopicType(broker.TopicTypeCommand)
	TopicCalculateShipping = createTopic("shipping", "handler", 1).WithPurpose("calculate").WithTopicType(broker.TopicTypeCommand)
	TopicNotifyCustomer    = createTopic("notifications", "handler", 1).WithPurpose("customer").WithTopicType(broker.TopicTypeEvent)
)

// CreateOrderHandler demonstrates minimal boilerplate for a parent handler
type CreateOrderHandler struct{}

func (h *CreateOrderHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicCreateOrder}
}

func (h *CreateOrderHandler) Handle(ctx *broker.Context) error {
	// 1. Unmarshal the incoming order
	var order Order
	if err := ctx.Unmarshal(&order); err != nil {
		return fmt.Errorf("failed to unmarshal order: %w", err)
	}

	// 2. Dispatch child commands with type safety
	inventory := broker.DispatchTyped[InventoryResponse](
		ctx,
		*TopicValidateInventory,
		order.Items,
		broker.WithRequired(),
	).OnResult(func(resp InventoryResponse) error {
		if !resp.Available {
			return fmt.Errorf("insufficient inventory")
		}
		return nil
	})

	payment := broker.DispatchTyped[PaymentResponse](
		ctx,
		*TopicChargePayment,
		PaymentRequest{
			CustomerID: order.CustomerID,
			Amount:     order.Total,
		},
		broker.WithRequired(),
		broker.WithTimeout(45*time.Second),
	).OnResult(func(resp PaymentResponse) error {
		if resp.Status != "approved" {
			return fmt.Errorf("payment failed: %s", resp.Reason)
		}
		return nil
	})

	// Optional shipping calculation (response expected but not required)
	shipping := broker.DispatchTyped[ShippingResponse](
		ctx,
		*TopicCalculateShipping,
		order,
		// No WithRequired(), so it's optional
	)

	// Fire-and-forget notification (no response expected)
	ctx.Dispatch(*TopicNotifyCustomer, OrderNotification{
		OrderID:    order.ID,
		CustomerID: order.CustomerID,
		Message:    "Your order is being processed",
	}, broker.AsEvent())

	// 3. Wait for all required responses (inventory + payment)
	if err := ctx.Wait(); err != nil {
		return err
	}

	// 4. Get typed values - errors already checked in OnResult
	invResp, err := inventory.Value()
	if err != nil {
		return fmt.Errorf("inventory validation failed: %w", err)
	}

	payResp, err := payment.Value()
	if err != nil {
		return fmt.Errorf("payment processing failed: %w", err)
	}

	// Optional shipping might fail but won't stop the order
	shipResp, _ := shipping.Value()

	// 5. Send confirmation response
	return ctx.Reply(OrderConfirmation{
		OrderID:           order.ID,
		Status:            "confirmed",
		ReservationID:     invResp.ReservationID,
		PaymentID:         payResp.TransactionID,
		EstimatedDelivery: shipResp.EstimatedDelivery,
	})
}

// InventoryHandler can also dispatch its own child commands
type InventoryHandler struct{}

func (h *InventoryHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicValidateInventory}
}

func (h *InventoryHandler) Handle(ctx *broker.Context) error {
	var items []Item
	if err := ctx.Unmarshal(&items); err != nil {
		return err
	}

	// Could dispatch to multiple warehouses here...
	// warehouse1 := ctx.Dispatch[WarehouseResponse](...)
	// warehouse2 := ctx.Dispatch[WarehouseResponse](...)

	// For this example, just return success
	return ctx.Reply(InventoryResponse{
		Available:     true,
		ReservationID: "RES-12345",
		QuantityLeft:  100,
	})
}

// PaymentHandler processes payments
type PaymentHandler struct{}

func (h *PaymentHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicChargePayment}
}

func (h *PaymentHandler) Handle(ctx *broker.Context) error {
	var req PaymentRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Simulate payment processing
	return ctx.Reply(PaymentResponse{
		Status:        "approved",
		TransactionID: "TXN-67890",
		Amount:        req.Amount,
	})
}

// Example usage showing minimal setup
func ExampleBroker() {
	// Create publisher/subscriber implementations (Kafka, RabbitMQ, etc)
	var publisher broker.Publisher   // = NewKafkaPublisher(...)
	var subscriber broker.Subscriber // = NewKafkaSubscriber(...)

	// Create broker
	b := broker.New(10, publisher, subscriber, nil)

	// Register handlers - that's it!
	_ = b.Register(
		&CreateOrderHandler{},
		&InventoryHandler{},
		&PaymentHandler{},
	)

	// Start processing
	// ctx := context.Background()
	// b.Start(ctx)
	// defer b.Stop()
}
