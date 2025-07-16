package test

import (
	"fmt"
	"time"

	broker "github.com/joshua-temple/nexus"
)

// This example demonstrates parallel dispatch where a handler sends multiple
// commands simultaneously and aggregates their results. It showcases:
// - Parallel dispatch to multiple services
// - Mixed required/optional responses
// - Timeout handling for specific operations
// - Fire-and-forget events

// Data structures
type TravelBookingRequest struct {
	BookingID   string    `json:"booking_id"`
	CustomerID  string    `json:"customer_id"`
	Destination string    `json:"destination"`
	CheckIn     time.Time `json:"check_in"`
	CheckOut    time.Time `json:"check_out"`
	Travelers   int       `json:"travelers"`
	Budget      float64   `json:"budget"`
}

type TravelBookingResult struct {
	BookingID           string           `json:"booking_id"`
	Status              string           `json:"status"`
	FlightDetails       *FlightResult    `json:"flight_details,omitempty"`
	HotelDetails        *HotelResult     `json:"hotel_details,omitempty"`
	CarRentalDetails    *CarRentalResult `json:"car_rental_details,omitempty"`
	InsuranceDetails    *InsuranceResult `json:"insurance_details,omitempty"`
	ActivitySuggestions []string         `json:"activity_suggestions,omitempty"`
	TotalCost           float64          `json:"total_cost"`
	Errors              []string         `json:"errors,omitempty"`
}

type FlightSearchRequest struct {
	Origin      string    `json:"origin"`
	Destination string    `json:"destination"`
	DepartDate  time.Time `json:"depart_date"`
	ReturnDate  time.Time `json:"return_date"`
	Passengers  int       `json:"passengers"`
}

type FlightResult struct {
	FlightNumber string  `json:"flight_number"`
	Airline      string  `json:"airline"`
	Price        float64 `json:"price"`
	Duration     string  `json:"duration"`
}

type HotelSearchRequest struct {
	Location string    `json:"location"`
	CheckIn  time.Time `json:"check_in"`
	CheckOut time.Time `json:"check_out"`
	Guests   int       `json:"guests"`
}

type HotelResult struct {
	HotelName     string  `json:"hotel_name"`
	RoomType      string  `json:"room_type"`
	PricePerNight float64 `json:"price_per_night"`
	TotalPrice    float64 `json:"total_price"`
}

type CarRentalRequest struct {
	Location   string    `json:"location"`
	PickupDate time.Time `json:"pickup_date"`
	ReturnDate time.Time `json:"return_date"`
}

type CarRentalResult struct {
	CompanyName string  `json:"company_name"`
	CarType     string  `json:"car_type"`
	DailyRate   float64 `json:"daily_rate"`
	TotalPrice  float64 `json:"total_price"`
}

type InsuranceQuoteRequest struct {
	BookingID string  `json:"booking_id"`
	TripValue float64 `json:"trip_value"`
	Travelers int     `json:"travelers"`
}

type InsuranceResult struct {
	Provider string  `json:"provider"`
	Coverage string  `json:"coverage"`
	Premium  float64 `json:"premium"`
}

type ActivitySearchRequest struct {
	Destination string   `json:"destination"`
	Interests   []string `json:"interests"`
}

type ActivityResult struct {
	Activities []string `json:"activities"`
}

type BookingConfirmation struct {
	BookingID string    `json:"booking_id"`
	Timestamp time.Time `json:"timestamp"`
	Message   string    `json:"message"`
}

// Topics
var (
	TopicTravelBooking  = mustTopic("travel", "booking", 1).WithPurpose("create").WithTopicType(broker.TopicTypeCommand)
	TopicFlightSearch   = mustTopic("travel", "flights", 1).WithPurpose("search").WithTopicType(broker.TopicTypeCommand)
	TopicHotelSearch    = mustTopic("travel", "hotels", 1).WithPurpose("search").WithTopicType(broker.TopicTypeCommand)
	TopicCarRental      = mustTopic("travel", "cars", 1).WithPurpose("search").WithTopicType(broker.TopicTypeCommand)
	TopicInsuranceQuote = mustTopic("travel", "insurance", 1).WithPurpose("quote").WithTopicType(broker.TopicTypeCommand)
	TopicActivitySearch = mustTopic("travel", "activities", 1).WithPurpose("search").WithTopicType(broker.TopicTypeCommand)
	TopicBookingEvent   = mustTopic("travel", "events", 1).WithPurpose("booking").WithTopicType(broker.TopicTypeEvent)
)

// Main travel booking handler demonstrating parallel dispatch
type TravelBookingHandler struct{}

func (h *TravelBookingHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicTravelBooking}
}

func (h *TravelBookingHandler) Handle(ctx *broker.Context) error {
	var req TravelBookingRequest
	if err := ctx.Unmarshal(&req); err != nil {
		return err
	}

	// Fire-and-forget event to notify booking started
	ctx.Dispatch(*TopicBookingEvent, BookingConfirmation{
		BookingID: req.BookingID,
		Timestamp: time.Now(),
		Message:   "Booking process started",
	}, broker.AsEvent())

	// Parallel dispatch to multiple services

	// REQUIRED: Flight search (with custom timeout)
	flightSearch := broker.DispatchTyped[FlightResult](
		ctx,
		*TopicFlightSearch,
		FlightSearchRequest{
			Origin:      "NYC", // Would normally be derived from customer profile
			Destination: req.Destination,
			DepartDate:  req.CheckIn,
			ReturnDate:  req.CheckOut,
			Passengers:  req.Travelers,
		},
		broker.WithRequired(),
		broker.WithTimeout(10*time.Second), // Tight timeout for flight search
	)

	// REQUIRED: Hotel search
	hotelSearch := broker.DispatchTyped[HotelResult](
		ctx,
		*TopicHotelSearch,
		HotelSearchRequest{
			Location: req.Destination,
			CheckIn:  req.CheckIn,
			CheckOut: req.CheckOut,
			Guests:   req.Travelers,
		},
		broker.WithRequired(),
	)

	// OPTIONAL: Car rental (not everyone needs a car)
	carRental := broker.DispatchTyped[CarRentalResult](
		ctx,
		*TopicCarRental,
		CarRentalRequest{
			Location:   req.Destination,
			PickupDate: req.CheckIn,
			ReturnDate: req.CheckOut,
		},
		// No WithRequired() - this is optional
	)

	// OPTIONAL: Travel insurance quote
	// Calculate estimated trip value for insurance
	estimatedValue := req.Budget * 0.8 // Conservative estimate
	insurance := broker.DispatchTyped[InsuranceResult](
		ctx,
		*TopicInsuranceQuote,
		InsuranceQuoteRequest{
			BookingID: req.BookingID,
			TripValue: estimatedValue,
			Travelers: req.Travelers,
		},
		broker.WithTimeout(5*time.Second), // Quick timeout for insurance
	)

	// OPTIONAL: Activity suggestions (nice to have, don't wait long)
	activities := broker.DispatchTyped[ActivityResult](
		ctx,
		*TopicActivitySearch,
		ActivitySearchRequest{
			Destination: req.Destination,
			Interests:   []string{"sightseeing", "dining", "adventure"},
		},
		broker.WithTimeout(3*time.Second), // Very short timeout
	)

	// Wait for all REQUIRED responses (flight and hotel)
	if err := ctx.Wait(); err != nil {
		// Critical failure - can't book without flight and hotel
		return ctx.Reply(TravelBookingResult{
			BookingID: req.BookingID,
			Status:    "failed",
			Errors:    []string{err.Error()},
		})
	}

	// Get required results
	flight, err := flightSearch.Value()
	if err != nil {
		return fmt.Errorf("flight search failed: %w", err)
	}

	hotel, err := hotelSearch.Value()
	if err != nil {
		return fmt.Errorf("hotel search failed: %w", err)
	}

	// Initialize result
	result := TravelBookingResult{
		BookingID:     req.BookingID,
		Status:        "confirmed",
		FlightDetails: &flight,
		HotelDetails:  &hotel,
		TotalCost:     flight.Price + hotel.TotalPrice,
		Errors:        []string{},
	}

	// Get optional results (don't fail if these timeout or error)
	if car, err := carRental.Value(); err == nil {
		result.CarRentalDetails = &car
		result.TotalCost += car.TotalPrice
	} else {
		result.Errors = append(result.Errors, fmt.Sprintf("Car rental unavailable: %v", err))
	}

	if ins, err := insurance.Value(); err == nil {
		result.InsuranceDetails = &ins
		result.TotalCost += ins.Premium
	}

	if act, err := activities.Value(); err == nil {
		result.ActivitySuggestions = act.Activities
	}

	// Check budget
	if result.TotalCost > req.Budget {
		result.Status = "over-budget"
		result.Errors = append(result.Errors,
			fmt.Sprintf("Total cost $%.2f exceeds budget $%.2f", result.TotalCost, req.Budget))
	}

	// Fire-and-forget completion event
	ctx.Dispatch(*TopicBookingEvent, BookingConfirmation{
		BookingID: req.BookingID,
		Timestamp: time.Now(),
		Message:   fmt.Sprintf("Booking completed with status: %s", result.Status),
	}, broker.AsEvent())

	return ctx.Reply(result)
}

// Example service handlers (simplified)

type FlightSearchHandler struct{}

func (h *FlightSearchHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicFlightSearch}
}

func (h *FlightSearchHandler) Handle(ctx *broker.Context) error {
	var req FlightSearchRequest
	ctx.Unmarshal(&req)

	// Simulate flight search
	return ctx.Reply(FlightResult{
		FlightNumber: "AA123",
		Airline:      "American Airlines",
		Price:        450.00,
		Duration:     "5h 30m",
	})
}

type HotelSearchHandler struct{}

func (h *HotelSearchHandler) Topics() []broker.Topic {
	return []broker.Topic{*TopicHotelSearch}
}

func (h *HotelSearchHandler) Handle(ctx *broker.Context) error {
	var req HotelSearchRequest
	ctx.Unmarshal(&req)

	nights := int(req.CheckOut.Sub(req.CheckIn).Hours() / 24)

	return ctx.Reply(HotelResult{
		HotelName:     "Grand Plaza Hotel",
		RoomType:      "Deluxe Suite",
		PricePerNight: 200.00,
		TotalPrice:    float64(nights) * 200.00,
	})
}

// Example_parallelDispatch demonstrates multiple simultaneous operations
func Example_parallelDispatch() {
	fmt.Println("Parallel Dispatch Example: Multiple simultaneous operations")
	fmt.Println("- Required: Flight and Hotel bookings")
	fmt.Println("- Optional: Car rental, Insurance, Activities")
	fmt.Println("- Events: Booking notifications (fire-and-forget)")
	fmt.Println("- Timeouts: Different timeouts for different services")

	// Output:
	// Parallel Dispatch Example: Multiple simultaneous operations
	// - Required: Flight and Hotel bookings
	// - Optional: Car rental, Insurance, Activities
	// - Events: Booking notifications (fire-and-forget)
	// - Timeouts: Different timeouts for different services
}
