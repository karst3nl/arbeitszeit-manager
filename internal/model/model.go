// SPDX-License-Identifier: AGPL-3.0-only

// Package model contains the data structures used by store and API.
package model

// User is a person that can log in.
type User struct {
	ID           int64
	Username     string
	DisplayName  string
	PasswordHash string
	Role         string
	Active       bool
	CreatedAt    int64
	LastLoginAt  int64
}

// IsAdmin reports whether the user has administrator privileges.
func (u *User) IsAdmin() bool {
	return u != nil && u.Role == "admin"
}

// Order is a work order with planned and actual time totals.
type Order struct {
	ID             int64
	Title          string
	Description    string
	Status         string
	OwnerID        int64
	OwnerName      string
	AssigneeID     int64
	AssigneeName   string
	TimerState     string
	TimerStartedAt int64
	CreatedAt      int64
	CompletedAt    int64
	Notes          string
	// PlannedMinutes is the Soll used for the evaluation: the sum of the
	// planned minutes of all tasks in this order.
	PlannedMinutes int64
	ActualSeconds  int64
	Interrupted    bool
}

// Done reports whether the order has been completed.
func (o *Order) Done() bool { return o != nil && o.Status == "done" }

// DeltaSeconds is the difference between actual and planned time.
func (o *Order) DeltaSeconds() int64 {
	if o == nil {
		return 0
	}
	return o.ActualSeconds - o.PlannedMinutes*60
}

// DeltaPercent is DeltaSeconds relative to the planned time.
func (o *Order) DeltaPercent() float64 {
	if o == nil || o.PlannedMinutes == 0 {
		return 0
	}
	return float64(o.DeltaSeconds()) / float64(o.PlannedMinutes*60) * 100
}

// Task is one planned activity inside an order.
type Task struct {
	ID             int64
	OrderID        int64
	Title          string
	PlannedMinutes int64
	Position       int
	Done           bool
}

// TaskTemplate is a reusable task maintained by an admin. Selecting one when
// creating an order adds a copy to that order.
type TaskTemplate struct {
	ID             int64
	Title          string
	PlannedMinutes int64
	CreatedAt      int64
}

// Report is the result of an order evaluation.
type Report struct {
	OrderID          int64
	Title            string
	PlannedMinutes   int64
	ActualSeconds    int64
	DeviationSeconds int64
	DeviationPercent float64
	Summary          string
	CreatedAt        int64
	Notes            string
	Tasks            []Task
}
