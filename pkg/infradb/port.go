// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.

// Package models translates frontend protobuf messages to backend messages
package infradb

import (
	"net"
	"time"

	// pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pb "github.com/mardim91/opi-api/network/evpn-gw/v1alpha1/gen/go"
)

// BridgePortType reflects the different types of a Bridge Port
type BridgePortType int32

const (
	// UNKNOWN bridge port type
	UNKNOWN BridgePortType = iota
	// ACCESS bridge port type
	ACCESS
	// TRUNK bridge port type
	TRUNK
)

// Port object, separate from protobuf for decoupling
type Port struct {
	Name                 string
	Ptype                BridgePortType
	MacAddress           net.HardwareAddr
	LogicalBridgeRefKeys []string
	CreatedAt            time.Time
	UpdatedAt            time.Time
	ResourceVersion      string
}

// build time check that struct implements interface
var _ EvpnObject[*pb.BridgePort] = (*Port)(nil)

// NewBridgePort creates new SVI object from protobuf message
func NewBridgePort(in *pb.BridgePort) *Port {
	mac := net.HardwareAddr(in.Spec.MacAddress)
	return &Port{
		Ptype:                BridgePortType(in.Spec.Ptype),
		MacAddress:           mac,
		LogicalBridgeRefKeys: in.Spec.LogicalBridges,
		CreatedAt:            time.Now(),
	}
}

// ToPb transforms SVI object to protobuf message
func (in *Port) ToPb() *pb.BridgePort {
	port := &pb.BridgePort{
		Spec: &pb.BridgePortSpec{
			Ptype:          pb.BridgePortType(in.Ptype),
			MacAddress:     in.MacAddress,
			LogicalBridges: in.LogicalBridgeRefKeys,
		},
		Status: &pb.BridgePortStatus{
			OperStatus: pb.BPOperStatus_BP_OPER_STATUS_UP,
		},
	}
	// TODO: add VtepIpPrefix
	return port
}

// GetName returns object unique name
func (in *Port) GetName() string {
	return in.Name
}
