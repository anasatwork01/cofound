// Package apierrs holds the api service's domain error codes.
//
// The chassis owns transport shapes; a service owns its domain shapes. None of
// these are speculative: api.openapi.yaml inlines two distinct 409s with
// different prose, and SPEC 7.2's own example error event uses
// budget_exceeded.
package apierrs

import "github.com/anasatwork01/cofound/packages/chassis/errs"

// Catalog extends the chassis transport vocabulary.
//
// Built once as a value rather than registered into a package-level map at
// init: a mutable global would make two test binaries in one `go test ./...`
// run order-dependent.
var Catalog = errs.MustCatalog(append(errs.ChassisEntries(),
	errs.Entry{
		Code: "turn_running", Status: 409,
		Message: "The agent is mid-turn in this session.",
		Fix:     "Wait for it to finish, or stop the turn and send this again.",
	},
	errs.Entry{
		Code: "no_turn_running", Status: 409,
		Message: "No turn is running in this session.",
		Fix:     "Send a message to start one.",
	},
	errs.Entry{
		Code: "lease_held", Status: 409,
		Message: "Someone is editing this project's files.",
		Fix:     "Ask them to release the write lease, or wait for the turn to end.",
	},
	errs.Entry{
		Code: "budget_exceeded", Status: 402,
		Message: "This project has used its budget for the period.",
		Fix:     "Raise the budget in project settings, or wait for it to reset.",
	},
)...)

// TurnRunning reports a session that already has a turn in flight.
func TurnRunning() *errs.Error { return Catalog.New("turn_running") }

// NoTurnRunning reports an abort with nothing to abort.
func NoTurnRunning() *errs.Error { return Catalog.New("no_turn_running") }

// LeaseHeld reports the write lease being held by someone else.
func LeaseHeld() *errs.Error { return Catalog.New("lease_held") }

// BudgetExceeded reports an exhausted project budget.
func BudgetExceeded() *errs.Error { return Catalog.New("budget_exceeded") }
