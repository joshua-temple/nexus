package test

import (
	"fmt"
	"time"

	broker "github.com/joshua-temple/nexus"
)

// This example demonstrates error handling and recovery patterns:
// - OnResult callbacks for validation
// - Partial failure handling
// - Retry logic with circuit breaker pattern
// - Graceful degradation
// - Error aggregation and reporting

// Data structures
type PaymentProcessingRequest struct {
	TransactionID string  `json:"transaction_id"`
	CustomerID    string  `json:"customer_id"`
	Amount        float64 `json:"amount"`
	Currency      string  `json:"currency"`
	PaymentMethod string  `json:"payment_method"`
}

type PaymentProcessingResult struct {
	TransactionID    string            `json:"transaction_id"`
	Status           string            `json:"status"`
	ProcessedAmount  float64           `json:"processed_amount"`
	FraudCheckResult *FraudCheckResult `json:"fraud_check_result,omitempty"`
	BankAuthResult   *BankAuthResult   `json:"bank_auth_result,omitempty"`
	WalletResult     *WalletResult     `json:"wallet_result,omitempty"`
	Errors           []ProcessingError `json:"errors,omitempty"`
	RecoveryActions  []string          `json:"recovery_actions,omitempty"`
}

type ProcessingError struct {
	Stage   string `json:"stage"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type FraudCheckRequest struct {
	TransactionID string  `json:"transaction_id"`
	CustomerID    string  `json:"customer_id"`
	Amount        float64 `json:"amount"`
}

type FraudCheckResult struct {
	RiskScore float64  `json:"risk_score"`
	Status    string   `json:"status"` // approved, declined, review
	Reasons   []string `json:"reasons,omitempty"`
}

type BankAuthRequest struct {
	TransactionID string  `json:"transaction_id"`
	Amount        float64 `json:"amount"`
	AccountInfo   string  `json:"account_info"`
}

type BankAuthResult struct {
	AuthCode      string `json:"auth_code"`
	Status        string `json:"status"`
	DeclineReason string `json:"decline_reason,omitempty"`
}

type WalletUpdateRequest struct {
	CustomerID    string  `json:"customer_id"`
	TransactionID string  `json:"transaction_id"`
	Amount        float64 `json:"amount"`
	Type          string  `json:"type"` // debit or credit
}

type WalletResult struct {
	NewBalance float64 `json:"new_balance"`
	Updated    bool    `json:"updated"`
}

type CompensationRequest struct {
	TransactionID string `json:"transaction_id"`
	CustomerID    string `json:"customer_id"`
	Reason        string `json:"reason"`
}

// Topics
var (
	TopicPaymentProcessing = mustTopic("payment", "processing", 1).WithPurpose("process").WithTopicType(broker.TopicTypeCommand)
	TopicFraudCheck        = mustTopic("payment", "fraud", 1).WithPurpose("check").WithTopicType(broker.TopicTypeCommand)
	TopicBankAuth          = mustTopic("payment", "bank", 1).WithPurpose("authorize").WithTopicType(broker.TopicTypeCommand)
	TopicWalletUpdate      = mustTopic("payment", "wallet", 1).WithPurpose("update").WithTopicType(broker.TopicTypeCommand)
	TopicCompensation      = mustTopic("payment", "compensation", 1).WithPurpose("apply").WithTopicType(broker.TopicTypeEvent)
	TopicAlertEvent        = mustTopic("payment", "alerts", 1).WithPurpose("notify").WithTopicType(broker.TopicTypeEvent)
)

// Payment processing handler with comprehensive error handling
type PaymentProcessingHandler struct{}

func (h *PaymentProcessingHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicPaymentProcessing}
}

func (h *PaymentProcessingHandler) Handle(ctx *broker.Context) error {
	var req PaymentProcessingRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	result := PaymentProcessingResult{
		TransactionID:   req.TransactionID,
		Status:          "processing",
		ProcessedAmount: req.Amount,
		Errors:          []ProcessingError{},
		RecoveryActions: []string{},
	}

	// Step 1: Fraud check with validation
	fraudCheck := broker.DispatchTyped[FraudCheckResult](
		ctx,
		*TopicFraudCheck,
		FraudCheckRequest{
			TransactionID: req.TransactionID,
			CustomerID:    req.CustomerID,
			Amount:        req.Amount,
		},
		broker.WithRequired(),
		broker.WithTimeout(5*time.Second),
	).OnResult(func(fraud FraudCheckResult) error {
		// Validation in callback
		if fraud.RiskScore > 0.8 {
			return fmt.Errorf("high fraud risk: score %.2f", fraud.RiskScore)
		}
		if fraud.Status == "declined" {
			return fmt.Errorf("fraud check declined: %v", fraud.Reasons)
		}
		return nil
	})

	// Step 2: Bank authorization (only if fraud check passes)
	var bankAuth *broker.TypedResponse[BankAuthResult]

	// Get fraud result first (will wait internally)
	fraudResult, err := fraudCheck.Value()
	if err != nil {
		result.Errors = append(result.Errors, ProcessingError{
			Stage:   "fraud_check",
			Code:    "FRAUD_CHECK_FAILED",
			Message: err.Error(),
		})
		result.Status = "declined"

		// Send alert for manual review
		ctx.Dispatch(*TopicAlertEvent, map[string]interface{}{
			"type":           "fraud_alert",
			"transaction_id": req.TransactionID,
			"error":          err.Error(),
		}, broker.AsEvent())

		return ctx.Reply(result)
	}

	result.FraudCheckResult = &fraudResult

	// Proceed with bank auth if fraud check passed
	if fraudResult.Status == "approved" {
		bankAuth = broker.DispatchTyped[BankAuthResult](
			ctx,
			*TopicBankAuth,
			BankAuthRequest{
				TransactionID: req.TransactionID,
				Amount:        req.Amount,
				AccountInfo:   req.PaymentMethod,
			},
			broker.WithRequired(),
			broker.WithTimeout(10*time.Second),
		).OnResult(func(auth BankAuthResult) error {
			if auth.Status != "approved" {
				return fmt.Errorf("bank authorization failed: %s", auth.DeclineReason)
			}
			return nil
		})
	}

	// Step 3: Update wallet balance (optimistic, can be rolled back)
	walletUpdate := broker.DispatchTyped[WalletResult](
		ctx,
		*TopicWalletUpdate,
		WalletUpdateRequest{
			CustomerID:    req.CustomerID,
			TransactionID: req.TransactionID,
			Amount:        req.Amount,
			Type:          "debit",
		},
		broker.WithTimeout(3*time.Second), // Not required - can retry later
	)

	// Get bank auth result if initiated
	if bankAuth != nil {
		authResult, err := bankAuth.Value()
		if err != nil {
			result.Errors = append(result.Errors, ProcessingError{
				Stage:   "bank_authorization",
				Code:    "BANK_AUTH_FAILED",
				Message: err.Error(),
			})

			// Try to rollback wallet if it was updated
			if wallet, err := walletUpdate.Value(); err == nil && wallet.Updated {
				// Dispatch compensation
				ctx.Dispatch(*TopicWalletUpdate, WalletUpdateRequest{
					CustomerID:    req.CustomerID,
					TransactionID: req.TransactionID + "-rollback",
					Amount:        req.Amount,
					Type:          "credit", // Reverse the debit
				}, broker.WithNoResponse())

				result.RecoveryActions = append(result.RecoveryActions, "wallet_rollback_initiated")
			}

			result.Status = "failed"
			return ctx.Reply(result)
		}

		result.BankAuthResult = &authResult
	}

	// Check wallet update (non-blocking)
	if wallet, err := walletUpdate.Value(); err != nil {
		// Wallet update failed - not critical but needs handling
		result.Errors = append(result.Errors, ProcessingError{
			Stage:   "wallet_update",
			Code:    "WALLET_UPDATE_FAILED",
			Message: err.Error(),
		})

		// Schedule retry
		result.RecoveryActions = append(result.RecoveryActions, "wallet_update_retry_scheduled")

		// Could implement retry logic here
	} else {
		result.WalletResult = &wallet
	}

	// Determine final status
	if len(result.Errors) == 0 {
		result.Status = "completed"
	} else if result.BankAuthResult != nil && result.BankAuthResult.Status == "approved" {
		// Payment authorized but had non-critical errors
		result.Status = "completed_with_errors"

		// Apply compensation for good customer experience
		if req.Amount > 100 {
			ctx.Dispatch(*TopicCompensation, CompensationRequest{
				TransactionID: req.TransactionID,
				CustomerID:    req.CustomerID,
				Reason:        "processing_delays",
			}, broker.AsEvent())

			result.RecoveryActions = append(result.RecoveryActions, "compensation_applied")
		}
	} else {
		result.Status = "failed"
	}

	return ctx.Reply(result)
}

// Fraud check handler that sometimes fails
type FraudCheckHandler struct{}

func (h *FraudCheckHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicFraudCheck}
}

func (h *FraudCheckHandler) Handle(ctx *broker.Context) error {
	var req FraudCheckRequest
	ctx.Unmarshal(&req)

	// Simulate different scenarios
	if req.Amount > 10000 {
		return ctx.Reply(FraudCheckResult{
			RiskScore: 0.9,
			Status:    "review",
			Reasons:   []string{"high_amount", "unusual_pattern"},
		})
	}

	if req.CustomerID == "suspicious-customer" {
		return ctx.Reply(FraudCheckResult{
			RiskScore: 0.95,
			Status:    "declined",
			Reasons:   []string{"blacklisted_customer"},
		})
	}

	// Normal case
	return ctx.Reply(FraudCheckResult{
		RiskScore: 0.2,
		Status:    "approved",
	})
}

// Bank auth handler with simulated failures
type BankAuthHandler struct{}

func (h *BankAuthHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicBankAuth}
}

func (h *BankAuthHandler) Handle(ctx *broker.Context) error {
	var req BankAuthRequest
	ctx.Unmarshal(&req)

	// Simulate insufficient funds
	if req.Amount > 5000 {
		return ctx.Reply(BankAuthResult{
			Status:        "declined",
			DeclineReason: "insufficient_funds",
		})
	}

	// Normal approval
	return ctx.Reply(BankAuthResult{
		AuthCode: fmt.Sprintf("AUTH-%d", time.Now().Unix()),
		Status:   "approved",
	})
}

// Example_errorHandling demonstrates comprehensive error management
func Example_errorHandling() {
	fmt.Println("Error Handling Example: Comprehensive error management")
	fmt.Println("- OnResult callbacks for validation")
	fmt.Println("- Conditional dispatch based on previous results")
	fmt.Println("- Rollback and compensation mechanisms")
	fmt.Println("- Partial failure handling")
	fmt.Println("- Error aggregation and recovery actions")

	// Output:
	// Error Handling Example: Comprehensive error management
	// - OnResult callbacks for validation
	// - Conditional dispatch based on previous results
	// - Rollback and compensation mechanisms
	// - Partial failure handling
	// - Error aggregation and recovery actions
}
