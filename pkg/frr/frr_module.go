package frr

// package main // frr_module

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"time"
	"context"

	"github.com/opiproject/opi-evpn-bridge/pkg/utils"
	"github.com/opiproject/opi-evpn-bridge/pkg/config"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
	"gopkg.in/yaml.v3"
	"path"
)

type SubscriberConfig struct {
	Name     string   `yaml:"name"`
	Priority int      `yaml:"priority"`
	Events   []string `yaml:"events"`
}

/*
	type Config_t struct {
		P4 struct {
			Enable bool `yaml:"enabled"`
		} `yaml: "p4"`
		Frr_module struct {
			Enable       bool   `yaml:"enabled"`
			Default_vtep string `yaml:"default_vtep"`
			Port_mux     string `yaml:"port_mux"`
			Vrf_mux      string `yaml:"vrf_mux"`
			Br_tenant    int    `yaml:"br_tenant"`
		} `yaml:"linux_frr"`
		Netlink struct {
			Enable        bool `yaml:"enabled"`
			Poll_interval int  `yaml:"poll_interval"`
			Phy_ports     []struct {
				Name string `yaml:"name"`
				Vsi  int    `yaml:"vsi"`
			} `yaml:"phy_ports"`
		} `yaml:"netlink"`
	}
*/
type Linux_frrConfig struct {
	Enable       bool   `yaml:"enabled"`
	Default_vtep string `yaml:"default_vtep"`
	Port_mux     string `yaml:"port_mux"`
	Vrf_mux      string `yaml:"vrf_mux"`
	Br_tenant    int    `yaml:"br_tenant"`
}

type Config struct {
	Subscribers []SubscriberConfig `yaml:"subscribers"`
	Linux_frr   Linux_frrConfig    `yaml:"linux_frr"`
}

type ModulefrrHandler struct{}

func (h *ModulefrrHandler) HandleEvent(eventType string, objectData *event_bus.ObjectData) {
	switch eventType {
	case "vrf": // "VRF_added":
		fmt.Printf("FRR recevied %s %s\n", eventType, objectData.Name)
		handlevrf(objectData)
	case "svi":
		fmt.Printf("FRR recevied %s %s\n", eventType, objectData.Name)
		handlesvi(objectData)
	default:
		fmt.Println("error: Unknown event type %s", eventType)
	}
}

func handlesvi(objectData *event_bus.ObjectData) {
	var comp common.Component
	SVI, err := infradb.GetSvi(objectData.Name)
	if err != nil {
		fmt.Printf("GetSvi error: %s %s\n", err, objectData.Name)
		return
	} else {
		fmt.Printf("FRR :GetSvi Name: %s\n", SVI.Name)
	}
	if (objectData.ResourceVersion != SVI.ResourceVersion){
		fmt.Printf("FRR: Mismatch in resoruce version %+v\n and SVI resource version %+v\n", objectData.ResourceVersion, SVI.ResourceVersion)
		comp.Name = "frr"
		comp.CompStatus= common.COMP_STATUS_ERROR
		if comp.Timer ==0 {
			comp.Timer=2 * time.Second
		} else {
			comp.Timer=comp.Timer*2
		}
		infradb.UpdateSviStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationId,nil,comp)
		return
	}
	if len(SVI.Status.Components) != 0 {
		for i := 0; i < len(SVI.Status.Components); i++ {
			if SVI.Status.Components[i].Name == "frr" {
				comp = SVI.Status.Components[i]
			}
		}
	}
	if SVI.Status.SviOperStatus != infradb.SVI_OPER_STATUS_TO_BE_DELETED {
		detail, status := set_up_svi(SVI)
		comp.Name = "frr"
		if status == true {
			comp.Details = detail
			comp.CompStatus = common.COMP_STATUS_SUCCESS
			comp.Timer = 0
		} else {
			if comp.Timer == 0 {
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
			comp.CompStatus = common.COMP_STATUS_ERROR
		}
		fmt.Printf("%+v\n", comp)
		infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
	} else {
		status := tear_down_svi(SVI)
		comp.Name = "frr"
		if status == true {
			comp.CompStatus = common.COMP_STATUS_SUCCESS
			comp.Timer = 0
		} else {
			if comp.Timer == 0 {
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
			comp.CompStatus = common.COMP_STATUS_ERROR
		}
		fmt.Printf("%+v\n", comp)
		infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
	}
}

func handlevrf(objectData *event_bus.ObjectData) {
	var comp common.Component
	VRF, err := infradb.GetVrf(objectData.Name)
	if err != nil {
		fmt.Printf("GetVRF error: %s %s\n", err, objectData.Name)
		return
	} else {
		fmt.Printf("FRR :GetVRF Name: %s\n", VRF.Name)
	}
	if len(VRF.Status.Components) != 0 {
                for i := 0; i < len(VRF.Status.Components); i++ {
                        if VRF.Status.Components[i].Name == "frr" {
                                comp = VRF.Status.Components[i]
                        }
                }
        }
	if objectData.ResourceVersion != VRF.ResourceVersion {
		fmt.Printf("FRR: Mismatch in resoruce version %+v\n and VRF resource version %+v\n", objectData.ResourceVersion, VRF.ResourceVersion)
		comp.Name = "frr"
		comp.CompStatus = common.COMP_STATUS_ERROR
		if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
			comp.Timer = 2 * time.Second
		} else {
			comp.Timer = comp.Timer * 2
		}
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
		return
	}
	if VRF.Status.VrfOperStatus != infradb.VRF_OPER_STATUS_TO_BE_DELETED {
		detail, status := set_up_vrf(VRF)
		comp.Name = "frr"
		if status == true {
			comp.Details = detail
			comp.CompStatus = common.COMP_STATUS_SUCCESS
			comp.Timer = 0
		} else {
			if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
			comp.CompStatus = common.COMP_STATUS_ERROR
		}
		fmt.Printf("%+v\n", comp)
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
	} else {
		status := tear_down_vrf(VRF)
		comp.Name = "frr"
		if status == true {
			comp.CompStatus = common.COMP_STATUS_SUCCESS
			comp.Timer = 0
		} else {
			if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
			comp.CompStatus = common.COMP_STATUS_ERROR
		}
		fmt.Printf("%+v\n", comp)
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
	}
}

func run(cmd []string, flag bool) (string, int) {
	//  fmt.Println("FRR: Executing command", cmd)
	var out []byte
	var err error
	//  out, err = exec.Command("sudo",cmd...).Output()
	out, err = exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		if flag == true {
			panic(fmt.Sprintf("FRR: Command %s': exit code %s;", out, err.Error()))
		}
		fmt.Printf("FRR: Command %s': exit code %s;", out, err)
		return "Error", -1
	}
	output := string(out)
	return output, 0
}

func readConfig(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}

var logger, default_vtep, port_mux, vrf_mux string
var br_tenant int

func subscribe_infradb(config *config.Config) {
	eb := event_bus.EBus
	for _, subscriberConfig := range config.Subscribers {
		if subscriberConfig.Name == "frr" {
			for _, eventType := range subscriberConfig.Events {
				eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModulefrrHandler{})
			}
		}
	}
}

func set_up_tenant_bridge() {
	//	run([]string{"ip","-br","l"},false)
	run([]string{"ip", "link", "add" /*strconv.Itoa(br_tenant)*/, "br-tenant", "type", "bridge", "vlan_default_pvid", "0", "vlan_filtering", "1", "vlan_protocol", "802.1Q"}, false)
	//	fmt.Println("Venky ",CP,err)
	run([]string{"ip", "link", "set", "br-tenant" /*"strconv.Itoa(br_tenant)",*/, "up"}, false)
	// fmt.Println("Venky1 ",CP,err)
}

var ctx context.Context
var Frr utils.Frr
const logfile string ="./frr.log"
// func main(){
func Init() {
	/*config, err := readConfig("config.yaml")
	if err != nil {
		fmt.Fatal(err)
		// os.Exit(0)
	}*/
//	fmt.SetOutput(os.Stdout)
        logFile, err := os.OpenFile(logfile, os.O_APPEND|os.O_RDWR|os.O_CREATE, 0644)
        if err != nil {
                log.Panic(err)
        }
        defer logFile.Close()
        log.SetOutput(logFile)
        log.SetFlags(log.Lshortfile | log.LstdFlags)
	frr_enabled := config.GlobalConfig.Linux_frr.Enabled
	if frr_enabled != true {
		fmt.Println("FRR Module disabled")
		return
	}
	default_vtep = config.GlobalConfig.Linux_frr.Default_vtep
	// br_tenant = config.GlobalConfig.Linux_frr.Br_tenant
	port_mux = config.GlobalConfig.Linux_frr.Port_mux
	vrf_mux = config.GlobalConfig.Linux_frr.Vrf_mux
	// Subscribe to InfraDB notifications
	subscribe_infradb(&config.GlobalConfig)
	// Set up the static configuration parts
	set_up_tenant_bridge()

	ctx = context.Background()
        Frr = utils.NewFrrWrapper()

	// Make sure IPv4 forwarding is enabled.
	run([]string{"sysctl", "-w", " net.ipv4.ip_forward=1"}, false)
}

func routing_table_busy(table uint32) bool {
	CP, err := run([]string{"ip", "route", "show", "table", strconv.Itoa(int(table))}, false)
	if err != 0 {
		fmt.Println(CP)
		return false
	}
	// fmt.Printf("route table busy %s %s\n",CP,err)
	// Table is busy if it exists and contains some routes
	return true // reflect.ValueOf(CP).IsZero() && len(CP)!= 0
}

type VRF struct {
	Name           string
	Vni            int
	Routing_tables []uint32
	Loopback       net.IP
	// Routing_tables uint32
}

type Bgp_l2vpn_cmd struct {
	Vni                   int
	Type                  string
	InKernel              string
	Rd                    string
	OriginatorIp          string
	AdvertiseGatewayMacip string
	AdvertiseSviMacIp     string
	AdvertisePip          string
	SysIP                 string
	SysMac                string
	Rmac                  string
	ImportRts             []string
	ExportRts             []string
}
type route struct{}
type Bgp_vrf_cmd struct {
	VrfId         int
	VrfName       string
	TableVersion  uint
	RouterId      string
	DefaultLocPrf uint
	LocalAS       int
	Routes        route
}

func set_up_vrf(VRF *infradb.Vrf) (string, bool) {
	// This function must not be executed for the VRF representing the GRD
	Ifname := strings.Split(VRF.Name, "/")
	ifwlen := len(Ifname)
	VRF.Name = Ifname[ifwlen-1]
	if VRF.Name == "GRD" {
		return "", true
	}
	if !reflect.ValueOf(VRF.Spec.Vni).IsZero() {
		// Configure the VRF in FRR and set up BGP EVPN for it
		vrf_name := fmt.Sprintf("vrf %s", VRF.Name)
		vni_id := fmt.Sprintf("vni %s", strconv.Itoa(int(*VRF.Spec.Vni)))
		_, err := Frr.FrrZebraCmd(ctx, fmt.Sprintf("configure terminal\n %s\n %s\n exit-vrf\n exit", vrf_name, vni_id))
		//fmt.Printf("FrrZebraCmd: %v:%v", data, err)
                if err != nil {
                        return "",false
                }
		fmt.Printf("FRR: Executed frr config t %s %s exit-vrf exit\n", vrf_name, vni_id)
                var LbiP string

		if reflect.ValueOf(VRF.Spec.LoopbackIP).IsZero() {
			LbiP = "0.0.0.0"
		} else {
			LbiP = fmt.Sprintf("%+v", VRF.Spec.LoopbackIP.IP)
		}
                _,err = Frr.FrrBgpCmd(ctx,fmt.Sprintf("configure terminal\n router bgp 65000 vrf %s\n bgp router-id %s\n no bgp ebgp-requires-policy\n no bgp hard-administrative-reset\n no bgp graceful-restart notification\n address-family ipv4 unicast\n redistribute connected\n redistribute static\n exit-address-family\n address-family l2vpn evpn\n advertise ipv4 unicast\n exit-address-family\n exit",VRF.Name,LbiP))
                if err != nil {
                        return "",false
                }

		fmt.Println("FRR: Executed config t bgp_vrf_name router bgp 65000 vrf",VRF.Name, "bgp_route_id",LbiP," no bgp ebgp-requires-policy exit-vrf exit")
		// Update the VRF with attributes from FRR
		cmd := fmt.Sprintf("show bgp l2vpn evpn vni %d json", *VRF.Spec.Vni)
		CP,err := Frr.FrrBgpCmd(ctx, cmd)
                if err != nil {
                        fmt.Println("error-", err)
                }
		hname,_ := os.Hostname()
		L2vpn_cmd := strings.Split(CP, "json")
		L2vpn_cmd = strings.Split(L2vpn_cmd[1], hname)
		CP = L2vpn_cmd[0]
		//fmt.Printf("FRR_L2vpn[0]: %s\n",CP)
		if len(CP) != 7 {
			CP = CP[3 : len(CP)-3]
		} else {
			fmt.Printf("FRR: unable to get the command %s\n", cmd)
			return "", false
		}
		var bgp_l2vpn Bgp_l2vpn_cmd
		err1 := json.Unmarshal([]byte(fmt.Sprintf("{%v}", CP)), &bgp_l2vpn)
		if err1 != nil {
			fmt.Println("error-", err)
		}
		cmd = fmt.Sprintf("show bgp vrf %s json", VRF.Name)
		CP,err = Frr.FrrBgpCmd(ctx, cmd)
		if err != nil {
                        fmt.Println("error-", err)
                }
		Bgp_cmd := strings.Split(CP, "json")
		Bgp_cmd = strings.Split(Bgp_cmd[1], hname)
		CP = Bgp_cmd[0]

		var bgp_vrf Bgp_vrf_cmd
		if len(CP) != 7 {
			CP = CP[5 : len(CP)-5]
		} else {
			fmt.Printf("FRR: unable to get the command \"%s\"\n", cmd)
			return "", false
		}
		err1 = json.Unmarshal([]byte(fmt.Sprintf("{%v}", CP)), &bgp_vrf)
		if err1 != nil {
			fmt.Println("error-", err)
		}
		fmt.Printf("FRR: Executed show bgp vrf %s json\n", VRF.Name)
		details := fmt.Sprintf("{ \"rd\":\"%s\",\"rmac\":\"%s\",\"importRts\":[\"%s\"],\"exportRts\":[\"%s\"],\"localAS\":%d }", bgp_l2vpn.Rd, bgp_l2vpn.Rmac, bgp_l2vpn.ImportRts, bgp_l2vpn.ExportRts, bgp_vrf.LocalAS)
		fmt.Printf("FRR Details %s\n", details)
		return details, true
	}
	return "", false
}

func check_frr_result(CP string, show bool) bool {
	return ((show && reflect.ValueOf(CP).IsZero()) || strings.Contains(CP, "warning") || strings.Contains(CP, "unknown") || strings.Contains(CP, "Unknown") || strings.Contains(CP, "Warning") || strings.Contains(CP, "Ambiguous") || strings.Contains(CP, "specified does not exist"))
}

func set_up_svi(SVI *infradb.Svi) (string, bool) {
	link_svi := fmt.Sprintf("%+v-%+v", path.Base(SVI.Spec.Vrf),strings.Split(path.Base(SVI.Spec.LogicalBridge),"vlan")[1])
	if SVI.Spec.EnableBgp && !reflect.ValueOf(SVI.Spec.GatewayIPs).IsZero() {
		gw_ip := fmt.Sprintf("%s", SVI.Spec.GatewayIPs[0].IP.To4())
		RemoteAs := fmt.Sprintf("%d", *SVI.Spec.RemoteAs)
		bgp_vrf_name := fmt.Sprintf("router bgp 65000 vrf %s\n", path.Base(SVI.Spec.Vrf))
		neighlink := fmt.Sprintf("neighbor %s peer-group\n", link_svi)
		neighlink_Re := fmt.Sprintf("neighbor %s remote-as %s\n",link_svi, RemoteAs)
		neighlink_gw := fmt.Sprintf("neighbor %s update-source %s\n",link_svi, gw_ip)
		neighlink_ov := fmt.Sprintf("neighbor %s as-override\n", link_svi)
		neighlink_sr := fmt.Sprintf("neighbor %s soft-reconfiguration inbound\n", link_svi)
		bgp_listen := fmt.Sprintf(" bgp listen range %s peer-group %s\n",SVI.Spec.GatewayIPs[0], link_svi)
	
		data, err := Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal %s bgp disable-ebgp-connected-route-check\n %s %s %s %s %s %s exit",bgp_vrf_name,neighlink,neighlink_Re,neighlink_gw,neighlink_ov,neighlink_sr,bgp_listen))

		if err != nil || check_frr_result(data, false) {
			fmt.Printf("FRR: Error in conf SVI %s %s command %s\n", SVI.Name, path.Base(SVI.Spec.Vrf), data)
			return "", false
		}
		return "", true
	}
	return "", false
}

func tear_down_svi(SVI *infradb.Svi) bool {
	link_svi := fmt.Sprintf("%+v-%+v", path.Base(SVI.Spec.Vrf),strings.Split(path.Base(SVI.Spec.LogicalBridge),"vlan")[1])
	if SVI.Spec.EnableBgp && !reflect.ValueOf(SVI.Spec.GatewayIPs).IsZero() {
		bgp_vrf_name := fmt.Sprintf("router bgp 65000 vrf %s", path.Base(SVI.Spec.Vrf))
		no_neigh := fmt.Sprintf("no neighbor %s peer-group", link_svi)
		data, err := Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n %s\n %s\n exit",bgp_vrf_name,no_neigh))
		if err != nil || check_frr_result(data, false) {
			fmt.Printf("FRR: Error in conf Delete VRF/VNI command %s\n", data)
			return false
		}
		fmt.Printf("FRR: Executed vtysh -c conf t -c router bgp 65000 vrf %s -c no  neighbor %s peer-group -c exit\n", path.Base(SVI.Spec.Vrf), link_svi)
		return true
	}
	return false
}


func tear_down_vrf(VRF *infradb.Vrf) bool { // interface{}){
	// This function must not be executed for the VRF representing the GRD
	Ifname := strings.Split(VRF.Name, "/")
	ifwlen := len(Ifname)
	VRF.Name = Ifname[ifwlen-1]
	if VRF.Name == "GRD" {
		return true
	}

	data, err := Frr.FrrZebraCmd(ctx, fmt.Sprintf("show vrf %s vni\n",VRF.Name))
	if check_frr_result(data, true) {
			fmt.Printf("CP FRR %s\n", data)
			return true
		}
	// Clean up FRR last
	if !reflect.ValueOf(*VRF.Spec.Vni).IsZero() {
		fmt.Println("FRR Deleted event")
		del_cmd1 := fmt.Sprintf("no router bgp 65000 vrf %s", VRF.Name)
		del_cmd2 := fmt.Sprintf("no vrf %s", VRF.Name)
		data, err = Frr.FrrBgpCmd(ctx, fmt.Sprintf("configure terminal\n %s\n exit\n",del_cmd1))
	        if err != nil {
        	     return false
	        }
		data, err = Frr.FrrZebraCmd(ctx, fmt.Sprintf("configure terminal\n %s\n exit\n", del_cmd2))
	        if err != nil {
        	     return false
	        }
		fmt.Printf("FRR: Executed vtysh -c conf t -c %s -c %s -c exit\n", del_cmd1, del_cmd2)
	}
	return true
}
