// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

// Package frr handles the frr related functionality
package frr

import (
	"context"
	"encoding/json"
	"fmt"

	"log"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/opiproject/opi-evpn-bridge/pkg/config"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriberframework/actionbus"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriberframework/eventbus"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils"
)

// frrComp string constant
const frrComp string = "frr"

// runningFrrConfFile holds the running configuration of FRR daemon
const runningFrrConfFile = "/etc/frr/frr.conf"

// basicFrrConfFile holds the basic/initial configuration of FRR daemon
const basicFrrConfFile = "/etc/frr/frr-basic.conf"

// backupFrrConfFile holds the backup configuration the current running config of FRR daemon
const backupFrrConfFile = "/etc/frr/frr.conf.bak"

const replayThreshold = 64 * time.Second

// ModulefrrHandler empty structure
type ModulefrrHandler struct{}

// ModuleFrrActionHandler empty structure
type ModuleFrrActionHandler struct{}

// HandleEvent handles the events
func (h *ModulefrrHandler) HandleEvent(eventType string, objectData *eventbus.ObjectData) {
	switch eventType {
	case "vrf": // "VRF_added":
		log.Printf("FRR recevied %s %s\n", eventType, objectData.Name)
		handlevrf(objectData)
	case "svi":
		log.Printf("FRR recevied %s %s\n", eventType, objectData.Name)
		handlesvi(objectData)
	case "tun-rep":
		log.Printf("FRR recevied %s %s\n", eventType, objectData.Name)
		handleTunRep(objectData)
	default:
		log.Printf("error: Unknown event type %s", eventType)
	}
}

// HandleAction handles the actions
func (h *ModuleFrrActionHandler) HandleAction(actionType string, actionData *actionbus.ActionData) {
	switch actionType {
	case "preReplay":
		log.Printf("Module FRR received %s\n", actionType)
		handlePreReplay(actionData)
	default:
		log.Printf("error: Unknown action type %s", actionType)
	}
}

func handlePreReplay(actionData *actionbus.ActionData) {
	var deferErr error

	defer func() {
		// The ErrCh is used in order to notify the sender that the preReplay step has
		// been executed successfully.
		actionData.ErrCh <- deferErr
	}()

	// Backup the current running config
	if err := os.Rename(runningFrrConfFile, backupFrrConfFile); err != nil {
		log.Printf("FRR: handlePreReplay(): Failed to backup running config of FRR: %s\n", err)
		deferErr = err
		return
	}

	// Create a new running config based on the basic/initial FRR config
	input, err := os.ReadFile(basicFrrConfFile)
	if err != nil {
		log.Printf("FRR: handlePreReplay(): Failed to read content of %s: %s\n", basicFrrConfFile, err)
		deferErr = err
		return
	}

	if err := os.WriteFile(runningFrrConfFile, input, 0600); err != nil {
		log.Printf("FRR: handlePreReplay(): Failed to write content to %s: %s\n", runningFrrConfFile, err)
		deferErr = err
		return
	}

	// Change ownership of the frr.conf to frr:frr
	group, err := user.Lookup("frr")
	if err != nil {
		log.Printf("FRR: handlePreReplay(): Failed to lookup user frr %s\n", err)
		deferErr = err
		return
	}

	uid, _ := strconv.Atoi(group.Uid)
	gid, _ := strconv.Atoi(group.Gid)

	if err := os.Chown(runningFrrConfFile, uid, gid); err != nil {
		log.Printf("FRR: handlePreReplay(): Failed to chown of %s to frr:frr : %s\n", runningFrrConfFile, err)
		deferErr = err
		return
	}

	// Restart FRR daemon
	_, errCmd := utils.Run([]string{"systemctl", "restart", "frr"}, false)
	if errCmd != 0 {
		log.Println("FRR: handlePreReplay(): Failed to restart FRR daemon")
		err := fmt.Errorf("restart FRR daemon failed")
		deferErr = err
		return
	}

	log.Println("FRR: handlePreReplay(): The pre-replay procedure has executed successfully")
}

func handleTunRep(objectData *eventbus.ObjectData) {
	var comp common.Component
	tr, err := infradb.GetTunRep(objectData.Name)
	if err != nil {
		log.Printf("FRR: GetTunRep error: %s %s\n", err, objectData.Name)
		comp.Name = frrComp
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
		log.Printf("FRR: Mismatch in resoruce version %+v\n and tr resource version %+v\n", objectData.ResourceVersion, tr.ResourceVersion)
		comp.Name = frrComp
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
			if tr.Status.Components[i].Name == frrComp {
				comp = tr.Status.Components[i]
			}
		}
	}
	if tr.Status.TunRepOperStatus != infradb.TunRepOperStatusToBeDeleted {
		var status bool
		if len(tr.OldVersions) > 0 {
			status = UpdateTunRep(tr)
		} else {
			status = setUpTunRep(tr)
		}
		comp.Name = frrComp
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
		log.Printf("FRR: %+v \n", comp)

		// Checking the timer to decide if we need to replay or not
		comp.CheckReplayThreshold(replayThreshold)

		err := infradb.UpdateTunRepStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating tr status: %s\n", err)
		}
	} else {
		status := tearDownTunRep(tr)
		comp.Name = frrComp
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
		log.Printf("FRR: %+v\n", comp)

		// Checking the timer to decide if we need to replay or not
		comp.CheckReplayThreshold(replayThreshold)

		err := infradb.UpdateTunRepStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating tr status: %s\n", err)
		}
	}
}

// handlesvi handles the svi functionality
//
//nolint:funlen,gocognit
func handlesvi(objectData *eventbus.ObjectData) {
	var comp common.Component
	svi, err := infradb.GetSvi(objectData.Name)
	if err != nil {
		log.Printf("GetSvi error: %s %s\n", err, objectData.Name)
		comp.Name = frrComp
		comp.CompStatus = common.ComponentStatusError
		if comp.Timer == 0 {
			comp.Timer = 2 * time.Second
		} else {
			comp.Timer *= 2
		}
		err := infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating svi status: %s\n", err)
		}
		return
	}

	if objectData.ResourceVersion != svi.ResourceVersion {
		log.Printf("FRR: Mismatch in resoruce version %+v\n and svi resource version %+v\n", objectData.ResourceVersion, svi.ResourceVersion)
		comp.Name = frrComp
		comp.CompStatus = common.ComponentStatusError
		if comp.Timer == 0 {
			comp.Timer = 2 * time.Second
		} else {
			comp.Timer *= 2
		}
		err := infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating svi status: %s\n", err)
		}
		return
	}
	if len(svi.Status.Components) != 0 {
		for i := 0; i < len(svi.Status.Components); i++ {
			if svi.Status.Components[i].Name == frrComp {
				comp = svi.Status.Components[i]
			}
		}
	}
	if svi.Status.SviOperStatus != infradb.SviOperStatusToBeDeleted {
		status := setUpSvi(svi)
		comp.Name = frrComp
		if status {
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
		log.Printf("%+v\n", comp)

		// Checking the timer to decide if we need to replay or not
		if comp.Timer > replayThreshold {
			comp.Replay = true
		}

		err := infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating svi status: %s\n", err)
		}
	} else {
		status := tearDownSvi(svi)
		comp.Name = frrComp
		if status {
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
		log.Printf("%+v\n", comp)

		// Checking the timer to decide if we need to replay or not
		if comp.Timer > replayThreshold {
			comp.Replay = true
		}

		err := infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating svi status: %s\n", err)
		}
	}
}

// handlevrf handles the vrf functionality
//
//nolint:funlen,gocognit
func handlevrf(objectData *eventbus.ObjectData) {
	var comp common.Component
	vrf, err := infradb.GetVrf(objectData.Name)
	if err != nil {
		log.Printf("GetVRF error: %s %s\n", err, objectData.Name)
		comp.Name = frrComp
		comp.CompStatus = common.ComponentStatusError
		if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
			comp.Timer = 2 * time.Second
		} else {
			comp.Timer *= 2
		}
		err := infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating vrf status: %s\n", err)
		}
		return
	}

	if objectData.ResourceVersion != vrf.ResourceVersion {
		log.Printf("FRR: Mismatch in resoruce version %+v\n and vrf resource version %+v\n", objectData.ResourceVersion, vrf.ResourceVersion)
		comp.Name = frrComp
		comp.CompStatus = common.ComponentStatusError
		if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
			comp.Timer = 2 * time.Second
		} else {
			comp.Timer *= 2
		}
		err := infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating vrf status: %s\n", err)
		}
		return
	}
	if len(vrf.Status.Components) != 0 {
		for i := 0; i < len(vrf.Status.Components); i++ {
			if vrf.Status.Components[i].Name == frrComp {
				comp = vrf.Status.Components[i]
			}
		}
	}
	if vrf.Status.VrfOperStatus != infradb.VrfOperStatusToBeDeleted {
		detail, status := setUpVrf(vrf)
		comp.Name = frrComp
		if status {
			comp.Details = detail
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer *= 2
			}
			comp.CompStatus = common.ComponentStatusError
		}
		log.Printf("%+v\n", comp)

		// Checking the timer to decide if we need to replay or not
		if comp.Timer > replayThreshold {
			comp.Replay = true
		}

		err := infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating vrf status: %s\n", err)
		}
	} else {
		status := tearDownVrf(vrf)
		comp.Name = frrComp
		if status {
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer *= 2
			}
			comp.CompStatus = common.ComponentStatusError
		}
		log.Printf("%+v\n", comp)

		// Checking the timer to decide if we need to replay or not
		if comp.Timer > replayThreshold {
			comp.Replay = true
		}

		err := infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		if err != nil {
			log.Printf("error in updating vrf status: %s\n", err)
		}
	}
}

// run function runs the command
func run(cmd []string, flag bool) (string, int) {
	//  fmt.Println("FRR: Executing command", cmd)
	var out []byte
	var err error
	//  out, err = exec.Command("sudo",cmd...).Output()
	out, err = exec.Command(cmd[0], cmd[1:]...).CombinedOutput() //nolint:gosec
	if err != nil {
		if flag {
			panic(fmt.Sprintf("FRR: Command %s': exit code %s;", out, err.Error()))
		}
		log.Printf("FRR: Command %s': exit code %s;", out, err)
		return "Error", -1
	}
	output := string(out)
	return output, 0
}

var defaultVtep, portMux, vrfMux string

var localas int

// var brTenant int

// subscribeInfradb function handles the infradb subscriptions
func subscribeInfradb(config *config.Config) {
	eb := eventbus.EBus
	ab := actionbus.ABus
	for _, subscriberConfig := range config.Subscribers {
		if subscriberConfig.Name == frrComp {
			for _, eventType := range subscriberConfig.Events {
				eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModulefrrHandler{})
			}
		}
	}
	ab.StartSubscriber(frrComp, "preReplay", &ModuleFrrActionHandler{})
}

// ctx variable of type context
var ctx context.Context

// Frr variable of type utils wrapper
var Frr utils.Frr

// Initialize function handles init functionality
func Initialize() {
	frrEnabled := config.GlobalConfig.LinuxFrr.Enabled
	if !frrEnabled {
		log.Println("FRR Module disabled")
		return
	}
	defaultVtep = config.GlobalConfig.LinuxFrr.DefaultVtep
	localas = config.GlobalConfig.LinuxFrr.LocalAs
	portMux = config.GlobalConfig.Interfaces.PortMux
	vrfMux = config.GlobalConfig.Interfaces.VrfMux
	log.Printf(" frr vtep: %+v port-mux %+v vrf-mux: +%v", defaultVtep, portMux, vrfMux)
	// Subscribe to InfraDB notifications
	subscribeInfradb(&config.GlobalConfig)

	ctx = context.Background()
	Frr = utils.NewFrrWrapperWithArgs("localhost", config.GlobalConfig.Tracer)

	// Make sure IPv4 forwarding is enabled.
	detail, flag := run([]string{"sysctl", "-w", " net.ipv4.ip_forward=1"}, false)
	if flag != 0 {
		log.Println("Error in running command", detail)
	}
}

// DeInitialize function handles stops functionality
func DeInitialize() {
	frrEnabled := config.GlobalConfig.LinuxFrr.Enabled
	if !frrEnabled {
		log.Println("FRR Module disabled")
		return
	}
	// Unsubscribe to InfraDB notifications
	eb := eventbus.EBus
	eb.UnsubscribeModule(frrComp)
}

// routingTableBusy function checks the routing table
/*func routingTableBusy(table uint32) bool {
	cp, err := run([]string{"ip", "route", "show", "table", strconv.Itoa(int(table))}, false)
	if err != 0 {
		fmt.Println(cp)
		return false
	}
	// fmt.Printf("route table busy %s %s\n",cp,err)
	// Table is busy if it exists and contains some routes
	return true // reflect.ValueOf(cp).IsZero() && len(cp)!= 0
}*/

// VRF structure
type VRF struct {
	Name          string
	Vni           int
	RoutingTables []uint32
	Loopback      net.IP
	// RoutingTables uint32
}

// BgpL2vpnCmd structure
type BgpL2vpnCmd struct {
	Vni                   int
	Type                  string
	InKernel              string
	Rd                    string
	OriginatorIP          string
	AdvertiseGatewayMacip string
	AdvertiseSviMacIP     string
	AdvertisePip          string
	SysIP                 string
	SysMac                string
	Rmac                  string
	ImportRts             []string
	ExportRts             []string
}

// route empty structure
type route struct{}

// BgpVrfCmd structure
type BgpVrfCmd struct {
	VrfID         int
	VrfName       string
	TableVersion  uint
	RouterID      string
	DefaultLocPrf uint
	LocalAS       int
	Routes        route
}

// setUpVrf sets up the vrf
func setUpVrf(vrf *infradb.Vrf) (string, bool) {
	// This function must not be executed for the vrf representing the GRD
	if path.Base(vrf.Name) == "GRD" {
		return "", true
	}
	if !reflect.ValueOf(vrf.Spec.Vni).IsZero() {
		// Configure the vrf in FRR and set up BGP EVPN for it
		vrfName := fmt.Sprintf("vrf %s", path.Base(vrf.Name))
		vniID := fmt.Sprintf("vni %s", strconv.Itoa(int(*vrf.Spec.Vni)))
		data, err := Frr.FrrZebraCmd(ctx, fmt.Sprintf("configure terminal\n %s\n %s\n exit-vrf\n exit", vrfName, vniID))
		if err != nil || checkFrrResult(data, false) {
			log.Printf("FRR: Error Executing frr config t %s %s exit-vrf exit data %v err is %v data is %v\n", vrfName, vniID, data, err, data)
			return "", false
		}
		err = Frr.Save(ctx)
		if err != nil {
			log.Printf("FRR(setUpVrf): Failed to run save command: %v\n", err)
		}
		log.Printf("FRR: Executed frr config t %s %s exit-vrf exit\n", vrfName, vniID)
		var LbiP string

		if reflect.ValueOf(vrf.Spec.LoopbackIP).IsZero() {
			LbiP = "0.0.0.0"
		} else {
			LbiP = fmt.Sprintf("%+v", vrf.Spec.LoopbackIP.IP)
		}
		data, err = Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n router bgp %+v vrf %s\n bgp router-id %s\n no bgp ebgp-requires-policy\n no bgp hard-administrative-reset\n no bgp graceful-restart notification\n address-family ipv4 unicast\n redistribute connected\n redistribute static\n exit-address-family\n address-family l2vpn evpn\n advertise ipv4 unicast\n exit-address-family\n exit", localas, path.Base(vrf.Name), LbiP))
		if err != nil || checkFrrResult(data, false) {
			log.Printf("FRR: Error Executing config t bgpVrfName router bgp %+v vrf %s bgp_route_id %s no bgp ebgp-requires-policy exit-vrf exit data %v \n", localas, vrf.Name, LbiP, data)
			return "", false
		}
		err = Frr.Save(ctx)
		if err != nil {
			log.Printf("FRR(setUpVrf): Failed to run save command: %v\n", err)
		}
		log.Printf("FRR: Executed config t bgpVrfName router bgp %+v vrf %s bgp_route_id %s no bgp ebgp-requires-policy exit-vrf exit\n", localas, vrf.Name, LbiP)
		// Update the vrf with attributes from FRR
		cmd := fmt.Sprintf("show bgp l2vpn evpn vni %d json", *vrf.Spec.Vni)
		cp, err := Frr.FrrBgpCmd(ctx, cmd)
		if err != nil || checkFrrResult(cp, true) {
			log.Printf("FRR Error-show bgp l2vpn evpn vni %v cp %v", err, cp)
		}
		err = Frr.Save(ctx)
		if err != nil {
			log.Printf("FRR(setUpVrf): Failed to run save command: %v\n", err)
		}
		hname, _ := os.Hostname()
		L2vpnCmd := strings.Split(cp, "json")
		L2vpnCmd = strings.Split(L2vpnCmd[1], hname)
		cp = L2vpnCmd[0]
		if len(cp) != 7 { // Checking CMD o/p
			cp = cp[3 : len(cp)-3]
		} else {
			log.Printf("FRR: unable to get the command %s\n", cmd)
			return "", false
		}
		var bgpL2vpn BgpL2vpnCmd
		err1 := json.Unmarshal([]byte(fmt.Sprintf("{%v}", cp)), &bgpL2vpn)
		if err1 != nil {
			log.Printf("error-%v", err)
		}
		cmd = fmt.Sprintf("show bgp vrf %s json", path.Base(vrf.Name))
		cp, err = Frr.FrrBgpCmd(ctx, cmd)
		if err != nil || checkFrrResult(cp, true) {
			log.Printf("error-%v", err)
		}
		err = Frr.Save(ctx)
		if err != nil {
			log.Printf("FRR(setUpVrf): Failed to run save command: %v\n", err)
		}
		BgpCmd := strings.Split(cp, "json")
		BgpCmd = strings.Split(BgpCmd[1], hname)
		cp = BgpCmd[0]

		var bgpVrf BgpVrfCmd
		if len(cp) != 7 {
			cp = cp[5 : len(cp)-5]
		} else {
			log.Printf("FRR: unable to get the command \"%s\"\n", cmd)
			return "", false
		}
		err1 = json.Unmarshal([]byte(fmt.Sprintf("{%v}", cp)), &bgpVrf)
		if err1 != nil {
			log.Printf("error-%v", err)
		}
		log.Printf("FRR: Executed show bgp vrf %s json\n", vrf.Name)
		details := fmt.Sprintf("{ \"rd\":\"%s\",\"rmac\":\"%s\",\"importRts\":[\"%s\"],\"exportRts\":[\"%s\"],\"localAS\":%d }", bgpL2vpn.Rd, bgpL2vpn.Rmac, bgpL2vpn.ImportRts, bgpL2vpn.ExportRts, bgpVrf.LocalAS)
		log.Printf("FRR Details %s\n", details)
		return details, true
	}
	return "", true
}

// checkFrrResult checks the vrf result
func checkFrrResult(cp string, show bool) bool {
	return ((show && reflect.ValueOf(cp).IsZero()) || strings.Contains(cp, "warning") || strings.Contains(cp, "unknown") || strings.Contains(cp, "Unknown") || strings.Contains(cp, "Warning") || strings.Contains(cp, "Ambiguous") || strings.Contains(cp, "specified does not exist") || strings.Contains(cp, "Error"))
}

// setUpSvi sets up the svi
func setUpSvi(svi *infradb.Svi) bool {
	BrObj, err := infradb.GetLB(svi.Spec.LogicalBridge)
	if err != nil {
		log.Printf("FRR: unable to find key %s and error is %v", svi.Spec.LogicalBridge, err)
		return false
	}
	linkSvi := fmt.Sprintf("%+v-%+v", path.Base(svi.Spec.Vrf), BrObj.Spec.VlanID)
	if svi.Spec.EnableBgp && !reflect.ValueOf(svi.Spec.GatewayIPs).IsZero() {
		// gwIP := fmt.Sprintf("%s", svi.Spec.GatewayIPs[0].IP.To4())
		gwIP := string(svi.Spec.GatewayIPs[0].IP.To4())
		RemoteAs := fmt.Sprintf("%d", *svi.Spec.RemoteAs)
		bgpVrfName := fmt.Sprintf("router bgp %+v vrf %s\n", localas, path.Base(svi.Spec.Vrf))
		neighlink := fmt.Sprintf("neighbor %s peer-group\n", linkSvi)
		neighlinkRe := fmt.Sprintf("neighbor %s remote-as %s\n", linkSvi, RemoteAs)
		neighlinkGw := fmt.Sprintf("neighbor %s update-source %s\n", linkSvi, gwIP)
		neighlinkOv := fmt.Sprintf("neighbor %s as-override\n", linkSvi)
		neighlinkSr := fmt.Sprintf("neighbor %s soft-reconfiguration inbound\n", linkSvi)
		bgpListen := fmt.Sprintf(" bgp listen range %s peer-group %s\n", svi.Spec.GatewayIPs[0], linkSvi)

		data, err := Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n %s bgp disable-ebgp-connected-route-check\n %s %s %s %s %s %s exit", bgpVrfName, neighlink, neighlinkRe, neighlinkGw, neighlinkOv, neighlinkSr, bgpListen))

		if err != nil || checkFrrResult(data, false) {
			log.Printf("FRR: Error in conf svi %s %s command %s\n", svi.Name, path.Base(svi.Spec.Vrf), data)
			return false
		}
		err = Frr.Save(ctx)
		if err != nil {
			log.Printf("FRR(setUpSvi): Failed to run save command: %v\n", err)
		}
		return true
	}
	return true
}

func UpdateTunRep(tun *infradb.TunRep) bool {
	for _, tuns := range tun.OldVersions {
		tunObj, err := infradb.GetTunRep(tuns)
		if err == nil {
			if !tearDownTunRep(tunObj) {
				log.Printf("LGM: UpdateTunRep failed for object %+v\n", tunObj)
				return false
			}
		}
	}
	return setUpTunRep(tun)
}

func setUpTunRep(tun *infradb.TunRep) bool {
	if tun.Spec.EnableBgp && tun.Spec.RemoteIp != nil {
		bgpVrfName := fmt.Sprintf("router bgp %+v vrf %s\n", localas, path.Base(tun.Spec.Vrf))
		neighlinkRe := fmt.Sprintf("neighbor %s remote-as %s\n", tun.Spec.IP, tun.Spec.RemoteIp)
		neighebgpMhop := fmt.Sprintf("neighbor %s ebgp-multihop 2\n", tun.Spec.IP)
		neighreconfigure := fmt.Sprintf("neighbor %s soft-reconfiguration inbound\n", tun.Spec.IP)
		neighTimer := fmt.Sprintf("neighbor %s timers 1 3\n", tun.Spec.IP)
		var neighBfd string
		if tun.enableBfd {
			neighBfd = fmt.Sprintf("neighbor %s bfd\n", tun.Spec.IP)
		} else {
			neighBfd = ""
		}

		data, err := Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n %s bgp disable-ebgp-connected-route-check\n no bgp ebgp-requires-policy\n %s %s %s %s %s %s exit", bgpVrfName, neighlinkRe, neighebgpMhop, neighreconfigure, neighTimer, neighBfd))
		if err != nil || checkFrrResult(data, false) {
			log.Printf("FRR: Error in conf tun %s %s command %s\n", tun.Name, path.Base(tun.Spec.Vrf), data)
			return false
		}
		err = Frr.Save(ctx)
		if err != nil {
			log.Printf("FRR(setUpTunRep): Failed to run save command: %v\n", err)
		}
		return true
	}
	return true
}

// tearDownSvi tears down svi
func tearDownSvi(svi *infradb.Svi) bool {
	// linkSvi := fmt.Sprintf("%+v-%+v", path.Base(svi.Spec.Vrf), strings.Split(path.Base(svi.Spec.LogicalBridge), "vlan")[1])
	BrObj, err := infradb.GetLB(svi.Spec.LogicalBridge)
	if err != nil {
		log.Printf("FRR: unable to find key %s and error is %v", svi.Spec.LogicalBridge, err)
		return false
	}
	linkSvi := fmt.Sprintf("%+v-%+v", path.Base(svi.Spec.Vrf), BrObj.Spec.VlanID)
	if svi.Spec.EnableBgp && !reflect.ValueOf(svi.Spec.GatewayIPs).IsZero() {
		bgpVrfName := fmt.Sprintf("router bgp %+v vrf %s", localas, path.Base(svi.Spec.Vrf))
		noNeigh := fmt.Sprintf("no neighbor %s peer-group", linkSvi)
		data, err := Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n %s\n %s\n exit", bgpVrfName, noNeigh))
		if strings.Contains(data, "Create the peer-group first") { // Trying to delete non exist peer-group return true
			return true
		}
		if err != nil || checkFrrResult(data, false) {
			log.Printf("FRR: Error in conf Delete vrf/VNI command %s\n", data)
			return false
		}
		err = Frr.Save(ctx)
		if err != nil {
			log.Printf("FRR(tearDownSvi): Failed to run save command: %v\n", err)
		}
		log.Printf("FRR: Executed vtysh -c conf t -c router bgp %+v vrf %s -c no  neighbor %s peer-group -c exit\n", localas, path.Base(svi.Spec.Vrf), linkSvi)
		return true
	}
	return true
}

// tearDownVrf tears down vrf
func tearDownVrf(vrf *infradb.Vrf) bool {
	// This function must not be executed for the vrf representing the GRD
	if path.Base(vrf.Name) == "GRD" {
		return true
	}

	data, err := Frr.FrrZebraCmd(ctx, fmt.Sprintf("show vrf %s vni\n", path.Base(vrf.Name)))
	if err != nil || checkFrrResult(data, true) {
		log.Printf("tearDownVrf : failed to run the command")
		log.Printf("FRR: Error  %s\n", data)
		return true
	}
	err = Frr.Save(ctx)
	if err != nil {
		log.Printf("FRR(tearDownVrf): Failed to run save command: %v\n", err)
	}
	// Clean up FRR last
	if !reflect.ValueOf(vrf.Spec.Vni).IsZero() {
		log.Printf("FRR Deleted event")
		delCmd1 := fmt.Sprintf("no router bgp %+v vrf %s", localas, path.Base(vrf.Name))
		delCmd2 := fmt.Sprintf("no vrf %s", path.Base(vrf.Name))
		data, err = Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n %s\n exit\n", delCmd1))
		if strings.Contains(data, "Can't find BGP instance") { // Trying to delete non exist VRF return true
			return true
		}
		if err != nil || checkFrrResult(data, false) {
			log.Printf("FRR: Error  %s\n", data)
			return false
		}
		err = Frr.Save(ctx)
		if err != nil {
			log.Printf("FRR(tearDownVrf): Failed to run save command: %v\n", err)
		}
		data, err = Frr.FrrZebraCmd(ctx, fmt.Sprintf("configure terminal\n %s\n exit\n", delCmd2))
		if err != nil || checkFrrResult(data, false) {
			log.Printf("FRR: Error  %s\n", data)
			return false
		}
		err = Frr.Save(ctx)
		if err != nil {
			log.Printf("FRR(tearDownVrf): Failed to run save command: %v\n", err)
		}
		log.Printf("FRR: Executed vtysh -c conf t -c %s -c %s -c exit\n", delCmd1, delCmd2)
	}
	return true
}

func tearDownTunRep(tun *infradb.TunRep) bool {
	if tun.enable_bgp && tun.Spec.RemoteIp != nil {
		bgpVrfName := fmt.Sprintf("router bgp %+v vrf %s\n", localas, path.Base(tun.Spec.Vrf))
		neighrem := fmt.Sprintf("no neighbor %s\n", tun.Spec.IP)
		data, err := Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n %s %s exit", bgpVrfName, neighrem))
		if err != nil || checkFrrResult(data, false) {
			log.Printf("FRR: Error in conf tun %s %s command %s\n", tun.Name, path.Base(tun.Spec.Vrf), data)
			return false
		}
		err = Frr.Save(ctx)
		if err != nil {
			log.Printf("FRR(setUpTunRep): Failed to run save command: %v\n", err)
		}
		return true
	}
	return true
}
