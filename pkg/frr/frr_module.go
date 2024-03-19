// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

// Package frr handles the frr related functionality
package frr

import (
	"context"
	"encoding/json"
	"fmt"

	// "io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"path"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/opiproject/opi-evpn-bridge/pkg/config"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriberframework/eventbus"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils"
	// "gopkg.in/yaml.v3"
)

// SubscriberConfig structure
type SubscriberConfig struct {
	Name     string   `yaml:"name"`
	Priority int      `yaml:"priority"`
	Events   []string `yaml:"events"`
}

// LinuxFrrConfig structure
type LinuxFrrConfig struct {
	Enable      bool   `yaml:"enabled"`
	DefaultVtep string `yaml:"default_vtep"`
	PortMux     string `yaml:"port_mux"`
	VrfMux      string `yaml:"vrf_mux"`
	BrTenant    int    `yaml:"br_tenant"`
}

// Config structure
type Config struct {
	Subscribers []SubscriberConfig `yaml:"subscribers"`
	LinuxFrr    LinuxFrrConfig     `yaml:"linux_frr"`
}

// frrComp string constant
const frrComp string = "frr"

// ModulefrrHandler empty structure
type ModulefrrHandler struct{}

// HandleEvent handles the events
func (h *ModulefrrHandler) HandleEvent(eventType string, objectData *eventbus.ObjectData) {
	switch eventType {
	case "vrf": // "VRF_added":
		fmt.Printf("FRR recevied %s %s\n", eventType, objectData.Name)
		handlevrf(objectData)
	case "svi":
		fmt.Printf("FRR recevied %s %s\n", eventType, objectData.Name)
		handlesvi(objectData)
	default:
		fmt.Printf("error: Unknown event type %s", eventType)
	}
}

// handlesvi handles the svi functionality
func handlesvi(objectData *eventbus.ObjectData) {
	var comp common.Component
	svi, err := infradb.GetSvi(objectData.Name)
	if err != nil {
		fmt.Printf("GetSvi error: %s %s\n", err, objectData.Name)
		return
	}

	fmt.Printf("FRR :GetSvi Name: %s\n", svi.Name)

	if objectData.ResourceVersion != svi.ResourceVersion {
		fmt.Printf("FRR: Mismatch in resoruce version %+v\n and svi resource version %+v\n", objectData.ResourceVersion, svi.ResourceVersion)
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
		detail, status := setUpSvi(svi)
		comp.Name = frrComp
		if status {
			comp.Details = detail
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
		fmt.Printf("%+v\n", comp)
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
		fmt.Printf("%+v\n", comp)
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
		fmt.Printf("GetVRF error: %s %s\n", err, objectData.Name)
		return
	}

	fmt.Printf("FRR :GetVRF Name: %s\n", vrf.Name)

	if len(vrf.Status.Components) != 0 {
		for i := 0; i < len(vrf.Status.Components); i++ {
			if vrf.Status.Components[i].Name == frrComp {
				comp = vrf.Status.Components[i]
			}
		}
	}
	if objectData.ResourceVersion != vrf.ResourceVersion {
		fmt.Printf("FRR: Mismatch in resoruce version %+v\n and vrf resource version %+v\n", objectData.ResourceVersion, vrf.ResourceVersion)
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
		fmt.Printf("%+v\n", comp)
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
		fmt.Printf("%+v\n", comp)
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
		fmt.Printf("FRR: Command %s': exit code %s;", out, err)
		return "Error", -1
	}
	output := string(out)
	return output, 0
}

var defaultVtep, portMux, vrfMux string

// var brTenant int

// subscribeInfradb function handles the infradb subscriptions
func subscribeInfradb(config *config.Config) {
	eb := eventbus.EBus
	for _, subscriberConfig := range config.Subscribers {
		if subscriberConfig.Name == frrComp {
			for _, eventType := range subscriberConfig.Events {
				eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModulefrrHandler{})
			}
		}
	}
}

// setUpTenantBridge function handles the tanent bridge
func setUpTenantBridge() {
	//	run([]string{"ip","-br","l"},false)
	run([]string{"ip", "link", "add" /*strconv.Itoa(brTenant)*/, "br-tenant", "type", "bridge", "vlan_default_pvid", "0", "vlan_filtering", "1", "vlan_protocol", "802.1Q"}, false)
	//	fmt.Println("Venky ",cp,err)
	run([]string{"ip", "link", "set", "br-tenant" /*"strconv.Itoa(brTenant)",*/, "up"}, false)
	// fmt.Println("Venky1 ",cp,err)
}

// ctx variable of type context
var ctx context.Context

// Frr variable of type utils wrapper
var Frr utils.Frr

// Init function handles init functionality
func Init() {
	/*config, err := readConfig("config.yaml")
	if err != nil {
		fmt.Fatal(err)
		// os.Exit(0)
	}*/
	//	fmt.SetOutput(os.Stdout)

	frrEnabled := config.GlobalConfig.LinuxFrr.Enabled
	if !frrEnabled {
		fmt.Println("FRR Module disabled")
		return
	}
	defaultVtep = config.GlobalConfig.LinuxFrr.DefaultVtep
	// br_tenant = config.GlobalConfig.LinuxFrr.Br_tenant
	portMux = config.GlobalConfig.LinuxFrr.PortMux
	vrfMux = config.GlobalConfig.LinuxFrr.VrfMux
	log.Printf(" frr vtep %+v \n", defaultVtep)
	// Subscribe to InfraDB notifications
	subscribeInfradb(&config.GlobalConfig)
	// Set up the static configuration parts
	setUpTenantBridge()

	ctx = context.Background()
	Frr = utils.NewFrrWrapper()

	// Make sure IPv4 forwarding is enabled.
	_, _ = run([]string{"sysctl", "-w", " net.ipv4.ip_forward=1"}, false)
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
	Ifname := strings.Split(vrf.Name, "/")
	ifwlen := len(Ifname)
	vrf.Name = Ifname[ifwlen-1]
	if vrf.Name == "GRD" {
		return "", true
	}
	if !reflect.ValueOf(vrf.Spec.Vni).IsZero() {
		// Configure the vrf in FRR and set up BGP EVPN for it
		vrfName := fmt.Sprintf("vrf %s", vrf.Name)
		vniID := fmt.Sprintf("vni %s", strconv.Itoa(int(*vrf.Spec.Vni)))
		_, err := Frr.FrrZebraCmd(ctx, fmt.Sprintf("configure terminal\n %s\n %s\n exit-vrf\n exit", vrfName, vniID))
		// fmt.Printf("FrrZebraCmd: %v:%v", data, err)
		if err != nil {
			return "", false
		}
		fmt.Printf("FRR: Executed frr config t %s %s exit-vrf exit\n", vrfName, vniID)
		var LbiP string

		if reflect.ValueOf(vrf.Spec.LoopbackIP).IsZero() {
			LbiP = "0.0.0.0"
		} else {
			LbiP = fmt.Sprintf("%+v", vrf.Spec.LoopbackIP.IP)
		}
		_, err = Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n router bgp 65000 vrf %s\n bgp router-id %s\n no bgp ebgp-requires-policy\n no bgp hard-administrative-reset\n no bgp graceful-restart notification\n address-family ipv4 unicast\n redistribute connected\n redistribute static\n exit-address-family\n address-family l2vpn evpn\n advertise ipv4 unicast\n exit-address-family\n exit", vrf.Name, LbiP))
		if err != nil {
			return "", false
		}

		fmt.Println("FRR: Executed config t bgpVrfName router bgp 65000 vrf", vrf.Name, "bgp_route_id", LbiP, " no bgp ebgp-requires-policy exit-vrf exit")
		// Update the vrf with attributes from FRR
		cmd := fmt.Sprintf("show bgp l2vpn evpn vni %d json", *vrf.Spec.Vni)
		cp, err := Frr.FrrBgpCmd(ctx, cmd)
		if err != nil {
			fmt.Println("error-", err)
		}
		hname, _ := os.Hostname()
		L2vpnCmd := strings.Split(cp, "json")
		L2vpnCmd = strings.Split(L2vpnCmd[1], hname)
		cp = L2vpnCmd[0]
		// fmt.Printf("FRR_L2vpn[0]: %s\n",cp)
		if len(cp) != 7 {
			cp = cp[3 : len(cp)-3]
		} else {
			fmt.Printf("FRR: unable to get the command %s\n", cmd)
			return "", false
		}
		var bgpL2vpn BgpL2vpnCmd
		err1 := json.Unmarshal([]byte(fmt.Sprintf("{%v}", cp)), &bgpL2vpn)
		if err1 != nil {
			fmt.Println("error-", err)
		}
		cmd = fmt.Sprintf("show bgp vrf %s json", vrf.Name)
		cp, err = Frr.FrrBgpCmd(ctx, cmd)
		if err != nil {
			fmt.Println("error-", err)
		}
		BgpCmd := strings.Split(cp, "json")
		BgpCmd = strings.Split(BgpCmd[1], hname)
		cp = BgpCmd[0]

		var bgpVrf BgpVrfCmd
		if len(cp) != 7 {
			cp = cp[5 : len(cp)-5]
		} else {
			fmt.Printf("FRR: unable to get the command \"%s\"\n", cmd)
			return "", false
		}
		err1 = json.Unmarshal([]byte(fmt.Sprintf("{%v}", cp)), &bgpVrf)
		if err1 != nil {
			fmt.Println("error-", err)
		}
		fmt.Printf("FRR: Executed show bgp vrf %s json\n", vrf.Name)
		details := fmt.Sprintf("{ \"rd\":\"%s\",\"rmac\":\"%s\",\"importRts\":[\"%s\"],\"exportRts\":[\"%s\"],\"localAS\":%d }", bgpL2vpn.Rd, bgpL2vpn.Rmac, bgpL2vpn.ImportRts, bgpL2vpn.ExportRts, bgpVrf.LocalAS)
		fmt.Printf("FRR Details %s\n", details)
		return details, true
	}
	return "", true
}

// checkFrrResult checks the vrf result
func checkFrrResult(cp string, show bool) bool {
	return ((show && reflect.ValueOf(cp).IsZero()) || strings.Contains(cp, "warning") || strings.Contains(cp, "unknown") || strings.Contains(cp, "Unknown") || strings.Contains(cp, "Warning") || strings.Contains(cp, "Ambiguous") || strings.Contains(cp, "specified does not exist"))
}

// setUpSvi sets up the svi
func setUpSvi(svi *infradb.Svi) (string, bool) {
	linkSvi := fmt.Sprintf("%+v-%+v", path.Base(svi.Spec.Vrf), strings.Split(path.Base(svi.Spec.LogicalBridge), "vlan")[1])
	if svi.Spec.EnableBgp && !reflect.ValueOf(svi.Spec.GatewayIPs).IsZero() {
		// gwIP := fmt.Sprintf("%s", svi.Spec.GatewayIPs[0].IP.To4())
		gwIP := string(svi.Spec.GatewayIPs[0].IP.To4())
		RemoteAs := fmt.Sprintf("%d", *svi.Spec.RemoteAs)
		bgpVrfName := fmt.Sprintf("router bgp 65000 vrf %s\n", path.Base(svi.Spec.Vrf))
		neighlink := fmt.Sprintf("neighbor %s peer-group\n", linkSvi)
		neighlinkRe := fmt.Sprintf("neighbor %s remote-as %s\n", linkSvi, RemoteAs)
		neighlinkGw := fmt.Sprintf("neighbor %s update-source %s\n", linkSvi, gwIP)
		neighlinkOv := fmt.Sprintf("neighbor %s as-override\n", linkSvi)
		neighlinkSr := fmt.Sprintf("neighbor %s soft-reconfiguration inbound\n", linkSvi)
		bgpListen := fmt.Sprintf(" bgp listen range %s peer-group %s\n", svi.Spec.GatewayIPs[0], linkSvi)

		data, err := Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal %s bgp disable-ebgp-connected-route-check\n %s %s %s %s %s %s exit", bgpVrfName, neighlink, neighlinkRe, neighlinkGw, neighlinkOv, neighlinkSr, bgpListen))

		if err != nil || checkFrrResult(data, false) {
			fmt.Printf("FRR: Error in conf svi %s %s command %s\n", svi.Name, path.Base(svi.Spec.Vrf), data)
			return "", false
		}
		return "", true
	}
	return "", true
}

// tearDownSvi tears down svi
func tearDownSvi(svi *infradb.Svi) bool {
	linkSvi := fmt.Sprintf("%+v-%+v", path.Base(svi.Spec.Vrf), strings.Split(path.Base(svi.Spec.LogicalBridge), "vlan")[1])
	if svi.Spec.EnableBgp && !reflect.ValueOf(svi.Spec.GatewayIPs).IsZero() {
		bgpVrfName := fmt.Sprintf("router bgp 65000 vrf %s", path.Base(svi.Spec.Vrf))
		noNeigh := fmt.Sprintf("no neighbor %s peer-group", linkSvi)
		data, err := Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n %s\n %s\n exit", bgpVrfName, noNeigh))
		if err != nil || checkFrrResult(data, false) {
			fmt.Printf("FRR: Error in conf Delete vrf/VNI command %s\n", data)
			return false
		}
		fmt.Printf("FRR: Executed vtysh -c conf t -c router bgp 65000 vrf %s -c no  neighbor %s peer-group -c exit\n", path.Base(svi.Spec.Vrf), linkSvi)
		return true
	}
	return true
}

// tearDownVrf tears down vrf
func tearDownVrf(vrf *infradb.Vrf) bool { // interface{}){
	// This function must not be executed for the vrf representing the GRD
	Ifname := strings.Split(vrf.Name, "/")
	ifwlen := len(Ifname)
	vrf.Name = Ifname[ifwlen-1]
	if vrf.Name == "GRD" {
		return true
	}

	data, err := Frr.FrrZebraCmd(ctx, fmt.Sprintf("show vrf %s vni\n", vrf.Name))
	if err != nil {
		fmt.Printf("tearDownVrf : failed to run the command")
	}
	if checkFrrResult(data, true) {
		fmt.Printf("CP FRR %s\n", data)
		return true
	}
	// Clean up FRR last
	if !reflect.ValueOf(*vrf.Spec.Vni).IsZero() {
		fmt.Println("FRR Deleted event")
		delCmd1 := fmt.Sprintf("no router bgp 65000 vrf %s", vrf.Name)
		delCmd2 := fmt.Sprintf("no vrf %s", vrf.Name)
		_, err = Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n %s\n exit\n", delCmd1))
		if err != nil {
			return false
		}
		_, err = Frr.FrrZebraCmd(ctx, fmt.Sprintf("configure terminal\n %s\n exit\n", delCmd2))
		if err != nil {
			return false
		}
		fmt.Printf("FRR: Executed vtysh -c conf t -c %s -c %s -c exit\n", delCmd1, delCmd2)
	}
	return true
}
