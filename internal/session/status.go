package session

import "github.com/Adyen/agent-marketplace/internal/materialize"

type StatusGroup int

const (
	GroupRequired StatusGroup = iota
	GroupEnabled
	GroupPending
	GroupStale
	GroupBroken
	GroupAvailable
	GroupMalformed
)

func StatusGroups() [7]StatusGroup {
	return [7]StatusGroup{
		GroupRequired,
		GroupEnabled,
		GroupPending,
		GroupStale,
		GroupBroken,
		GroupAvailable,
		GroupMalformed,
	}
}

func (g StatusGroup) ID() string {
	switch g {
	case GroupRequired:
		return "required"
	case GroupEnabled:
		return "enabled"
	case GroupPending:
		return "pending"
	case GroupStale:
		return "stale"
	case GroupBroken:
		return "broken"
	case GroupAvailable:
		return "available"
	case GroupMalformed:
		return "malformed"
	default:
		return "unknown"
	}
}

func (g StatusGroup) Title() string {
	switch g {
	case GroupRequired:
		return "Required"
	case GroupEnabled:
		return "Enabled"
	case GroupPending:
		return "Pending sync"
	case GroupStale:
		return "Stale"
	case GroupBroken:
		return "Broken"
	case GroupAvailable:
		return "Available"
	case GroupMalformed:
		return "Malformed"
	default:
		return "?"
	}
}

func (r Row) StatusGroup() StatusGroup {
	if r.IsMalformed {
		return GroupMalformed
	}
	if r.UnavailableReason != "" {
		return GroupBroken
	}
	if r.Artifact.Required {
		return GroupRequired
	}
	if r.IsAvailable {
		return GroupAvailable
	}
	if r.DriftItem != nil {
		switch r.DriftItem.Reason {
		case materialize.DriftMissing, materialize.DriftOrphaned:
			return GroupPending
		case materialize.DriftBroken:
			return GroupBroken
		}
	}
	return GroupEnabled
}
