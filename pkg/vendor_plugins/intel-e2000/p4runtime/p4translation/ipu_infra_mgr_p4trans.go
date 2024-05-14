// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

package p4translation

import (
	"encoding/json"
	"fmt"

	"log"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
	"github.com/opiproject/opi-evpn-bridge/pkg/config"
	nm "github.com/opiproject/opi-evpn-bridge/pkg/netlink"
	eb "github.com/opiproject/opi-evpn-bridge/pkg/netlink/event_bus"
	p4client "github.com/opiproject/opi-evpn-bridge/pkg/vendor_plugins/intel-e2000/p4runtime/p4driverAPI"
	"google.golang.org/grpc"


	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriberframework/eventbus"
)

var L3 L3Decoder
var Vxlan VxlanDecoder
var Pod PodDecoder

type ModuleipuHandler struct{}

func isValidMAC(mac string) bool {
	macPattern := `^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$`

	match, _ := regexp.MatchString(macPattern, mac)
	return match
}
func getMac(dev string) string {
	cmd := exec.Command("ip", "-d", "-j", "link", "show", dev)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("intel-e2000: Error running command: %v\n", err)
		return ""
	}

	var links []struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(out, &links); err != nil {
		log.Printf("intel-e2000: Error unmarshaling JSON: %v\n", err)
		return ""
	}

	if len(links) > 0 {
		mac := links[0].Address
		return mac
	}

	return ""
}

func vport_from_mac(mac string) int {
	mbyte := strings.Split(mac, ":")
	if len(mbyte) < 5 {
		return -1
	}
	byte0, _ := strconv.ParseInt(mbyte[0], 16, 64)
	byte1, _ := strconv.ParseInt(mbyte[1], 16, 64)

	return int(byte0<<8 + byte1)
}

func ids_of(value string) (string, string, error) {
	if isValidMAC(value) {
		return strconv.Itoa(vport_from_mac(value)), value, nil
	}
	mac := getMac(value)
	vsi := vport_from_mac(mac)
	return strconv.Itoa(vsi), mac, nil
}

var (
	defaultAddr = fmt.Sprintf("127.0.0.1:9559")
	Conn        *grpc.ClientConn
)

func startSubscriber(eventBus *eb.EventBus, eventType string) {
	subscriber := eventBus.Subscribe(eventType)

	go func() {
		for {
			select {
			case event := <-subscriber.Ch:
				log.Printf("intel-e2000: Subscriber for %s received event: %s\n", eventType, event)
				switch eventType {
				case "route_added":
					handleRouteAdded(event)
				case "route_updated":
					handleRouteUpdated(event)
				case "route_deleted":
					handleRouteDeleted(event)
				case "nexthop_added":
					handleNexthopAdded(event)
				case "nexthop_updated":
					handleNexthopUpdated(event)
				case "nexthop_deleted":
					handleNexthopDeleted(event)
				case "fdb_entry_added":
					handleFbdEntryAdded(event)
				case "fdb_entry_updated":
					handleFbdEntryUpdated(event)
				case "fdb_entry_deleted":
					handleFbdEntryDeleted(event)
				case "l2_nexthop_added":
					handleL2NexthopAdded(event)
				case "l2_nexthop_updated":
					handleL2NexthopUpdated(event)
				case "l2_nexthop_deleted":
					handleL2NexthopDeleted(event)
				}
			case <-subscriber.Quit:
				return
			}
		}
	}()
}

func handleRouteAdded(route interface{}) {
	var entries []interface{}
	routeData, _ := route.(nm.RouteStruct)
	entries = L3.translate_added_route(routeData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil{
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Printf("intel-e2000: Entry is not of type p4client.TableEntry:- %v\n", e)
		}
	}
}

func handleRouteUpdated(route interface{}) {
	var entries []interface{}
	routeData, _ := route.(nm.RouteStruct)
	entries = L3.translate_deleted_route(routeData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = append(entries, L3.translate_added_route(routeData))
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}
func handleRouteDeleted(route interface{}) {
	var entries []interface{}
	routeData, _ := route.(nm.RouteStruct)
	entries = L3.translate_deleted_route(routeData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}

func handleNexthopAdded(nexthop interface{}) {
	var entries []interface{}
	nexthopData, _ := nexthop.(nm.NexthopStruct)
	entries = L3.translate_added_nexthop(nexthopData)

	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Vxlan.translate_added_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}
func handleNexthopUpdated(nexthop interface{}) {
	var entries []interface{}
	nexthopData, _ := nexthop.(nm.NexthopStruct)
	entries = L3.translate_deleted_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Vxlan.translate_deleted_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = L3.translate_added_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Vxlan.translate_added_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}

func handleNexthopDeleted(nexthop interface{}) {
	var entries []interface{}
	nexthopData, _ := nexthop.(nm.NexthopStruct)
	entries = L3.translate_deleted_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Vxlan.translate_deleted_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}
func handleFbdEntryAdded(fbdEntry interface{}) {
	var entries []interface{}
	fbdEntryData, _ := fbdEntry.(nm.FdbEntryStruct)
	entries = Vxlan.translate_added_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_added_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}

func handleFbdEntryUpdated(fdbEntry interface{}) {
	var entries []interface{}
	fbdEntryData, _ := fdbEntry.(nm.FdbEntryStruct)
	entries = Vxlan.translate_deleted_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_deleted_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}

	entries = Vxlan.translate_added_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_added_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}
func handleFbdEntryDeleted(fdbEntry interface{}) {
	var entries []interface{}
	fbdEntryData, _ := fdbEntry.(nm.FdbEntryStruct)
	entries = Vxlan.translate_deleted_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_deleted_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}

func handleL2NexthopAdded(l2NextHop interface{}) {
	var entries []interface{}
	l2NextHopData, _ := l2NextHop.(nm.L2NexthopStruct)

	entries = Vxlan.translate_added_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_added_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}
func handleL2NexthopUpdated(l2NextHop interface{}) {
	var entries []interface{}
	l2NextHopData, _ := l2NextHop.(nm.L2NexthopStruct)
	entries = Vxlan.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Vxlan.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("iintel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}

func handleL2NexthopDeleted(l2NextHop interface{}) {
	var entries []interface{}
	l2NextHopData, _ := l2NextHop.(nm.L2NexthopStruct)
	entries = Vxlan.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error deleting entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}

// InfraDB event Handler
func (h *ModuleipuHandler) HandleEvent(eventType string, objectData *eventbus.ObjectData) {
	switch eventType {
	case "vrf":
		log.Printf("intel-e2000: recevied %s %s\n", eventType, objectData.Name)
		handlevrf(objectData)
	case "logical-bridge":
		log.Printf("inyel-e2000: recevied %s %s\n", eventType, objectData.Name)
		handlelb(objectData)
	case "bridge-port":
		log.Printf("intel-e2000: recevied %s %s\n", eventType, objectData.Name)
		handlebp(objectData)
	case "svi":
		log.Printf("intel-e2000: recevied %s %s\n", eventType, objectData.Name)
		handlesvi(objectData)
	default:
		log.Println("intel-e2000: error: Unknown event type %s", eventType)
	}
}

func handlevrf(objectData *eventbus.ObjectData) {
	var comp common.Component
	VRF, err := infradb.GetVrf(objectData.Name)
	if err != nil {
		log.Printf("intel-e2000: GetVRF error: %s %s\n", err, objectData.Name)
		return
	} else {
		log.Printf("intel-e2000 : GetVRF Name: %s\n", VRF.Name)
	}
	if objectData.ResourceVersion != VRF.ResourceVersion {
		log.Printf("intel-e2000: Mismatch in resoruce version %+v\n and VRF resource version %+v\n", objectData.ResourceVersion, VRF.ResourceVersion)
		comp.Name = "intel-e2000"
		comp.CompStatus = common.ComponentStatusError
		if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
			comp.Timer = 2 * time.Second
		} else {
			comp.Timer = comp.Timer * 2
		}
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		return
	}

	if len(VRF.Status.Components) != 0 {
		for i := 0; i < len(VRF.Status.Components); i++ {
			if VRF.Status.Components[i].Name == "intel-e2000" {
				comp = VRF.Status.Components[i]
			}
		}
	}
	if VRF.Status.VrfOperStatus != infradb.VrfOperStatusToBeDeleted {
		details, status := offload_vrf(VRF)
		if status == true {
			comp.Details = details
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Name = "intel-e2000"
			comp.Timer = 0
		} else {
			if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2 * time.Second
			}
			comp.Name = "intel-e2000"
			comp.CompStatus = common.ComponentStatusError
		}
		log.Printf("intel-e2000: %+v\n", comp)
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, VRF.Metadata, comp)
	} else {
		status := tear_down_vrf(VRF)
		if status == true {
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Name = "intel-e2000"
			comp.Timer = 0
		} else {
			comp.CompStatus = common.ComponentStatusError
			comp.Name = "intel-e2000"
			if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
				comp.Timer = 2
			} else {
				comp.Timer = comp.Timer * 2
			}
		}
		log.Printf("intel-e2000: %+v\n", comp)
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
	}
}

func handlelb(objectData *eventbus.ObjectData) {
        var comp common.Component
        LB, err := infradb.GetLB(objectData.Name)
        if err != nil {
                log.Printf("intel-e2000: GetLB error: %s %s\n", err, objectData.Name)
                return
        } else {
		log.Printf("intel-e2000: GetLB Name: %s\n", LB.Name)
        }
	if len(LB.Status.Components) != 0 {
		for i := 0; i < len(LB.Status.Components); i++ {
			if LB.Status.Components[i].Name == "intel-e2000" {
				comp = LB.Status.Components[i]
			}
		}
	}
	if LB.Status.LBOperStatus != infradb.LogicalBridgeOperStatusToBeDeleted {
		status := set_up_lb(LB)
		comp.Name = "intel-e2000"
		if status == true {
			comp.Details = ""
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			if comp.Timer == 0 {
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
			comp.CompStatus = common.ComponentStatusError
		}
		log.Printf("intel-e2000: %+v \n", comp)
		infradb.UpdateLBStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
	} else {
		status := tear_down_lb(LB)
		comp.Name = "intel-e2000"
		if status == true {
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			comp.CompStatus = common.ComponentStatusError
			if comp.Timer == 0 {
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
		}
		log.Printf("intel-e2000: %+v\n", comp)
		infradb.UpdateLBStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
	}
}

func handlebp(objectData *eventbus.ObjectData){
	var comp common.Component
	BP, err := infradb.GetBP(objectData.Name)
	if err != nil {
		log.Printf("intel-e2000: GetBP error: %s\n", err)
		return
	} else {
                log.Printf("intel-e2000: GetBP Name: %s\n", BP.Name)
        }
	if (len(BP.Status.Components) != 0 ){
		for i:=0;i<len(BP.Status.Components);i++ {
			if (BP.Status.Components[i].Name == "intel-e2000") {
				comp = BP.Status.Components[i]
			}
		}
	}
	if (BP.Status.BPOperStatus !=infradb.BridgePortOperStatusToBeDeleted){
		status := set_up_bp(BP)
		comp.Name= "intel-e2000"
		if (status == true) {
			comp.Details = ""
			comp.CompStatus= common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			if comp.Timer ==0 {
				comp.Timer=2 * time.Second
			} else {
				comp.Timer=comp.Timer*2
			}
			comp.CompStatus = common.ComponentStatusError
		}
		log.Printf("intel-e2000: %+v \n",comp)
		infradb.UpdateBPStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationID,nil,comp)
	}else {
		status := tear_down_bp(BP)
		comp.Name= "intel-e2000"
		if (status == true) {
			comp.CompStatus= common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			if comp.Timer ==0 {
				comp.Timer=2 * time.Second
			} else {
				comp.Timer=comp.Timer*2
			}
			comp.CompStatus = common.ComponentStatusError
		}
		log.Printf("intel-e2000: %+v \n",comp)
		infradb.UpdateBPStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationID,nil,comp)
	}
}

func handlesvi(objectData *eventbus.ObjectData) {
	var comp common.Component
	SVI, err := infradb.GetSvi(objectData.Name)
	if err != nil {
		log.Printf("intel-e2000: GetSvi error: %s %s\n", err, objectData.Name)
		return
	} else {
		log.Printf("intel-e2000 : GetSvi Name: %s\n", SVI.Name)
	}
	if (objectData.ResourceVersion != SVI.ResourceVersion){
		log.Printf("intel-e2000: Mismatch in resoruce version %+v\n and SVI resource version %+v\n", objectData.ResourceVersion, SVI.ResourceVersion)
		comp.Name= "intl-e2000"
		comp.CompStatus= common.ComponentStatusError
		if comp.Timer ==0 {
			comp.Timer=2 * time.Second
		} else {
			comp.Timer=comp.Timer*2
		}
		infradb.UpdateSviStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationID,nil,comp)
		return
	}
	if len(SVI.Status.Components) != 0 {
		for i := 0; i < len(SVI.Status.Components); i++ {
			if SVI.Status.Components[i].Name == "intel-e2000" {
				comp = SVI.Status.Components[i]
			}
		}
	}
	if SVI.Status.SviOperStatus != infradb.SviOperStatusToBeDeleted {
		details, status := set_up_svi(SVI)
		comp.Name = "intel-e2000"
		if status == true {
			comp.Details = details
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			if comp.Timer == 0 {
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
			comp.CompStatus = common.ComponentStatusError
		}
		log.Printf("intel-e2000: %+v \n", comp)
		infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
	} else {
		status := tear_down_svi(SVI)
		comp.Name = "intel-e2000"
		if status == true {
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			comp.CompStatus = common.ComponentStatusError
			if comp.Timer == 0 {
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
		}
		log.Printf("intel-e2000: %+v \n", comp)
		infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
	}
}

func offload_vrf(VRF *infradb.Vrf) (string, bool) {
	if path.Base(VRF.Name) == "GRD" {
		return "", true
	}
	var entries []interface{}
	entries = Vxlan.translate_added_vrf(VRF)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("ntel-e2000: Entry is not of type p4client.TableEntry:-", e)
			return "", false
		}
	}
	return "", true
}


func set_up_lb(LB *infradb.LogicalBridge) (bool) {
	var entries []interface{}
	entries = Vxlan.translate_added_lb(LB)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry:-", e)
			return false
		}
	}
	return true
}

func set_up_bp(BP *infradb.BridgePort)(bool) {
	var entries []interface{}
	entries, err := Pod.translate_added_bp(BP)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry:-", e)
			return false
		}
	}
	return true
}

func set_up_svi(SVI *infradb.Svi) (string, bool) {
	var entries []interface{}
	entries, err := Pod.translate_added_svi(SVI)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry:-", e)
			return "", false
		}
	}
	return "", true
}

func tear_down_vrf(VRF *infradb.Vrf) bool {
	if path.Base(VRF.Name) == "GRD" {
		return true
	}
	var entries []interface{}
	entries = Vxlan.translate_deleted_vrf(VRF)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
			return false
		}
	}
	return true
}


func tear_down_lb(LB *infradb.LogicalBridge) bool {
	var entries []interface{}
	entries = Vxlan.translate_deleted_lb(LB)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
			return false
		}
	}
	return true
}

func tear_down_bp(BP *infradb.BridgePort) bool {
        var entries []interface{}
	entries, err := Pod.translate_deleted_bp(BP)
	if err != nil {
		return false
	}
        for _, entry := range entries {
                if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
                } else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
                        return false
                }
        }
        return true
}

func tear_down_svi(SVI *infradb.Svi) bool {
        var entries []interface{}
	entries, err := Pod.translate_deleted_svi(SVI)
	if err != nil {
		return false
	}
        for _, entry := range entries {
                if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
                } else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
                        return false
                }
        }
        return true
}
func Init() {
	// Netlink Listener
	startSubscriber(nm.EventBus, "route_added")

	startSubscriber(nm.EventBus, "route_updated")
	startSubscriber(nm.EventBus, "route_deleted")
	startSubscriber(nm.EventBus, "nexthop_added")
	startSubscriber(nm.EventBus, "nexthop_updated")
	startSubscriber(nm.EventBus, "nexthop_deleted")
	startSubscriber(nm.EventBus, "fdb_entry_added")
	startSubscriber(nm.EventBus, "fdb_entry_updated")
	startSubscriber(nm.EventBus, "fdb_entry_deleted")
	startSubscriber(nm.EventBus, "l2_nexthop_added")
	startSubscriber(nm.EventBus, "l2_nexthop_updated")
	startSubscriber(nm.EventBus, "l2_nexthop_deleted")

	// InfraDB Listener

	eb := eventbus.EBus
	for _, subscriberConfig := range config.GlobalConfig.Subscribers {
		if subscriberConfig.Name == "intel-e2000" {
			for _, eventType := range subscriberConfig.Events {
				eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModuleipuHandler{})
			}
		}
	}
	// Setup p4runtime connection
	Conn, err := grpc.Dial(defaultAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("intel-e2000: Cannot connect to server: %v\n", err)
	}

	err1 := p4client.NewP4RuntimeClient(config.GlobalConfig.P4.Config.BinFile, config.GlobalConfig.P4.Config.P4infoFile, Conn)
	if err1 != nil {
		log.Fatalf("intel-e2000: Failed to create P4Runtime client: %v\n", err1)
	}
	// add static rules into the pipeline of representators read from config
	representors := make(map[string][2]string)
	for k, v := range config.GlobalConfig.P4.Representors {
		vsi, mac, err := ids_of(v.(string))
		if err != nil {
			log.Println("intel-e2000: Error:", err)
			return
		}
		representors[k] = [2]string{vsi, mac}
	}
	log.Println("intel-e2000: REPRESENTORS %+v\n", representors)
	L3 = L3.L3DecoderInit(representors)
	Pod = Pod.PodDecoderInit(representors)
	// decoders = []interface{}{L3, Vxlan, Pod}
	Vxlan = Vxlan.VxlanDecoderInit(representors)
	L3entries := L3.Static_additions()
	for _, entry := range L3entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	Podentries := Pod.Static_additions()
	for _, entry := range Podentries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Add_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}

func Exit() {
	L3entries := L3.Static_deletions()
	for _, entry := range L3entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
	Podentries := Pod.Static_deletions()
	for _, entry := range Podentries {
		if e, ok := entry.(p4client.TableEntry); ok {
			er := p4client.Del_entry(e)
			if er != nil {
				log.Printf("intel-e2000: error adding entry for %v error %v\n",e.Tablename, er)
			}
		} else {
			log.Println("intel-e2000: Entry is not of type p4client.TableEntry")
		}
	}
}
