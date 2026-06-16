package models

import "time"

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

const (
	StatusActive     = "active"
	StatusBlocked    = "blocked"
	StatusRestricted = "restricted"

	GenderMale   = "male"
	GenderFemale = "female"
	GenderAny    = "any"

	ActionLike   = "like"
	ActionNext   = "next"
	ActionReport = "report"
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
