// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.

// Package models translates frontend protobuf messages to backend messages
package infradb

import (
	"encoding/binary"
	"net"
	//"time"
	//pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pb "github.com/mardim91/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pc "github.com/opiproject/opi-api/network/opinetcommon/v1alpha1/gen/go"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
)

type VRF_OPER_STATUS int32

const (
	// unknown
	VRF_OPER_STATUS_UNSPECIFIED VRF_OPER_STATUS = iota
	// vrf is up
	VRF_OPER_STATUS_UP = iota
	// vrf is down
	VRF_OPER_STATUS_DOWN = iota
	// vrf is to be deleted
	VRF_OPER_STATUS_TO_BE_DELETED = iota
)

/*type COMP_STATUS int

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
}*/
type VrfStatus struct {
	VrfOperStatus VRF_OPER_STATUS
	Components    []common.Component
}
type VrfSpec struct {
	Name         string
	Vni          uint32
	LoopbackIP   net.IPNet
	VtepIP       net.IPNet
	//LocalAs      uint32
	//RoutingTable uint32
	//MacAddress   net.HardwareAddr
}

type Vrf struct {
	Name            string
	Spec            VrfSpec
	Status          VrfStatus
	ResourceVersion string
}

// build time check that struct implements interface
var _ EvpnObject[*pb.Vrf] = (*Vrf)(nil)

// NewVrf creates new VRF object from protobuf message
func NewVrf(in *pb.Vrf) *Vrf {
	//mac := net.HardwareAddr(in.Status.Rmac)
	loopip := make(net.IP, 4)
	binary.BigEndian.PutUint32(loopip, in.Spec.LoopbackIpPrefix.Addr.GetV4Addr())
	lip := net.IPNet{IP: loopip, Mask: net.CIDRMask(int(in.Spec.LoopbackIpPrefix.Len), 32)}
	vtepip := make(net.IP, 4)
	binary.BigEndian.PutUint32(vtepip, in.Spec.VtepIpPrefix.Addr.GetV4Addr())
	vip := net.IPNet{IP: vtepip, Mask: net.CIDRMask(int(in.Spec.VtepIpPrefix.Len), 32)}
	//return &Vrf{Spec.oopbackIP: lip, Spec.MacAddress: mac, Spec.RoutingTable: in.Status.RoutingTable,ResourceVersion:generateVersion()}
	return &Vrf{
		Name: in.Name,
		Spec: VrfSpec{
			Name:         in.Name,
			Vni:          *in.Spec.Vni,
			LoopbackIP:   lip,
			VtepIP:       vip,
			//LocalAs:      in.Status.LocalAs,
			//RoutingTable: in.Status.RoutingTable,
			//MacAddress:   mac,
		},
		Status: VrfStatus{
			VrfOperStatus: VRF_OPER_STATUS(VRF_OPER_STATUS_DOWN),
			/*Components: []Component{
					{Name: "FRR", CompStatus: COMP_STATUS_PENDING, details: ""},
					{Name: "Linux", CompStatus: COMP_STATUS_PENDING, details: ""},
			},*/
		},
		ResourceVersion: generateVersion(),
	}
}

// ToPb transforms VRF object to protobuf message
func (in *Vrf) ToPb() *pb.Vrf {
	loopbackIP := ConvertToIPPrefix(&in.Spec.LoopbackIP)
	vtepip := ConvertToIPPrefix(&in.Spec.VtepIP)
	vrf := &pb.Vrf{
		Name: in.Name,
		Spec: &pb.VrfSpec{
			Vni:              &in.Spec.Vni,
			LoopbackIpPrefix: loopbackIP,

			VtepIpPrefix: vtepip,
		},
		Status: &pb.VrfStatus{
			//LocalAs: in.Spec.LocalAs,
		},
	}
	if in.Status.VrfOperStatus == VRF_OPER_STATUS_DOWN {
		vrf.Status.OperStatus = pb.VRFOperStatus_VRF_OPER_STATUS_DOWN
	} else if in.Status.VrfOperStatus == VRF_OPER_STATUS_UP {
		vrf.Status.OperStatus = pb.VRFOperStatus_VRF_OPER_STATUS_UP
	} else if in.Status.VrfOperStatus == VRF_OPER_STATUS_UNSPECIFIED {
		vrf.Status.OperStatus = pb.VRFOperStatus_VRF_OPER_STATUS_UNSPECIFIED
	}
	for _, comp := range in.Status.Components {
		component := &pb.Component{Name: comp.Name, Details: comp.Details}

		if comp.CompStatus == common.COMP_STATUS_PENDING {
			component.Status = pb.CompStatus_COMP_STATUS_PENDING
		} else if comp.CompStatus == common.COMP_STATUS_SUCCESS {
			component.Status = pb.CompStatus_COMP_STATUS_SUCCESS
		} else if comp.CompStatus == common.COMP_STATUS_SUCCESS {
			component.Status = pb.CompStatus_COMP_STATUS_ERROR
		} else {
			component.Status = pb.CompStatus_COMP_STATUS_UNSPECIFIED
		}
		vrf.Status.Components = append(vrf.Status.Components, component)
	}
	// TODO: add LocalAs, LoopbackIP, VtepIP
	return vrf
}

// GetName returns object unique name
func (in *Vrf) GetName() string {
	return in.Name
}
func ip4ToInt(ip net.IP) uint32 {
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
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
