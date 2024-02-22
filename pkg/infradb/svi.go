// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Dell Inc, or its subsidiaries.

// Package models translates frontend protobuf messages to backend messages
package infradb

import (
	"encoding/binary"
	"fmt"
	"net"

	// pb "github.com/opiproject/opi-api/network/evpn-gw/v1alpha1/gen/go"
	pb "github.com/mardim91/opi-api/network/evpn-gw/v1alpha1/gen/go"
	opinetcommon "github.com/opiproject/opi-api/network/opinetcommon/v1alpha1/gen/go"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
)

type SVI_OPER_STATUS int32

const (
	// unknown
	SVI_OPER_STATUS_UNSPECIFIED SVI_OPER_STATUS = iota
	// svi is up
	SVI_OPER_STATUS_UP = iota
	// svi is down
	SVI_OPER_STATUS_DOWN = iota
	// svi is to be deleted
	SVI_OPER_STATUS_TO_BE_DELETED = iota
)

type SviStatus struct {
	SviOperStatus SVI_OPER_STATUS
	Components    []common.Component
}
type SviSpec struct {
	Vrf           string
	LogicalBridge string
	MacAddress    *net.HardwareAddr
	// TODO: This should be plural in Protobuf as well
	GatewayIPs []*net.IPNet
	EnableBgp  bool
	RemoteAs   *uint32
}

type SviMetadata struct {
}

// Svi object, separate from protobuf for decoupling
type Svi struct {
	Name            string
	Spec            *SviSpec
	Status          *SviStatus
	Metadata        *SviMetadata
	ResourceVersion string
}

// build time check that struct implements interface
var _ EvpnObject[*pb.Svi] = (*Svi)(nil)

// NewSvi creates new SVI object from protobuf message
func NewSvi(in *pb.Svi) *Svi {
	var components []common.Component
	var gwIPs []*net.IPNet

	// Parse Gateway IPs
	for _, gwIpPrefix := range in.Spec.GwIpPrefix {
		gatewayIP := make(net.IP, 4)
		binary.BigEndian.PutUint32(gatewayIP, gwIpPrefix.Addr.GetV4Addr())
		gwIP := net.IPNet{IP: gatewayIP, Mask: net.CIDRMask(int(gwIpPrefix.Len), 32)}
		gwIPs = append(gwIPs, &gwIP)
	}

	subscribers := event_bus.EBus.GetSubscribers("svi")
	if subscribers == nil {
		fmt.Println("NewSvi(): No subscribers for SVI objects")
	}

	for _, sub := range subscribers {
		component := common.Component{Name: sub.Name, CompStatus: common.COMP_STATUS_PENDING, Details: ""}
		components = append(components, component)
	}

	return &Svi{
		Name: in.Name,
		Spec: &SviSpec{
			Vrf:           in.Spec.Vrf,
			LogicalBridge: in.Spec.LogicalBridge,
			MacAddress:    (*net.HardwareAddr)(&in.Spec.MacAddress),
			GatewayIPs:    gwIPs,
			EnableBgp:     in.Spec.EnableBgp,
			RemoteAs:      &in.Spec.RemoteAs,
		},
		Status: &SviStatus{
			SviOperStatus: SVI_OPER_STATUS(SVI_OPER_STATUS_DOWN),
			Components:    components,
		},
		Metadata:        &SviMetadata{},
		ResourceVersion: generateVersion(),
	}
}

// ToPb transforms Svi object to protobuf message
func (in *Svi) ToPb() *pb.Svi {
	var gatewayIPs []*opinetcommon.IPPrefix

	for _, gwIP := range in.Spec.GatewayIPs {
		gatewayIP := common.ConvertToIPPrefix(gwIP)
		gatewayIPs = append(gatewayIPs, gatewayIP)
	}

	svi := &pb.Svi{
		Name: in.Name,
		Spec: &pb.SviSpec{
			Vrf:           in.Spec.Vrf,
			LogicalBridge: in.Spec.LogicalBridge,
			MacAddress:    *in.Spec.MacAddress,
			GwIpPrefix:    gatewayIPs,
			EnableBgp:     in.Spec.EnableBgp,
			RemoteAs:      *in.Spec.RemoteAs,
		},
		Status: &pb.SviStatus{},
	}
	if in.Status.SviOperStatus == SVI_OPER_STATUS_DOWN {
		svi.Status.OperStatus = pb.SVIOperStatus_SVI_OPER_STATUS_DOWN
	} else if in.Status.SviOperStatus == SVI_OPER_STATUS_UP {
		svi.Status.OperStatus = pb.SVIOperStatus_SVI_OPER_STATUS_UP
	} else if in.Status.SviOperStatus == SVI_OPER_STATUS_UNSPECIFIED {
		svi.Status.OperStatus = pb.SVIOperStatus_SVI_OPER_STATUS_UNSPECIFIED
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
		svi.Status.Components = append(svi.Status.Components, component)
	}

	return svi
}

// GetName returns object unique name
func (in *Svi) GetName() string {
	return in.Name
}
