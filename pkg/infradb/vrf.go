// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.

// Package models translates frontend protobuf messages to backend messages
package infradb

import (
	"encoding/binary"
	"fmt"
	"net"

	// "time"
	// pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pb "github.com/mardim91/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
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

/*
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
*/
type VrfStatus struct {
	VrfOperStatus VRF_OPER_STATUS
	Components    []common.Component
}
type VrfSpec struct {
	// TODO: Need to change the uint32 to *uint32
	Vni uint32
	// TODO: need to change the net.IPNet to *net.IPNet
	LoopbackIP net.IPNet
	VtepIP     net.IPNet
	// LocalAs      uint32
	// RoutingTable uint32
	// MacAddress   net.HardwareAddr
}

type VrfMetadata struct {
	// We add a pointer here because the default value of uint32 type is "0"
	// and that can be considered a legit value. Using *uint32 the default value
	// will be nil
	RoutingTable []*uint32
}

type Vrf struct {
	Name            string
	Spec            VrfSpec
	Status          VrfStatus
	Metadata        *VrfMetadata
	Svis            map[string]bool
	ResourceVersion string
}

// build time check that struct implements interface
var _ EvpnObject[*pb.Vrf] = (*Vrf)(nil)

// NewVrfWithArgs creates a vrf object by passing arguments
func NewVrfWithArgs(name string, vni *uint32, loopbackIP, vtepIP *net.IPNet) (*Vrf, error) {
	var components []common.Component
	vrf := &Vrf{}

	if name == "" {
		err := fmt.Errorf("NewVrfWithArgs(): Vrf name cannot be empty")
		return nil, err
	}

	vrf.Name = name

	if vni != nil {
		vrf.Spec.Vni = *vni
	}

	if loopbackIP != nil {
		vrf.Spec.LoopbackIP = *loopbackIP
	}

	if vtepIP != nil {
		vrf.Spec.VtepIP = *vtepIP
	}

	subscribers := event_bus.EBus.GetSubscribers("vrf")
	if subscribers == nil {
		fmt.Println("NewVrfWithArgs(): No subscribers for Vrf objects")
	}

	for _, sub := range subscribers {
		component := common.Component{Name: sub.Name, CompStatus: common.COMP_STATUS_PENDING, Details: ""}
		components = append(components, component)
	}

	vrf.Status = VrfStatus{
		VrfOperStatus: VRF_OPER_STATUS(VRF_OPER_STATUS_DOWN),
		Components:    components,
	}
	vrf.Metadata = &VrfMetadata{}

	vrf.Svis = make(map[string]bool)

	vrf.ResourceVersion = generateVersion()

	return vrf, nil
}

// NewVrf creates new VRF object from protobuf message
func NewVrf(in *pb.Vrf) *Vrf {
	var components []common.Component

	loopip := make(net.IP, 4)
	binary.BigEndian.PutUint32(loopip, in.Spec.LoopbackIpPrefix.Addr.GetV4Addr())
	lip := net.IPNet{IP: loopip, Mask: net.CIDRMask(int(in.Spec.LoopbackIpPrefix.Len), 32)}
	vtepip := make(net.IP, 4)
	binary.BigEndian.PutUint32(vtepip, in.Spec.VtepIpPrefix.Addr.GetV4Addr())
	vip := net.IPNet{IP: vtepip, Mask: net.CIDRMask(int(in.Spec.VtepIpPrefix.Len), 32)}

	subscribers := event_bus.EBus.GetSubscribers("vrf")
	if subscribers == nil {
		fmt.Println("NewVrf(): No subscribers for Vrf objects")
	}

	for _, sub := range subscribers {
		component := common.Component{Name: sub.Name, CompStatus: common.COMP_STATUS_PENDING, Details: ""}
		components = append(components, component)
	}

	return &Vrf{
		Name: in.Name,
		Spec: VrfSpec{
			Vni:        *in.Spec.Vni,
			LoopbackIP: lip,
			VtepIP:     vip,
		},
		Status: VrfStatus{
			VrfOperStatus: VRF_OPER_STATUS(VRF_OPER_STATUS_DOWN),

			Components: components,
		},
		Metadata:        &VrfMetadata{},
		Svis:            make(map[string]bool),
		ResourceVersion: generateVersion(),
	}
}

// ToPb transforms VRF object to protobuf message
func (in *Vrf) ToPb() *pb.Vrf {
	loopbackIP := common.ConvertToIPPrefix(&in.Spec.LoopbackIP)
	vtepip := common.ConvertToIPPrefix(&in.Spec.VtepIP)
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

		switch comp.CompStatus {
		case common.COMP_STATUS_PENDING:
			component.Status = pb.CompStatus_COMP_STATUS_PENDING
		case common.COMP_STATUS_SUCCESS:
			component.Status = pb.CompStatus_COMP_STATUS_SUCCESS
		case common.COMP_STATUS_ERROR:
			component.Status = pb.CompStatus_COMP_STATUS_ERROR

		default:
			component.Status = pb.CompStatus_COMP_STATUS_UNSPECIFIED
		}
		/*
			if comp.CompStatus == common.COMP_STATUS_PENDING {
				component.Status = pb.CompStatus_COMP_STATUS_PENDING
			} else if comp.CompStatus == common.COMP_STATUS_SUCCESS {
				component.Status = pb.CompStatus_COMP_STATUS_SUCCESS
			} else if comp.CompStatus == common.COMP_STATUS_SUCCESS {
				component.Status = pb.CompStatus_COMP_STATUS_ERROR
			} else {
				component.Status = pb.CompStatus_COMP_STATUS_UNSPECIFIED
			}*/
		vrf.Status.Components = append(vrf.Status.Components, component)
	}
	// TODO: add LocalAs, LoopbackIP, VtepIP
	return vrf
}

func (in *Vrf) AddSvi(sviName string) error {
	_, ok := in.Svis[sviName]
	if ok {
		return fmt.Errorf("AddSvi(): The VRF %+v is already associated with this SVI interface: %+v\n", in.Name, sviName)
	}

	in.Svis[sviName] = false
	return nil
}

func (in *Vrf) DeleteSvi(sviName string) error {
	_, ok := in.Svis[sviName]
	if !ok {
		return fmt.Errorf("DeleteSvi(): The VRF %+v has no SVI interface: %+v\n", in.Name, sviName)
	}
	delete(in.Svis, sviName)
	return nil
}

// GetName returns object unique name
func (in *Vrf) GetName() string {
	return in.Name
}
