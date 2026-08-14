package main

import "time"

const (
	auditSucceeded = "SUCCEEDED"
	auditFailed    = "FAILED"
	auditDuplicate = "DUPLICATE"
	auditRejected  = "REJECTED"

	maxAuditReasonLength = 200
)

// auditEvent records who asked for what, on which machine, and how it ended. It
// deliberately carries no arguments, no raw envelope and no configuration value, so
// the audit list can be read by anyone who operates the queue.
type auditEvent struct {
	CommandID     string    `json:"commandId,omitempty"`
	CorrelationID string    `json:"correlationId,omitempty"`
	Target        string    `json:"target"`
	Action        string    `json:"action,omitempty"`
	RequestedBy   string    `json:"requestedBy,omitempty"`
	Status        string    `json:"status"`
	Reason        string    `json:"reason,omitempty"`
	RequestedAt   time.Time `json:"requestedAt,omitempty"`
	ReceivedAt    time.Time `json:"receivedAt"`
	CompletedAt   time.Time `json:"completedAt,omitempty"`
}

func commandAudit(c command, status, reason string, receivedAt, completedAt time.Time) auditEvent {
	return auditEvent{
		CommandID:     c.CommandID,
		CorrelationID: c.CorrelationID,
		Target:        c.Target,
		Action:        c.Action,
		RequestedBy:   c.RequestedBy,
		Status:        status,
		Reason:        boundedReason(reason),
		RequestedAt:   c.RequestedAt,
		ReceivedAt:    receivedAt,
		CompletedAt:   completedAt,
	}
}

// rejectionAudit describes a message that never became a command. Nothing from the
// rejected payload is copied into it; the dead-letter list keeps the raw envelope.
func rejectionAudit(target, reason string, receivedAt time.Time) auditEvent {
	return auditEvent{
		Target:     target,
		Status:     auditRejected,
		Reason:     boundedReason(reason),
		ReceivedAt: receivedAt,
	}
}

func boundedReason(reason string) string {
	if len(reason) <= maxAuditReasonLength {
		return reason
	}
	return reason[:maxAuditReasonLength]
}
