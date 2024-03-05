// Package common holds common functionality
package common

import (
	"net"
	"reflect"
	"time"

	pc "github.com/opiproject/opi-api/network/opinetcommon/v1alpha1/gen/go"
)

// ComponentStatus describes the status of each component
type ComponentStatus int

const (
	// ComponentStatusUnspecified for Component unknown state
	ComponentStatusUnspecified ComponentStatus = iota + 1
	// ComponentStatusPending for Component pending state
	ComponentStatusPending
	// ComponentStatusSuccess for Component success state
	ComponentStatusSuccess
	// ComponentStatusError for Component error state
	ComponentStatusError
)

// Component holds component data
type Component struct {
	Name       string
	CompStatus ComponentStatus
	// Free format json string
	Details string
	Timer   time.Duration
}

func ip4ToInt(ip net.IP) uint32 {
	if !reflect.ValueOf(ip).IsZero() {
		return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	}
	return 0
}

// ConvertToIPPrefix converts IPNet type to IPPrefix
func ConvertToIPPrefix(ipNet *net.IPNet) *pc.IPPrefix {
	return &pc.IPPrefix{
		Addr: &pc.IPAddress{
			V4OrV6: &pc.IPAddress_V4Addr{
				V4Addr: ip4ToInt(ipNet.IP.To4()),
			},
		},
		Len: int32(len(ipNet.Mask) * 8),
	}
}
