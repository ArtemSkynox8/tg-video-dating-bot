package models

import "time"

const FakePlatformUserPrefix = "fake_circle_"

type User struct {
	ID             int64
	PlatformUserID string
	PlatformChatID string
	PlatformDialogID string
	ProfileLink    string
	ContactPhone   string
	Username       string
	Name           string
	Gender         string
	PreferredGender string
	FlowState      string
	IsPremium      bool
	ReferrerUserID *int64
	ReferralContactCredits int
	ReferralRewardedAt *time.Time
	Status         string
	RestrictedUntil *time.Time
	PremiumOfferChatID string
	PremiumOfferMessageID string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (u User) IsFake() bool {
	return len(u.PlatformUserID) >= len(FakePlatformUserPrefix) && u.PlatformUserID[:len(FakePlatformUserPrefix)] == FakePlatformUserPrefix
}

type Video struct {
	ID              int64
	UserID          int64
	PlatformMediaID string
	StorageURL      string
	Duration        int
	IsActive        bool
	CreatedAt       time.Time
}

type Candidate struct {
	Video
	Owner User
}

type PremiumPayment struct {
	ExternalID      string
	Status          string
	Plan            string
	Amount          string
	PeriodDays      int
	PaymentMethodID string
}

type PremiumSubscription struct {
	User            User
	Plan            string
	Amount          string
	PeriodDays      int
	PaymentMethodID string
	CurrentPeriodUntil time.Time
	NextChargeAt    time.Time
}

type AdStats struct {
	Tag    string
	Users  int64
	Offer  int64
	Buyers int64
	Sum    float64
}

const (
	StatusActive     = "active"
	StatusBlocked    = "blocked"
	StatusRestricted = "restricted"

	GenderMale   = "male"
	GenderFemale = "female"
	GenderAny    = "any"

	ActionLike    = "like"
	ActionContact = "contact"
	ActionNext    = "next"
	ActionReport  = "report"
)

const (
	StateAwaitingName = "awaiting_name"
	StateAwaitingGender = "awaiting_gender"
	StateAwaitingPreferredGender = "awaiting_preferred_gender"
	StateAwaitingVideo = "awaiting_video"
	StateAwaitingRewriteVideo = "awaiting_rewrite_video"
	StateAwaitingEditName = "awaiting_edit_name"
	StateAwaitingProfileLink = "awaiting_profile_link"
)
