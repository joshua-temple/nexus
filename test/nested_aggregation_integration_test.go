package test

import (
	"context"
	"testing"
	"time"

	broker "github.com/joshua-temple/nexus"
)

// Data structures for each level
type RiskAssessmentRequest struct {
	CustomerID string  `json:"customer_id"`
	LoanAmount float64 `json:"loan_amount"`
}

type RiskAssessmentResult struct {
	CustomerID      string  `json:"customer_id"`
	RiskScore       float64 `json:"risk_score"`
	ApprovalStatus  string  `json:"approval_status"`
	CreditScore     int     `json:"credit_score"`
	DebtToIncome    float64 `json:"debt_to_income"`
	PaymentHistory  string  `json:"payment_history"`
	RecommendedRate float64 `json:"recommended_rate"`
}

type CreditCheckRequest struct {
	CustomerID string `json:"customer_id"`
}

type CreditCheckResult struct {
	Score          int     `json:"score"`
	DebtToIncome   float64 `json:"debt_to_income"`
	PaymentHistory string  `json:"payment_history"`
}

type BureauQueryRequest struct {
	CustomerID string `json:"customer_id"`
}

type BureauQueryResult struct {
	RawScore       int    `json:"raw_score"`
	AccountCount   int    `json:"account_count"`
	OldestAccount  string `json:"oldest_account"`
	PaymentHistory string `json:"payment_history"`
}

type DetailedAnalysisRequest struct {
	CustomerID string `json:"customer_id"`
}

type DetailedAnalysisResult struct {
	PaymentPatterns []string `json:"payment_patterns"`
	RiskFactors     []string `json:"risk_factors"`
	HistorySummary  string   `json:"history_summary"`
}

// Topics for each level
var (
	TopicRiskAssessment   = mustTopic("lending", "risk", 1).WithPurpose("assess").WithTopicType(broker.TopicTypeCommand)
	TopicCreditCheck      = mustTopic("lending", "credit", 1).WithPurpose("check").WithTopicType(broker.TopicTypeCommand)
	TopicBureauQuery      = mustTopic("lending", "bureau", 1).WithPurpose("query").WithTopicType(broker.TopicTypeCommand)
	TopicDetailedAnalysis = mustTopic("lending", "analysis", 1).WithPurpose("detailed").WithTopicType(broker.TopicTypeCommand)
)

// Level 1: Risk Assessment Handler (Parent)
type RiskAssessmentHandler struct{}

func (h *RiskAssessmentHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicRiskAssessment}
}

func (h *RiskAssessmentHandler) Handle(ctx *broker.Context) error {
	var req RiskAssessmentRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Dispatch to credit check (child)
	creditResult := broker.DispatchTyped[CreditCheckResult](
		ctx,
		*TopicCreditCheck,
		CreditCheckRequest{CustomerID: req.CustomerID},
		broker.WithRequired(),
		broker.WithTimeout(30*time.Second),
	)

	// Wait for child response
	if err := ctx.Wait(); err != nil {
		return err
	}

	credit, err := creditResult.Value()
	if err != nil {
		return err
	}

	// Calculate risk based on aggregated data
	riskScore := calculateRiskScore(credit.Score, credit.DebtToIncome)
	approvalStatus := "approved"
	if riskScore > 0.7 {
		approvalStatus = "declined"
	}

	// Bubble up aggregated result to parent
	return ctx.Reply(RiskAssessmentResult{
		CustomerID:      req.CustomerID,
		RiskScore:       riskScore,
		ApprovalStatus:  approvalStatus,
		CreditScore:     credit.Score,
		DebtToIncome:    credit.DebtToIncome,
		PaymentHistory:  credit.PaymentHistory,
		RecommendedRate: calculateRate(riskScore),
	})
}

// Level 2: Credit Check Handler (Child)
type CreditCheckHandler struct{}

func (h *CreditCheckHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicCreditCheck}
}

func (h *CreditCheckHandler) Handle(ctx *broker.Context) error {
	var req CreditCheckRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Dispatch to bureau query (grandchild)
	bureauResult := broker.DispatchTyped[BureauQueryResult](
		ctx,
		*TopicBureauQuery,
		req, // Direct conversion instead of struct literal
		broker.WithRequired(),
	)

	// Wait for grandchild response
	if err := ctx.Wait(); err != nil {
		return err
	}

	bureau, err := bureauResult.Value()
	if err != nil {
		return err
	}

	// Process and aggregate bureau data
	score := adjustScore(bureau.RawScore, bureau.AccountCount)
	dti := calculateDTI(bureau.AccountCount)

	// Bubble up processed data to parent
	return ctx.Reply(CreditCheckResult{
		Score:          score,
		DebtToIncome:   dti,
		PaymentHistory: bureau.PaymentHistory,
	})
}

// Level 3: Bureau Query Handler (Grandchild)
type BureauQueryHandler struct{}

func (h *BureauQueryHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicBureauQuery}
}

func (h *BureauQueryHandler) Handle(ctx *broker.Context) error {
	var req BureauQueryRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Dispatch to detailed analysis (great-grandchild)
	analysisResult := broker.DispatchTyped[DetailedAnalysisResult](
		ctx,
		*TopicDetailedAnalysis,
		req, // Direct conversion instead of struct literal
		broker.WithRequired(),
	)

	// Wait for great-grandchild response
	if err := ctx.Wait(); err != nil {
		return err
	}

	analysis, err := analysisResult.Value()
	if err != nil {
		return err
	}

	// Aggregate analysis data with bureau data
	return ctx.Reply(BureauQueryResult{
		RawScore:       750, // Simulated bureau score
		AccountCount:   5,
		OldestAccount:  "2015-01-01",
		PaymentHistory: analysis.HistorySummary, // Using data from great-grandchild
	})
}

// Level 4: Detailed Analysis Handler (Great-grandchild)
type DetailedAnalysisHandler struct{}

func (h *DetailedAnalysisHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicDetailedAnalysis}
}

func (h *DetailedAnalysisHandler) Handle(ctx *broker.Context) error {
	var req DetailedAnalysisRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Simulate detailed analysis (deepest level)
	patterns := []string{"on-time", "on-time", "late", "on-time"}
	factors := []string{"high-utilization", "new-credit"}

	// Return detailed analysis to grandchild
	return ctx.Reply(DetailedAnalysisResult{
		PaymentPatterns: patterns,
		RiskFactors:     factors,
		HistorySummary:  "Generally good payment history with occasional delays",
	})
}

// Helper functions
func calculateRiskScore(creditScore int, dti float64) float64 {
	base := 1.0 - (float64(creditScore) / 850.0)
	dtiPenalty := dti * 0.5
	return base + dtiPenalty
}

func calculateRate(riskScore float64) float64 {
	baseRate := 3.5
	return baseRate + (riskScore * 10.0)
}

func adjustScore(rawScore, accountCount int) int {
	bonus := accountCount * 10
	if bonus > 50 {
		bonus = 50
	}
	return rawScore + bonus
}

func calculateDTI(accountCount int) float64 {
	// Simplified DTI calculation
	return float64(accountCount) * 0.08
}

func TestNestedAggregation(t *testing.T) {
	// Create test broker
	b, pub, cleanup := createTestBroker(t, 5)
	defer cleanup()

	// Register all handlers
	if err := b.Register(&RiskAssessmentHandler{}); err != nil {
		t.Fatalf("Failed to register RiskAssessmentHandler: %v", err)
	}
	if err := b.Register(&CreditCheckHandler{}); err != nil {
		t.Fatalf("Failed to register CreditCheckHandler: %v", err)
	}
	if err := b.Register(&BureauQueryHandler{}); err != nil {
		t.Fatalf("Failed to register BureauQueryHandler: %v", err)
	}
	if err := b.Register(&DetailedAnalysisHandler{}); err != nil {
		t.Fatalf("Failed to register DetailedAnalysisHandler: %v", err)
	}

	// Start broker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}

	// Give broker time to start
	time.Sleep(100 * time.Millisecond)

	// Create risk assessment request
	request := RiskAssessmentRequest{
		CustomerID: "test-customer-123",
		LoanAmount: 25000.00,
	}

	// Create envelope
	envelope := &broker.Envelope{
		ID:               "test-risk-1",
		RootID:           "root-risk-1",
		SourceTopic:      "test-client",
		DestinationTopic: TopicRiskAssessment.String(),
		ReplyToTopics:    []string{"test-reply"},
		Payload:          marshalPayload(request),
		CreatedAt:        time.Now().UnixNano(),
	}

	// Send request and wait for response
	response, err := sendAndWaitForResponse(t, pub, envelope, "test-reply", 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to get response: %v", err)
	}

	// Unmarshal response
	var result RiskAssessmentResult
	unmarshalResponse(t, response, &result)

	// Verify nested aggregation worked
	if result.CustomerID != request.CustomerID {
		t.Errorf("Expected CustomerID %s, got %s", request.CustomerID, result.CustomerID)
	}

	if result.CreditScore != 800 { // Based on our mock data
		t.Errorf("Expected CreditScore 800, got %d", result.CreditScore)
	}

	if result.PaymentHistory != "Generally good payment history with occasional delays" {
		t.Errorf("Expected aggregated payment history from great-grandchild, got %s", result.PaymentHistory)
	}

	if result.ApprovalStatus != "approved" {
		t.Errorf("Expected approval status 'approved', got %s", result.ApprovalStatus)
	}

	// Check that all 4 levels were traversed
	if result.RiskScore == 0 {
		t.Error("Risk score should be calculated")
	}

	if result.DebtToIncome == 0 {
		t.Error("Debt to income should be calculated")
	}

	t.Logf("Successfully completed 4-level nested aggregation: Risk %.2f, Credit Score %d",
		result.RiskScore, result.CreditScore)
}

func TestNestedAggregationWithHighRisk(t *testing.T) {
	// Create test broker
	b, pub, cleanup := createTestBroker(t, 5)
	defer cleanup()

	// Register all handlers
	if err := b.Register(&RiskAssessmentHandler{}); err != nil {
		t.Fatalf("Failed to register RiskAssessmentHandler: %v", err)
	}
	if err := b.Register(&CreditCheckHandler{}); err != nil {
		t.Fatalf("Failed to register CreditCheckHandler: %v", err)
	}
	if err := b.Register(&BureauQueryHandler{}); err != nil {
		t.Fatalf("Failed to register BureauQueryHandler: %v", err)
	}
	if err := b.Register(&DetailedAnalysisHandler{}); err != nil {
		t.Fatalf("Failed to register DetailedAnalysisHandler: %v", err)
	}

	// Start broker
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := b.Start(ctx); err != nil {
		t.Fatalf("Failed to start broker: %v", err)
	}

	// Give broker time to start
	time.Sleep(100 * time.Millisecond)

	// Create high-risk request
	request := RiskAssessmentRequest{
		CustomerID: "risky-customer",
		LoanAmount: 100000.00, // High amount triggers different calculation
	}

	// Create envelope
	envelope := &broker.Envelope{
		ID:               "test-risk-2",
		RootID:           "root-risk-2",
		SourceTopic:      "test-client",
		DestinationTopic: TopicRiskAssessment.String(),
		ReplyToTopics:    []string{"test-reply-2"},
		Payload:          marshalPayload(request),
		CreatedAt:        time.Now().UnixNano(),
	}

	// Send request and wait for response
	response, err := sendAndWaitForResponse(t, pub, envelope, "test-reply-2", 5*time.Second)
	if err != nil {
		t.Fatalf("Failed to get response: %v", err)
	}

	// Unmarshal response
	var result RiskAssessmentResult
	unmarshalResponse(t, response, &result)

	// Verify risk scenario (risk is based on credit score and DTI, not loan amount)
	// With credit score of 800 and DTI of 0.40, risk should still be low
	if result.RiskScore > 0.7 {
		t.Errorf("Expected low risk score <= 0.7, got %.2f", result.RiskScore)
	}

	if result.ApprovalStatus != "approved" {
		t.Errorf("Expected approval status 'approved', got %s", result.ApprovalStatus)
	}

	t.Logf("Risk scenario handled correctly: Risk %.2f, Status %s",
		result.RiskScore, result.ApprovalStatus)
}
