package models

import "time"

const (
	OrderStatusCreated = "created"
	OrderStatusPending = "pending"
	OrderStatusPaid    = "paid"
	OrderStatusSuccess = "success"
	OrderStatusError   = "error"
	OrderStatusManual  = "manual"
)

type User struct {
	ID             int64
	PlatformUserID string
	PlatformChatID string
	Username       string
	Name           string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Order struct {
	ID               int64
	UserID           int64
	PlatformUserID   string
	PlatformChatID   string
	NominalCode      string
	ProductLabel     string
	KinguinProductID string
	SourcePrice      float64
	SourceCurrency   string
	OrderSum         float64
	Status           string
	PaymentProvider  string
	PaymentID        string
	PaymentURL       string
	KinguinOrderID   string
	GiftCode         string
	ErrorText        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type ProductQuote struct {
	ProductID string
	Name      string
	Price     float64
	Currency  string
	Qty       int
}

type WaitlistEntry struct {
	ID             int64
	UserID         int64
	PlatformUserID string
	PlatformChatID string
	NominalCode    string
	ProductLabel    string
}
