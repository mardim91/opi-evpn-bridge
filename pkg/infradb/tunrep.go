// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2024 Intel Corporation, or its subsidiaries.
// Copyright (C) 2024 Ericsson AB.

package infradb

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"

	"github.com/opiproject/opi-evpn-bridge/pkg/config"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriberframework/eventbus"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils"
)

var (
	// ErrTunRepNotBoundToSa for tunRep not bound to SA
	ErrTunRepNotBoundToSa = errors.New("tunnel representor not bound to sa")
)

// TunRepOperStatus operational Status for TunReps
type TunRepOperStatus int32

const (
	// TunRepOperStatusUnspecified for TunRep unknown state
	TunRepOperStatusUnspecified TunRepOperStatus = iota
	// TunRepOperStatusUp for TunRep up state
	TunRepOperStatusUp = iota
	// TunRepOperStatusDown for TunRep down state
	TunRepOperStatusDown = iota
	// TunRepOperStatusToBeDeleted for TunRep to be deleted state
	TunRepOperStatusToBeDeleted = iota
)

// TunRepStatus holds TunRep Status
type TunRepStatus struct {
	TunRepOperStatus TunRepOperStatus
	Components       []common.Component
}

// TunRepSpec holds TunRep Spec
type TunRepSpec struct {
	IfName   string
	IfID     uint32
	Vrf      string
	IP       *net.IP
	IPNet    *net.IPNet
	RemoteIP *net.IP
	Sa       string
	SaIdx    *uint32
	Spi      *uint32
	SrcIP    *net.IP
	DstIP    *net.IP
	SrcMac   string
	DestMac  string
}

// TunRepMetadata holds TunRep Metadata
type TunRepMetadata struct{}

// TunRep holds TunRep info
type TunRep struct {
	Name            string
	Spec            *TunRepSpec
	Status          *TunRepStatus
	Metadata        *TunRepMetadata
	OldVersions     []string
	ResourceVersion string
}

// NewTunRep creates a new TunRep object
func NewTunRep(tunCfg config.TunnelConfig) (*TunRep, error) {
	components := make([]common.Component, 0)

	name := createTunRepName(tunCfg.IfID)

	ip, ipnet, err := net.ParseCIDR(tunCfg.IP)
	if err != nil {
		return nil, err
	}

	remoteIP := net.ParseIP(tunCfg.RemoteIP)
	if remoteIP == nil {
		err = fmt.Errorf("NewTunRep(): Malformed tunnel remote IP: %+v", tunCfg.RemoteIP)
		return nil, err
	}

	subscribers := eventbus.EBus.GetSubscribers("tun-rep")
	if len(subscribers) == 0 {
		log.Println("NewTunRep(): No subscribers for tunnel representors objects")
		return nil, errors.New("no subscribers found for tunnel representors")
	}

	for _, sub := range subscribers {
		component := common.Component{Name: sub.Name, CompStatus: common.ComponentStatusPending, Details: ""}
		components = append(components, component)
	}

	return &TunRep{
		Name: name,
		Spec: &TunRepSpec{
			IfName:   tunCfg.Name,
			IfID:     tunCfg.IfID,
			Vrf:      "//network.opiproject.org/vrfs/GRD",
			IP:       &ip,
			IPNet:    ipnet,
			RemoteIP: &remoteIP,
		},
		Status: &TunRepStatus{
			TunRepOperStatus: TunRepOperStatus(TunRepOperStatusDown),

			Components: components,
		},
		Metadata:        &TunRepMetadata{},
		ResourceVersion: generateVersion(),
	}, nil
}

func createTunRepName(ifID uint32) string {
	return "tunRep" + "-" + strconv.Itoa(int(ifID))
}

// setComponentState set the stat of the component
func (in *TunRep) setComponentState(component common.Component) {
	tunRepComponents := in.Status.Components
	for i, comp := range tunRepComponents {
		if comp.Name == component.Name {
			in.Status.Components[i] = component
			break
		}
	}
}

// checkForAllSuccess check if all the components are in Success state
func (in *TunRep) checkForAllSuccess() bool {
	for _, comp := range in.Status.Components {
		if comp.CompStatus != common.ComponentStatusSuccess {
			return false
		}
	}
	return true
}

// parseMeta parse metadata
func (in *TunRep) parseMeta(tunRepMeta *TunRepMetadata) {
	if tunRepMeta != nil {
		in.Metadata = tunRepMeta
	}
}

func (in *TunRep) bindSa(sa *Sa) error {
	var err error

	nlink := utils.NewNetlinkWrapperWithArgs(config.GlobalConfig.Tracer)
	ctx := context.Background()

	// GRD routing table number
	routingTableNum := 255

	in.Spec.Sa = sa.Name
	in.Spec.Spi = sa.Spec.Spi
	in.Spec.SrcIP = sa.Spec.SrcIP
	in.Spec.DstIP = sa.Spec.DstIP
	in.Spec.SaIdx = sa.Index
	in.Spec.SrcMac, err = nlink.ResolveLocalIP(ctx, in.Spec.SrcIP.String(), routingTableNum)
	if err != nil {
		return err
	}

	return nil
}

func (in *TunRep) unbindSa(sa *Sa) error {
	if in.Spec.Sa != sa.Name {
		log.Printf("unbindSa() : Failed to unbind SA %s from tunnel representor %+v. SA %s bound to Tunnel representor.\n", sa.Name, in, in.Spec.Sa)
		return ErrTunRepNotBoundToSa
	}

	in.Spec.Sa = ""
	in.Spec.Spi = nil
	in.Spec.SrcIP = nil
	in.Spec.DstIP = nil
	in.Spec.SaIdx = nil
	in.Spec.SrcMac = ""

	return nil
}

// prepareObjectsForReplay prepares an object for replay by setting the unsuccessful components
// in pending state and returning a list of the components that need to be contacted for the
// replay of the particular object that called the function.
func (in *TunRep) prepareObjectsForReplay(componentName string, tunRepSubs []*eventbus.Subscriber) []*eventbus.Subscriber {
	// We assume that the list of Components that are returned
	// from DB is ordered based on the priority as that was the
	// way that has been stored in the DB in first place.
	tunRepComponents := in.Status.Components
	tempSubs := []*eventbus.Subscriber{}
	for i, comp := range tunRepComponents {
		if comp.Name == componentName || comp.CompStatus != common.ComponentStatusSuccess {
			in.Status.Components[i] = common.Component{Name: comp.Name, CompStatus: common.ComponentStatusPending, Details: ""}
			tempSubs = append(tempSubs, tunRepSubs[i])
		}
	}
	if in.Status.TunRepOperStatus == BridgePortOperStatusUp {
		in.Status.TunRepOperStatus = TunRepOperStatusDown
	}

	in.ResourceVersion = generateVersion()
	return tempSubs
}
