package common

import (
	"time"
)
type COMP_STATUS int

const (
	COMP_STATUS_UNSPECIFIED COMP_STATUS = iota + 1
	COMP_STATUS_PENDING
	COMP_STATUS_SUCCESS
	COMP_STATUS_ERROR
)

// Vrf object, separate from protobuf for decoupling

type Component struct {
	Name       string
	CompStatus COMP_STATUS
	//Free format json string
	Details string
	Timer   time.Duration
}

