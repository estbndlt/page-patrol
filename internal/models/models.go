package models

import (
	"strings"
	"time"
)

const (
	RoleMember      = "member"
	RoleCoordinator = "coordinator"
)

const (
	EventReadMarked    = "read_marked"
	EventReadUnmarked  = "read_unmarked"
	EventTargetSet     = "target_published"
	EventMemberAdded   = "member_added"
	EventMemberRemoved = "member_removed"
)

type User struct {
	ID          int64
	Email       string
	DisplayName string
	Role        string
	Active      bool
	CreatedAt   time.Time
}

func (u User) Name() string {
	if strings.TrimSpace(u.DisplayName) != "" {
		return u.DisplayName
	}
	return u.Email
}

type Invite struct {
	ID          int64
	Email       string
	DisplayName string
	Active      bool
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type MagicLinkToken struct {
	ID        int64
	Email     string
	TokenHash string
	ExpiresAt time.Time
	UsedAt    *time.Time
	CreatedAt time.Time
}

type Session struct {
	ID               int64
	UserID           int64
	SessionTokenHash string
	ExpiresAt        time.Time
	CreatedAt        time.Time
}

type ReadingTarget struct {
	ID            int64
	BookTitle     string
	ProgressMode  string
	ProgressStart int
	ProgressEnd   int
	DueDate       time.Time
	Notes         string
	Status        string
	CreatedBy     int64
	CreatedAt     time.Time
}

type CreateTargetInput struct {
	BookTitle     string
	ProgressMode  string
	ProgressStart int
	ProgressEnd   int
	DueDate       time.Time
	Notes         string
}

type ReadStatus struct {
	ID        int64
	TargetID  int64
	UserID    int64
	IsRead    bool
	UpdatedAt time.Time
}

type MemberStatus struct {
	UserID      int64
	Email       string
	DisplayName string
	IsRead      bool
}

type ActivityEvent struct {
	ID          int64
	TargetID    *int64
	ActorUserID int64
	ActorEmail  string
	ActorName   string
	TargetTitle string
	EventType   string
	PayloadJSON string
	CreatedAt   time.Time
}

type EmailJob struct {
	ID             int64
	JobType        string
	RecipientEmail string
	PayloadJSON    string
	Status         string
	AttemptCount   int
	NextAttemptAt  *time.Time
	CreatedAt      time.Time
	SentAt         *time.Time
}

type OutboundEmail struct {
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

type WeeklyTrend struct {
	TargetID       int64
	BookTitle      string
	DueDate        time.Time
	Readers        int
	TotalMembers   int
	CompletionRate float64
}

type MemberStreak struct {
	UserID      int64
	Email       string
	DisplayName string
	Streak      int
}
