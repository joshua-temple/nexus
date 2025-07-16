package test

import (
	"fmt"
	"time"

	broker "github.com/joshua-temple/nexus"
)

// This example demonstrates how a great-grandchild can reply directly to the parent,
// bypassing intermediate levels using the ReplyToTopics feature.
//
// Hierarchy:
// OrderProcessing -> InventoryCheck -> WarehouseQuery -> StockVerification
//                 ^                                    |
//                 |____________________________________| (direct reply)

// Data structures
type OrderProcessingRequest struct {
	OrderID    string   `json:"order_id"`
	CustomerID string   `json:"customer_id"`
	Items      []string `json:"items"`
	Priority   string   `json:"priority"`
}

type OrderProcessingResult struct {
	OrderID           string `json:"order_id"`
	Status            string `json:"status"`
	EstimatedShipDate string `json:"estimated_ship_date"`
	WarehouseLocation string `json:"warehouse_location"`
	StockVerified     bool   `json:"stock_verified"`
	ExpressShipping   bool   `json:"express_shipping"`
}

type InventoryCheckRequest struct {
	OrderID  string   `json:"order_id"`
	Items    []string `json:"items"`
	Priority string   `json:"priority"`
	// Include parent's reply topic for direct response
	ParentReplyTopic string `json:"parent_reply_topic"`
}

type InventoryCheckResult struct {
	Available bool   `json:"available"`
	Location  string `json:"location"`
}

type WarehouseQueryRequest struct {
	OrderID  string   `json:"order_id"`
	Items    []string `json:"items"`
	Priority string   `json:"priority"`
	// Propagate parent's reply topic
	GrandparentReplyTopic string `json:"grandparent_reply_topic"`
}

type WarehouseQueryResult struct {
	WarehouseID string `json:"warehouse_id"`
	HasStock    bool   `json:"has_stock"`
}

type StockVerificationRequest struct {
	OrderID     string   `json:"order_id"`
	WarehouseID string   `json:"warehouse_id"`
	Items       []string `json:"items"`
	Priority    string   `json:"priority"`
	// Great-grandparent's reply topic for direct response
	GreatGrandparentReplyTopic string `json:"great_grandparent_reply_topic"`
}

type StockVerificationResult struct {
	Verified          bool   `json:"verified"`
	WarehouseLocation string `json:"warehouse_location"`
}

// Direct reply from great-grandchild to parent
type ExpressShippingNotification struct {
	OrderID         string `json:"order_id"`
	ExpressEligible bool   `json:"express_eligible"`
	Reason          string `json:"reason"`
}

// Topics
var (
	TopicOrderProcessing     = mustTopic("orders", "processing", 1).WithPurpose("process").WithTopicType(broker.TopicTypeCommand)
	TopicInventoryCheck      = mustTopic("inventory", "check", 1).WithPurpose("verify").WithTopicType(broker.TopicTypeCommand)
	TopicWarehouseQuery      = mustTopic("warehouse", "query", 1).WithPurpose("find").WithTopicType(broker.TopicTypeCommand)
	TopicStockVerification   = mustTopic("stock", "verification", 1).WithPurpose("verify").WithTopicType(broker.TopicTypeCommand)
	TopicExpressNotification = mustTopic("shipping", "express", 1).WithPurpose("notify").WithTopicType(broker.TopicTypeEvent)
)

// Parent Handler
type OrderProcessingHandler struct{}

func (h *OrderProcessingHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicOrderProcessing}
}

func (h *OrderProcessingHandler) Handle(ctx *broker.Context) error {
	var req OrderProcessingRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Track if we receive direct express shipping notification
	var expressShipping bool
	var expressNotificationReceived = make(chan bool, 1)

	// Listen for direct express shipping notification
	go func() {
		// In a real implementation, this would be handled by the broker's
		// response routing mechanism. For demo purposes, we're simulating it.
		// The great-grandchild would send directly to this handler's reply topic.
		expressNotificationReceived <- req.Priority == "express"
	}()

	// Dispatch to inventory check with our reply topic
	inventoryResult := broker.DispatchTyped[InventoryCheckResult](
		ctx,
		*TopicInventoryCheck,
		InventoryCheckRequest{
			OrderID:          req.OrderID,
			Items:            req.Items,
			Priority:         req.Priority,
			ParentReplyTopic: ctx.Envelope.SourceTopic, // Pass our topic for direct replies
		},
		broker.WithRequired(),
	)

	// Wait for required response
	if err := ctx.Wait(); err != nil {
		return err
	}

	inventory, err := inventoryResult.Value()
	if err != nil {
		return err
	}

	// Check for express shipping notification (non-blocking)
	select {
	case expressShipping = <-expressNotificationReceived:
		fmt.Printf("Received direct express shipping notification: %v\n", expressShipping)
	case <-time.After(100 * time.Millisecond):
		// Continue without express shipping
	}

	// Prepare response
	return ctx.Reply(OrderProcessingResult{
		OrderID:           req.OrderID,
		Status:            "confirmed",
		EstimatedShipDate: calculateShipDate(expressShipping),
		WarehouseLocation: inventory.Location,
		StockVerified:     inventory.Available,
		ExpressShipping:   expressShipping,
	})
}

// Child Handler
type InventoryCheckHandler struct{}

func (h *InventoryCheckHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicInventoryCheck}
}

func (h *InventoryCheckHandler) Handle(ctx *broker.Context) error {
	var req InventoryCheckRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Dispatch to warehouse query, passing along parent's reply topic
	warehouseResult := broker.DispatchTyped[WarehouseQueryResult](
		ctx,
		*TopicWarehouseQuery,
		WarehouseQueryRequest{
			OrderID:               req.OrderID,
			Items:                 req.Items,
			Priority:              req.Priority,
			GrandparentReplyTopic: req.ParentReplyTopic, // Propagate parent's topic
		},
		broker.WithRequired(),
	)

	if err := ctx.Wait(); err != nil {
		return err
	}

	warehouse, err := warehouseResult.Value()
	if err != nil {
		return err
	}

	// Reply to our immediate parent
	return ctx.Reply(InventoryCheckResult{
		Available: warehouse.HasStock,
		Location:  warehouse.WarehouseID,
	})
}

// Grandchild Handler
type WarehouseQueryHandler struct{}

func (h *WarehouseQueryHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicWarehouseQuery}
}

func (h *WarehouseQueryHandler) Handle(ctx *broker.Context) error {
	var req WarehouseQueryRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Find best warehouse
	warehouseID := "warehouse-east"
	if req.Priority == "express" {
		warehouseID = "warehouse-central" // Closer for express
	}

	// Dispatch to stock verification, passing great-grandparent's topic
	stockResult := broker.DispatchTyped[StockVerificationResult](
		ctx,
		*TopicStockVerification,
		StockVerificationRequest{
			OrderID:                    req.OrderID,
			WarehouseID:                warehouseID,
			Items:                      req.Items,
			Priority:                   req.Priority,
			GreatGrandparentReplyTopic: req.GrandparentReplyTopic, // Pass it down
		},
		broker.WithRequired(),
	)

	if err := ctx.Wait(); err != nil {
		return err
	}

	stock, err := stockResult.Value()
	if err != nil {
		return err
	}

	// Reply to our immediate parent
	return ctx.Reply(WarehouseQueryResult{
		WarehouseID: warehouseID,
		HasStock:    stock.Verified,
	})
}

// Great-grandchild Handler
type StockVerificationHandler struct{}

func (h *StockVerificationHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicStockVerification}
}

func (h *StockVerificationHandler) Handle(ctx *broker.Context) error {
	var req StockVerificationRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Perform stock verification
	verified := true // Simulated
	location := req.WarehouseID + "-section-A"

	// If this is an express order and we have the great-grandparent's reply topic,
	// send a direct notification back to the parent
	if req.Priority == "express" && req.GreatGrandparentReplyTopic != "" {
		// Create a custom envelope to reply directly to the great-grandparent
		directReplyEnv := &broker.Envelope{
			ID:               fmt.Sprintf("direct-reply-%s", req.OrderID),
			RootID:           ctx.Envelope.RootID,
			SourceTopic:      ctx.Envelope.DestinationTopic,
			DestinationTopic: req.GreatGrandparentReplyTopic,
			Payload: ExpressShippingNotification{
				OrderID:         req.OrderID,
				ExpressEligible: verified && req.WarehouseID == "warehouse-central",
				Reason:          "Stock available in central warehouse for express delivery",
			},
		}

		// Send direct notification (fire-and-forget)
		ctx.Dispatch(*TopicExpressNotification, directReplyEnv, broker.AsEvent())

		fmt.Printf("Great-grandchild sent direct express notification to parent\n")
	}

	// Also reply normally to our immediate parent (grandchild)
	return ctx.Reply(StockVerificationResult{
		Verified:          verified,
		WarehouseLocation: location,
	})
}

// Helper functions
func calculateShipDate(express bool) string {
	if express {
		return time.Now().Add(24 * time.Hour).Format("2006-01-02")
	}
	return time.Now().Add(72 * time.Hour).Format("2006-01-02")
}

// Example_directReply demonstrates direct replies from great-grandchild to parent
func Example_directReply() {
	fmt.Println("Direct Reply Example: Great-grandchild replies directly to parent")
	fmt.Println("OrderProcessing -> InventoryCheck -> WarehouseQuery -> StockVerification")
	fmt.Println("StockVerification can send express shipping notification directly to OrderProcessing")

	// Output:
	// Direct Reply Example: Great-grandchild replies directly to parent
	// OrderProcessing -> InventoryCheck -> WarehouseQuery -> StockVerification
	// StockVerification can send express shipping notification directly to OrderProcessing
}
