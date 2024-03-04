// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.

// Package models translates frontend protobuf messages to backend messages
package infradb

import (
	// "encoding/binary"
	"encoding/binary"
	"fmt"
	"log"
	"net"

	// pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pb "github.com/mardim91/opi-api/network/evpn-gw/v1alpha1/gen/go"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
)

type LB_OPER_STATUS int32

const (
	// unknown
	LB_OPER_STATUS_UNSPECIFIED LB_OPER_STATUS = iota
	// Logical Bridge is up
	LB_OPER_STATUS_UP = iota
	// Logical Bridge is down
	LB_OPER_STATUS_DOWN = iota
	// Logical Bridge is to be deleted
	LB_OPER_STATUS_TO_BE_DELETED = iota
)

type LogicalBridgeStatus struct {
	LBOperStatus LB_OPER_STATUS
	Components   []common.Component
}

type LogicalBridgeSpec struct {
	VlanId uint32
	Vni    *uint32
	VtepIP *net.IPNet
}

type LogicalBridgeMetadata struct{}

type LogicalBridge struct {
	Name            string
	Spec            *LogicalBridgeSpec
	Status          *LogicalBridgeStatus
	Metadata        *LogicalBridgeMetadata
	Svi             string
	BridgePorts     map[string]bool
	MacTable        map[string]string
	ResourceVersion string
}

// build time check that struct implements interface
var _ EvpnObject[*pb.LogicalBridge] = (*LogicalBridge)(nil)

// NewLogicalBridge creates new Logica Bridge object from protobuf message
func NewLogicalBridge(in *pb.LogicalBridge) *LogicalBridge {
	var components []common.Component

	// Parse vtep IP
	vtepip := make(net.IP, 4)
	binary.BigEndian.PutUint32(vtepip, in.Spec.VtepIpPrefix.Addr.GetV4Addr())
	vip := net.IPNet{IP: vtepip, Mask: net.CIDRMask(int(in.Spec.VtepIpPrefix.Len), 32)}

	subscribers := event_bus.EBus.GetSubscribers("logical-bridge")
	if subscribers == nil {
		log.Println("NewLogicalBridge(): No subscribers for Logical Bridge objects")
	}

	for _, sub := range subscribers {
		component := common.Component{Name: sub.Name, CompStatus: common.COMP_STATUS_PENDING, Details: ""}
		components = append(components, component)
	}

	return &LogicalBridge{
		Name: in.Name,
		Spec: &LogicalBridgeSpec{
			VlanId: in.Spec.VlanId,
			Vni:    in.Spec.Vni,
			VtepIP: &vip,
		},
		Status: &LogicalBridgeStatus{
			LBOperStatus: LB_OPER_STATUS(LB_OPER_STATUS_DOWN),
			Components:   components,
		},
		Metadata:        &LogicalBridgeMetadata{},
		BridgePorts:     make(map[string]bool),
		MacTable:        make(map[string]string),
		ResourceVersion: generateVersion(),
	}
}

// ToPb transforms Logical Bridge object to protobuf message
func (in *LogicalBridge) ToPb() *pb.LogicalBridge {
	vtepip := common.ConvertToIPPrefix(in.Spec.VtepIP)

	lb := &pb.LogicalBridge{
		Name: in.Name,
		Spec: &pb.LogicalBridgeSpec{
			VlanId:       in.Spec.VlanId,
			Vni:          in.Spec.Vni,
			VtepIpPrefix: vtepip,
		},
		Status: &pb.LogicalBridgeStatus{},
	}
	if in.Status.LBOperStatus == LB_OPER_STATUS_DOWN {
		lb.Status.OperStatus = pb.LBOperStatus_LB_OPER_STATUS_DOWN
	} else if in.Status.LBOperStatus == LB_OPER_STATUS_UP {
		lb.Status.OperStatus = pb.LBOperStatus_LB_OPER_STATUS_UP
	} else if in.Status.LBOperStatus == LB_OPER_STATUS_UNSPECIFIED {
		lb.Status.OperStatus = pb.LBOperStatus_LB_OPER_STATUS_UNSPECIFIED
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

		lb.Status.Components = append(lb.Status.Components, component)
	}

	return lb
}

func (in *LogicalBridge) AddSvi(sviName string) error {
	if in.Svi != "" {
		return fmt.Errorf("AddSvi(): The Logical Bridge is already associated with an SVI interface: %+v\n", in.Svi)
	}

	in.Svi = sviName
	return nil
}

func (in *LogicalBridge) DeleteSvi(sviName string) error {
	if in.Svi != sviName {
		return fmt.Errorf("DeleteSvi(): The Logical Bridge is not associated with the SVI interface: %+v\n", sviName)
	}

	in.Svi = ""
	return nil
}

func (in *LogicalBridge) AddBridgePort(bpName, bpMac string) error {
	_, found := in.BridgePorts[bpName]
	if found {
		return fmt.Errorf("AddBridgePort(): The Logical Bridge %+v is already associated with the Bridge Port: %+v\n", in.Name, bpName)
	}

	_, found = in.MacTable[bpMac]
	if found {
		return fmt.Errorf("AddBridgePort(): The Logical Bridge %+v is already associated with the Bridge Port MAC: %+v\n", in.Name, bpMac)
	}
	in.BridgePorts[bpName] = false
	in.MacTable[bpMac] = bpName

	return nil
}

func (in *LogicalBridge) DeleteBridgePort(bpName, bpMac string) error {
	_, found := in.BridgePorts[bpName]
	if !found {
		return fmt.Errorf("DeleteBridgePort(): The Logical Bridge %+v is not associated with the Bridge Port: %+v\n", in.Name, bpName)
	}

	_, found = in.MacTable[bpMac]
	if !found {
		return fmt.Errorf("DeleteBridgePort(): The Logical Bridge %+v is not associated with the Bridge Port MAC: %+v\n", in.Name, bpMac)
	}

	delete(in.BridgePorts, bpName)
	delete(in.MacTable, bpMac)

	return nil
}

// GetName returns object unique name
func (in *LogicalBridge) GetName() string {
	return in.Name
}
