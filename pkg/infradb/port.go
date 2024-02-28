// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.

// Package models translates frontend protobuf messages to backend messages
package infradb

import (
//	"fmt"
	"net"
	"log"
	// pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pb "github.com/mardim91/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
)

// BridgePortType reflects the different types of a Bridge Port
type BP_TYPE int32

const (
	// UNSPECIFIED bridge port type
	UNSPECIFIED BP_TYPE = iota
	// ACCESS bridge port type
	ACCESS = iota
	// TRUNK bridge port type
	TRUNK = iota
)

type BP_OPER_STATUS int32

const (
	// unknown
	BP_OPER_STATUS_UNSPECIFIED BP_OPER_STATUS = iota
	// Bridge Port is up
	BP_OPER_STATUS_UP = iota
	// Bridge Port is down
	BP_OPER_STATUS_DOWN = iota
	// Bridge Port is to be deleted
	BP_OPER_STATUS_TO_BE_DELETED = iota
)

type BridgePortStatus struct {
	BPOperStatus BP_OPER_STATUS
	Components   []common.Component
}
type BridgePortSpec struct {
	Name           string
	Ptype          BP_TYPE
	MacAddress     *net.HardwareAddr
	LogicalBridges []string
}

type BridgePortMetadata struct {
	// Dimitris: We assume that this is Vendor specific
	// so it will be generated by the LVM
	VPort string
}

// Bridge Port object, separate from protobuf for decoupling
type BridgePort struct {
	Name             string
	Spec             *BridgePortSpec
	Status           *BridgePortStatus
	Metadata         *BridgePortMetadata
	TransparentTrunk bool
	Vlans            []*uint32
	ResourceVersion  string
}

// build time check that struct implements interface
var _ EvpnObject[*pb.BridgePort] = (*BridgePort)(nil)

// NewBridgePort creates new Bridge Port object from protobuf message
func NewBridgePort(in *pb.BridgePort) *BridgePort {
	var components []common.Component
	var bpType BP_TYPE
	var transTrunk bool

	subscribers := event_bus.EBus.GetSubscribers("bridge-port")
	if subscribers == nil {
		log.Println("NewBridgePort(): No subscribers for Bridge Port objects")
	}

	for _, sub := range subscribers {
		component := common.Component{Name: sub.Name, CompStatus: common.COMP_STATUS_PENDING, Details: ""}
		components = append(components, component)
	}

	if len(in.Spec.LogicalBridges) == 0 {
		transTrunk = true
	}

	switch in.Spec.Ptype {
	case pb.BridgePortType_BRIDGE_PORT_TYPE_ACCESS:
		bpType = ACCESS
	case pb.BridgePortType_BRIDGE_PORT_TYPE_TRUNK:
		bpType = TRUNK
	default:
		bpType = UNSPECIFIED
	}

	return &BridgePort{
		Name: in.Name,
		Spec: &BridgePortSpec{
			Ptype:          bpType,
			MacAddress:     BytetoMac(in.Spec.MacAddress),
			LogicalBridges: in.Spec.LogicalBridges,
		},
		Status: &BridgePortStatus{
			BPOperStatus: BP_OPER_STATUS(BP_OPER_STATUS_DOWN),
			Components:   components,
		},
		Metadata:         &BridgePortMetadata{},
		TransparentTrunk: transTrunk,
		ResourceVersion:  generateVersion(),
	}
}

// ToPb transforms Bridge Port object to protobuf message
func (in *BridgePort) ToPb() *pb.BridgePort {
	bp := &pb.BridgePort{
		Name: in.Name,
		Spec: &pb.BridgePortSpec{
			MacAddress: *in.Spec.MacAddress,
		},
		Status: &pb.BridgePortStatus{},
	}

	switch in.Spec.Ptype {
	case ACCESS:
		bp.Spec.Ptype = pb.BridgePortType_BRIDGE_PORT_TYPE_ACCESS
	case TRUNK:
		bp.Spec.Ptype = pb.BridgePortType_BRIDGE_PORT_TYPE_TRUNK
	default:
		bp.Spec.Ptype = pb.BridgePortType_BRIDGE_PORT_TYPE_UNSPECIFIED
	}

	if !in.TransparentTrunk {
		bp.Spec.LogicalBridges = in.Spec.LogicalBridges
	}

	if in.Status.BPOperStatus == BP_OPER_STATUS_DOWN {
		bp.Status.OperStatus = pb.BPOperStatus_BP_OPER_STATUS_DOWN
	} else if in.Status.BPOperStatus == BP_OPER_STATUS_UP {
		bp.Status.OperStatus = pb.BPOperStatus_BP_OPER_STATUS_UP
	} else if in.Status.BPOperStatus == BP_OPER_STATUS_UNSPECIFIED {
		bp.Status.OperStatus = pb.BPOperStatus_BP_OPER_STATUS_UNSPECIFIED
	}
	for _, comp := range in.Status.Components {
		component := &pb.Component{Name: comp.Name, Details: comp.Details}

		if comp.CompStatus == common.COMP_STATUS_PENDING {
			component.Status = pb.CompStatus_COMP_STATUS_PENDING
		} else if comp.CompStatus == common.COMP_STATUS_SUCCESS {
			component.Status = pb.CompStatus_COMP_STATUS_SUCCESS
		} else if comp.CompStatus == common.COMP_STATUS_ERROR {
			component.Status = pb.CompStatus_COMP_STATUS_ERROR
		} else {
			component.Status = pb.CompStatus_COMP_STATUS_UNSPECIFIED
		}
		bp.Status.Components = append(bp.Status.Components, component)
	}

	return bp
}

// GetName returns object unique name
func (in *BridgePort) GetName() string {
	return in.Name
}
