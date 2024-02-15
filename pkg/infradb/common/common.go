package common

import (
	"net"
	"time"
	"reflect"
	pc "github.com/opiproject/opi-api/network/opinetcommon/v1alpha1/gen/go"
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
	// Free format json string
	Details string
	Timer   time.Duration
}

func ip4ToInt(ip net.IP) uint32 {
	if !reflect.ValueOf(ip).IsZero(){
		return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
	}
	return 0
}

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
