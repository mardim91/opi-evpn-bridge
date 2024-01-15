// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.

// Package models translates frontend protobuf messages to backend messages
package infradb

import (
	// "encoding/binary"
	"net"
	"time"

	//pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pb "github.com/mardim91/opi-api/network/evpn-gw/v1alpha1/gen/go"
)

// Bridge object, separate from protobuf for decoupling
type Bridge struct {
	Name      string
	Vni       uint32
	VlanID    uint32
	VtepIP    net.IPNet
	Ports     map[string]struct{}
	MacTable  map[string]Port
	CreatedAt time.Time
	UpdatedAt time.Time
	ResourceVersion	string
}

// build time check that struct implements interface
var _ EvpnObject[*pb.LogicalBridge] = (*Bridge)(nil)

// NewBridge creates new SVI object from protobuf message
func NewBridge(in *pb.LogicalBridge) *Bridge {
	// vtepip := make(net.IP, 4)
	// binary.BigEndian.PutUint32(vtepip, in.Spec.VtepIpPrefix.Addr.GetV4Addr())
	// vip := net.IPNet{IP: vtepip, Mask: net.CIDRMask(int(in.Spec.VtepIpPrefix.Len), 32)}
	// TODO: Vni: *in.Spec.Vni
	if in.Spec.VlanId < 1 || in.Spec.VlanId > 4095 {
		return nil
	}

	return &Bridge{Name:in.Name, VlanID: in.Spec.VlanId, Ports:make(map[string]struct{}),MacTable: make(map[string]Port), CreatedAt: time.Now(), ResourceVersion:generateVersion()}
}

// ToPb transforms SVI object to protobuf message
func (in *Bridge) ToPb() *pb.LogicalBridge {
	bridge := &pb.LogicalBridge{
		Spec: &pb.LogicalBridgeSpec{
			Vni:    &in.Vni,
			VlanId: in.VlanID,
		},
		Status: &pb.LogicalBridgeStatus{
			OperStatus: pb.LBOperStatus_LB_OPER_STATUS_UP,
		},
	}

	return bridge
}

// GetName returns object unique name
func (in *Bridge) GetName() string {
	return in.Name
}
