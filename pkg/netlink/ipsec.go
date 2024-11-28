// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

// Package netlink handles the netlink related functionality
package netlink

import (
	"log"
	"time"

	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriberframework/eventbus"
)

// HandleEvent handles the events
func (h *ModuleNetlinkHandler) HandleEvent(eventType string, objectData *eventbus.ObjectData) {
	switch eventType {
	case "tun-rep":
		log.Printf("Netlink recevied %s %s\n", eventType, objectData.Name)
		handleTunRep(objectData)
	default:
		log.Printf("error: Unknown event type %s", eventType)
	}
}
func handleTunRep(objectData *eventbus.ObjectData) {
	var comp common.Component
	tr, err := infradb.GetTunRep(objectData.Name)
	if err != nil {
		log.Printf("Netlink: GetTunRep error: %s %s\n", err, objectData.Name)
		comp.Name = netlinkComp
		comp.CompStatus = common.ComponentStatusError
		if comp.Timer == 0 {
			comp.Timer = 2 * time.Second
		} else {
			comp.Timer *= 2
		}
		err := infradb.UpdateTunRepStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating tr status: %s\n", err)
		}
		return
	}
	if objectData.ResourceVersion != tr.ResourceVersion {
		log.Printf("Netlink: Mismatch in resoruce version %+v\n and tr resource version %+v\n", objectData.ResourceVersion, tr.ResourceVersion)
		comp.Name = netlinkComp
		comp.CompStatus = common.ComponentStatusError
		if comp.Timer == 0 {
			comp.Timer = 2 * time.Second
		} else {
			comp.Timer *= 2
		}
		err := infradb.UpdateTunRepStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating tr status: %s\n", err)
		}
		return
	}
	if len(tr.Status.Components) != 0 {
		for i := 0; i < len(tr.Status.Components); i++ {
			if tr.Status.Components[i].Name == netlinkComp {
				comp = tr.Status.Components[i]
			}
		}
	}
	if tr.Status.TunRepOperStatus != infradb.TunRepOperStatusToBeDeleted {
		var status bool
		// The reason for having two functions instead of one is beacuase in future maybe we want to differentiate between
		// an update event of Tunnel rep object and an addition event of tunnel rep object. But right now addition
		// and update events of Tunnel rep object do not differ from functionality netlink watcher perspective.
		if len(tr.OldVersions) > 0 {
			status = updateTunRep(tr)
		} else {
			status = addTunRep(tr)
		}
		comp.Name = netlinkComp
		if status {
			comp.Details = ""
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			if comp.Timer == 0 {
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer *= 2
			}
			comp.CompStatus = common.ComponentStatusError
		}
		log.Printf("Netlink: %+v \n", comp)
		err := infradb.UpdateTunRepStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating tr status: %s\n", err)
		}
	} else {
		status := deleteTunRep(tr)
		comp.Name = netlinkComp
		if status {
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			comp.CompStatus = common.ComponentStatusError
			if comp.Timer == 0 {
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer *= 2
			}
		}
		log.Printf("Netlink: %+v\n", comp)
		err := infradb.UpdateTunRepStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating tr status: %s\n", err)
		}
	}
}

func deleteTunRep(tr *infradb.TunRep) bool {
	delete(tun_reps, tr.Name)
	return true

}

func updateTunRep(newRep *infradb.TunRep) bool {
	tun_reps[newRep.Spec.IfName] = newRep.Name
	/*if oldRep.Spec.Sa != "" && newRep.Spec.Sa != "" && newRep.Spec.Sa != oldRep.Spec.Sa {
		if newRep.Spec.DstIP == oldRep.Spec.DstIP {

			log.Printf("Updating IPSec nexthops with metadata from updated %v", newRep)
			for _, nh := range nexthops {
				if (nh.NhType == TUN || nh.NhType == VXLAN_TUN) && nh.Metadata["sa_idx"] == oldRep.Spec.SaIdx {

					NewNH := *nh
					NewNH.Metadata = deepCopyMetadata(nh.Metadata)
					nh.Metadata["local_tep_ip"] = newRep.Spec.SrcIP
					nh.Metadata["spi"] = newRep.Spec.Spi
					nh.Metadata["sa_idx"] = newRep.Spec.SaIdx
					notifyAddDel(nh, nexthopOperations.Delete)
					notifyAddDel(NewNH, nexthopOperations.Add)
				}
			}
		}
	}*/
	return true
}

func addTunRep(tr *infradb.TunRep) bool {
	tun_reps[tr.Spec.IfName] = tr.Name
	return true
}
