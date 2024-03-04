package LinuxGeneralModule

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/opiproject/opi-evpn-bridge/pkg/config"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriberframework/eventbus"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"path"
)

type ModulelgmHandler struct{}

const RoutingTableMax = 4000
const RoutingTableMin = 1000

// range specification, note that min <= max
func Generate_route_table() uint32 {
	return uint32(rand.Intn(RoutingTableMax-RoutingTableMin+1) + RoutingTableMin)
}

func run(cmd []string, flag bool) (string, int) {
	var out []byte
	var err error
	out, err = exec.Command(cmd[0], cmd[1:]...).CombinedOutput()
	if err != nil {
		if flag {
			panic(fmt.Sprintf("LGM: Command %s': exit code %s;", out, err.Error()))
		}
		log.Println("LGM: Command %s': exit code %s;\n", out, err)
		return "Error", -1
	}
	output := string(out)
	return output, 0
}

func (h *ModulelgmHandler) HandleEvent(eventType string, objectData *eventbus.ObjectData) {
	switch eventType {
	case "vrf":
		log.Println("LGM recevied %s %s\n", eventType, objectData.Name)
		handlevrf(objectData)
	case "svi":
		log.Println("LGM recevied %s %s\n", eventType, objectData.Name)
		handlesvi(objectData)
	case "logical-bridge":
		log.Println("LGM recevied %s %s\n", eventType, objectData.Name)
		handleLB(objectData)
	default:
		log.Println("LGM: error: Unknown event type %s", eventType)
	}
}

func handleLB(objectData *eventbus.ObjectData) {
	var comp common.Component
	LB, err := infradb.GetLB(objectData.Name)
	if err != nil {
		log.Println("LGM: GetLB error: %s %s\n", err, objectData.Name)
		return
	} else {
		log.Println("LGM : GetLB Name: %s\n", LB.Name)
	}
	if len(LB.Status.Components) != 0 {
		for i := 0; i < len(LB.Status.Components); i++ {
			if LB.Status.Components[i].Name == "lgm" {
				comp = LB.Status.Components[i]
			}
		}
	}
	if LB.Status.LBOperStatus != infradb.LogicalBridgeOperStatusToBeDeleted {
		status := set_up_bridge(LB)
		comp.Name = "lgm"
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
		log.Println("LGM: %+v \n", comp)
		infradb.UpdateLBStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
	} else {
		status := tear_down_bridge(LB)
		comp.Name = "lgm"
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
		log.Println("LGM: %+v\n", comp)
		infradb.UpdateLBStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
	}
}

func handlesvi(objectData *eventbus.ObjectData) {
	var comp common.Component
	SVI, err := infradb.GetSvi(objectData.Name)
	if err != nil {
		log.Println("LGM: GetSvi error: %s %s\n", err, objectData.Name)
		return
	} else {
		log.Println("LGM : GetSvi Name: %s\n", SVI.Name)
	}
	if objectData.ResourceVersion != SVI.ResourceVersion {
		log.Println("LGM: Mismatch in resoruce version %+v\n and SVI resource version %+v\n", objectData.ResourceVersion, SVI.ResourceVersion)
		comp.Name = "lgm"
		comp.CompStatus = common.ComponentStatusError
		if comp.Timer == 0 {
			comp.Timer = 2 * time.Second
		} else {
			comp.Timer = comp.Timer * 2
		}
		infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
		return
	}
	if len(SVI.Status.Components) != 0 {
		for i := 0; i < len(SVI.Status.Components); i++ {
			if SVI.Status.Components[i].Name == "lgm" {
				comp = SVI.Status.Components[i]
			}
		}
	}
	if SVI.Status.SviOperStatus != infradb.SviOperStatusToBeDeleted {
		details, status := set_up_svi(SVI)
		comp.Name = "lgm"
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
		log.Println("LGM: %+v \n", comp)
		infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
	} else {
		status := tear_down_svi(SVI)
		comp.Name = "lgm"
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
		log.Println("LGM: %+v \n", comp)
		infradb.UpdateSviStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
	}
}

func handlevrf(objectData *eventbus.ObjectData) {
	var comp common.Component
	VRF, err := infradb.GetVrf(objectData.Name)
	if err != nil {
		log.Println("LGM: GetVRF error: %s %s\n", err, objectData.Name)
		return
	} else {
		log.Println("LGM : GetVRF Name: %s\n", VRF.Name)
	}
	if objectData.ResourceVersion != VRF.ResourceVersion {
		log.Println("LGM: Mismatch in resoruce version %+v\n and VRF resource version %+v\n", objectData.ResourceVersion, VRF.ResourceVersion)
		comp.Name = "lgm"
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
			if VRF.Status.Components[i].Name == "lgm" {
				comp = VRF.Status.Components[i]
			}
		}
	}
	if VRF.Status.VrfOperStatus != infradb.VrfOperStatusToBeDeleted {
		details, status := set_up_vrf(VRF)
		comp.Name = "lgm"
		if status == true {
			comp.Details = details
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
			comp.CompStatus = common.ComponentStatusError
		}
		log.Println("LGM: %+v \n", comp)
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, VRF.Metadata, comp)
	} else {
		status := tear_down_vrf(VRF)
		comp.Name = "lgm"
		if status == true {
			comp.CompStatus = common.ComponentStatusSuccess
			comp.Timer = 0
		} else {
			comp.CompStatus = common.ComponentStatusError
			if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
		}
		log.Println("LGM: %+v\n", comp)
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationID, nil, comp)
	}
}

/*func readConfig(filename string) (*Config, error) {
	data, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}

	var config Config
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, err
	}

	return &config, nil
}*/

var default_vtep string
var ip_mtu int
var br_tenant string
var ctx context.Context
var nlink utils.Netlink

const logfile string = "./gen_linux.log"

func Init() {
	/*config, err := readConfig("config.yaml")
	if err != nil {
		log.Fatal(err)
	}*/
	// open log file
	logFile, err := os.OpenFile(logfile, os.O_APPEND|os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		log.Panic(err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)
	log.SetFlags(log.Lshortfile | log.LstdFlags)
	eb := eventbus.EBus
	for _, subscriberConfig := range config.GlobalConfig.Subscribers {
		if subscriberConfig.Name == "lgm" {
			for _, eventType := range subscriberConfig.Events {
				eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModulelgmHandler{})
			}
		}
	}
	br_tenant = "br-tenant"
	default_vtep = config.GlobalConfig.Linux_frr.Default_vtep
	ip_mtu = config.GlobalConfig.Linux_frr.Ip_mtu
	ctx = context.Background()
	nlink = utils.NewNetlinkWrapper()
}

func routing_table_busy(table uint32) bool {
	_, err := nlink.RouteListFiltered(ctx, netlink.FAMILY_V4, &netlink.Route{Table: int(table)}, netlink.RT_FILTER_TABLE)
	return err == nil
}

func set_up_bridge(LB *infradb.LogicalBridge) bool {
	link := fmt.Sprintf("vxlan-%+v", LB.Spec.VlanID)
	if !reflect.ValueOf(LB.Spec.Vni).IsZero() {
		//Vni := fmt.Sprintf("%+v", *LB.Spec.Vni)
		//VtepIP := fmt.Sprintf("%+v", LB.Spec.VtepIP.IP)
		//Vlanid := fmt.Sprintf("%+v", LB.Spec.VlanId)
		//ip_mtu := fmt.Sprintf("%+v", ip_mtu)
		brIntf, err := nlink.LinkByName(ctx, br_tenant)
		if err != nil {
			log.Println("LGM: Failed to get link information for %s: %v\n", br_tenant, err)
			return false
		}
		vxlan := &netlink.Vxlan{LinkAttrs: netlink.LinkAttrs{Name: link}, VxlanId: int(*LB.Spec.Vni), Port: 4789, Learning: false, SrcAddr: LB.Spec.VtepIP.IP}
		if err := nlink.LinkAdd(ctx, vxlan); err != nil {
			log.Println("LGM: Failed to create Vxlan linki %s: %v\n", link, err)
			return false
		}
		// Example: ip link set vxlan-<LB-vlan-id> master br-tenant addrgenmode none
		if err = nlink.LinkSetMaster(ctx, vxlan, brIntf); err != nil {
			log.Println("LGM: Failed to add Vxlan %s to bridge %s: %v\n", link, br_tenant, err)
			return false
		}
		// Example: ip link set vxlan-<LB-vlan-id> up
		if err = nlink.LinkSetUp(ctx, vxlan); err != nil {
			log.Println("LGM: Failed to up Vxlan link %s: %v\n", link, err)
			return false
		}
		// Example: bridge vlan add dev vxlan-<LB-vlan-id> vid <LB-vlan-id> pvid untagged
		if err = nlink.BridgeVlanAdd(ctx, vxlan, uint16(LB.Spec.VlanID), true, true, false, false); err != nil {
			log.Println("LGM: Failed to add vlan to bridge %s: %v\n", br_tenant, err)
			return false
		}
		if err = nlink.LinkSetBrNeighSuppress(ctx, vxlan, true); err != nil {
			log.Println("LGM: Failed to add bridge %s neigh_suppress: %v\n", vxlan, err)
			return false
		}
		/*
			CP, err := run([]string{"ip", "link", "add", link, "type", "vxlan", "id", Vni, "local", VtepIP, "dstport", "4789", "nolearning", "proxy"}, false)
			if err != 0 {
				log.Println("LGM:Error in executing command %s %s\n", "link add ", link)
				log.Println("%s\n", CP)
				return false
			}
			CP, err = run([]string{"ip", "link", "set", link, "master", br_tenant, "up", "mtu", ip_mtu}, false)
			if err != 0 {
				log.Println("LGM:Error in executing command %s %s\n", "link set ", link)
				log.Println("%s\n", CP)
				return false
			}
			CP, err = run([]string{"bridge", "vlan", "add", "dev", link, "vid", Vlanid, "pvid", "untagged"}, false)
			if err != 0 {
				log.Println("LGM:Error in executing command %s %s\n", "bridge vlan add dev", link)
				log.Println("%s\n", CP)
				return false
			}
			CP, err = run([]string{"bridge", "link", "set", "dev", link, "neigh_suppress", "on"}, false)
			if err != 0 {
				log.Println("LGM:Error in executing command %s %s\n", "bridge link set dev link neigh_suppress on", link)
				log.Println("%s\n", CP)
				return false
			}*/
		return true
	}
	return true
}

func set_up_vrf(VRF *infradb.Vrf) (string, bool) {
	Ip_Mtu := fmt.Sprintf("%+v", ip_mtu)
	Ifname := strings.Split(VRF.Name, "/")
	ifwlen := len(Ifname)
	VRF.Name = Ifname[ifwlen-1]
	if VRF.Name == "GRD" {
		VRF.Metadata.RoutingTable = make([]*uint32, 2)
		VRF.Metadata.RoutingTable[0] = new(uint32)
		VRF.Metadata.RoutingTable[1] = new(uint32)
		*VRF.Metadata.RoutingTable[0] = 254
		*VRF.Metadata.RoutingTable[1] = 255
		return "", true
	}
	routing_table := Generate_route_table()
	VRF.Metadata.RoutingTable = make([]*uint32, 1)
	VRF.Metadata.RoutingTable[0] = new(uint32)
	if routing_table_busy(routing_table) {
		log.Println("LGM :Routing table %d is not empty\n", routing_table)
		// return "Error"
	}
	var vtip string
	if !reflect.ValueOf(VRF.Spec.VtepIP).IsZero() {
		vtip = fmt.Sprintf("%+v", VRF.Spec.VtepIP.IP)
		// Verify that the specified VTEP IP exists as local IP
		err := nlink.RouteListIpTable(ctx, vtip)
		// Not found similar API in viswananda library so retain the linux commands as it is .. not able to get the route list exact vtip table local
		if err != true {
			log.Println(" LGM: VTEP IP not found: %+v\n", VRF.Spec.VtepIP)
			return "", false
		}
	} else {
		// Pick the IP of interface default VTEP interface
		// log.Println("LGM: VTEP iP %+v\n",get_ip_address(default_vtep))
		vtip = fmt.Sprintf("%+v", VRF.Spec.VtepIP.IP)
		*VRF.Spec.VtepIP = get_ip_address(default_vtep)
	}
	log.Println("set_up_vrf: %s %d %d\n", vtip, *VRF.Spec.Vni, routing_table)
	// Create the VRF interface for the specified routing table and add loopback address

	link_adderr := nlink.LinkAdd(ctx, &netlink.Vrf{
		LinkAttrs: netlink.LinkAttrs{Name: VRF.Name},
		Table:     routing_table,
	})
	if link_adderr != nil {
		log.Println("LGM: Error in Adding VRF link table %d\n", VRF.Spec.Vni)
		return "", false
	}

	log.Println("LGM: VRF link %s Added with table id %d\n", VRF.Name, VRF.Spec.Vni)

	link, link_err := nlink.LinkByName(ctx, VRF.Name)
	if link_err != nil {
		log.Println("LGM : Link %s not found\n", VRF.Name)
		return "", false
	}

	linkmtu_err := nlink.LinkSetMTU(ctx, link, ip_mtu)
	if linkmtu_err != nil {
		log.Println("LGM : Unable to set MTU to link %s \n", VRF.Name)
		return "", false
	}

	linksetup_err := nlink.LinkSetUp(ctx, link)
	if linksetup_err != nil {
		log.Println("LGM : Unable to set link %s UP \n", VRF.Name)
		return "", false
	}
	Lbip := fmt.Sprintf("%+v", VRF.Spec.LoopbackIP.IP)

	var address = VRF.Spec.LoopbackIP
	var Addrs = &netlink.Addr{
		IPNet: address,
	}
	addr_err := nlink.AddrAdd(ctx, link, Addrs)
	if addr_err != nil {
		log.Println("LGM: Unable to set the loopback ip to VRF link %s \n", VRF.Name)
		return "", false
	}

	log.Println("LGM: Added Address %s dev %s\n", Lbip, VRF.Name)

	Src1 := net.IPv4(0, 0, 0, 0)
	route := netlink.Route{
		Table:    int(routing_table),
		Type:     unix.RTN_THROW,
		Protocol: 255,
		Priority: 9999,
		Src:      Src1,
	}
	routeadd_err := nlink.RouteAdd(ctx, &route)
	if routeadd_err != nil {
		log.Println("LGM : Failed in adding Route throw default %+v\n", routeadd_err)
		return "", false
	}

	log.Println("LGM : Added route throw default table %d proto opi_evpn_br metric 9999\n", routing_table)
	// Disable reverse-path filtering to accept ingress traffic punted by the pipeline
	// disable_rp_filter("rep-"+VRF.Name)
	// Configuration specific for VRFs associated with L3 EVPN
	if !reflect.ValueOf(VRF.Spec.Vni).IsZero() {
		// Create bridge for external VXLAN under VRF
		// Linux apparently creates a deterministic MAC address for a bridge type link with a given
		// name. We need to assign a true random MAC address to avoid collisions when pairing two
		// IPU servers.

		br_err := nlink.LinkAdd(ctx, &netlink.Bridge{
			LinkAttrs: netlink.LinkAttrs{Name: "br-" + VRF.Name},
		})
		if br_err != nil {
			log.Println("LGM : Error in added bridge port\n")
			return "", false
		}
		log.Println("LGM : Added link br-%s type bridge\n", VRF.Name)

		rmac := fmt.Sprintf("%+v", GenerateMac()) // str(macaddress.MAC(b'\x00'+random.randbytes(5))).replace("-", ":")
		hw, _ := net.ParseMAC(rmac)

		link_br, br_err := nlink.LinkByName(ctx, "br-"+VRF.Name)
		if br_err != nil {
			log.Println("LGM : Error in getting the br-%s\n", VRF.Name)
			return "", false
		}
		hw_err := nlink.LinkSetHardwareAddr(ctx, link_br, hw)
		if hw_err != nil {
			log.Println("LGM: Failed in the setting Hardware Address\n")
			return "", false
		}

		linkmtu_err := nlink.LinkSetMTU(ctx, link_br, ip_mtu)
		if linkmtu_err != nil {
			log.Println("LGM : Unable to set MTU to link br-%s \n", VRF.Name)
			return "", false
		}

		link_master, err_master := nlink.LinkByName(ctx, VRF.Name)
		if err_master != nil {
			log.Println("LGM : Error in getting the %s\n", VRF.Name)
			return "", false
		}

		err := nlink.LinkSetMaster(ctx, link_br, link_master)
		if err != nil {
			log.Println("LGM : Unable to set the master to br-%s link", VRF.Name)
			return "", false
		}

		linksetup_err = nlink.LinkSetUp(ctx, link_br)
		if linksetup_err != nil {
			log.Println("LGM : Unable to set link %s UP \n", VRF.Name)
			return "", false
		}
		log.Println("LGM: link set  br-%s master  %s up mtu \n", VRF.Name, Ip_Mtu)

		// Create the VXLAN link in the external bridge

		Src_vtep := VRF.Spec.VtepIP.IP
		vxlan_err := nlink.LinkAdd(ctx, &netlink.Vxlan{
			LinkAttrs: netlink.LinkAttrs{Name: "vxlan-" + VRF.Name, MTU: ip_mtu}, VxlanId: int(*VRF.Spec.Vni), SrcAddr: Src_vtep, Learning: false, Proxy: true, Port: 4789})
		if vxlan_err != nil {
			log.Println("LGM : Error in added vxlan port\n")
			return "", false
		}

		log.Println("LGM : link added vxlan-%s type vxlan id %d local %s dstport 4789 nolearning proxy\n", VRF.Name, *VRF.Spec.Vni, vtip)

		link_vxlan, vxlan_err := nlink.LinkByName(ctx, "vxlan-"+VRF.Name)
		if vxlan_err != nil {
			log.Println("LGM : Error in getting the %s\n", "vxlan-"+VRF.Name)
			return "", false
		}

		err = nlink.LinkSetMaster(ctx, link_vxlan, link_br)
		if err != nil {
			log.Println("LGM : Unable to set the master to vxlan-%s link", VRF.Name)
			return "", false
		}

		log.Println("LGM: VRF Link vxlan setup master\n")

		linksetup_err = nlink.LinkSetUp(ctx, link_vxlan)
		if linksetup_err != nil {
			log.Println("LGM : Unable to set link %s UP \n", VRF.Name)
			return "", false
		}
	}
	details := fmt.Sprintf("{\"routing_table\":\"%d\"}", routing_table)
	*VRF.Metadata.RoutingTable[0] = routing_table
	return details, true
}

func set_up_svi(SVI *infradb.Svi) (string, bool) {
	link_svi := fmt.Sprintf("%+v-%+v", path.Base(SVI.Spec.Vrf), strings.Split(path.Base(SVI.Spec.LogicalBridge), "vlan")[1])
	MacAddress := fmt.Sprintf("%+v", SVI.Spec.MacAddress)
	//ip_mtu := fmt.Sprintf("%+v", ip_mtu)
	//vid := strings.Split(path.Base(SVI.Spec.LogicalBridge),"vlan")[1]
	vid, err := strconv.Atoi(strings.Split(path.Base(SVI.Spec.LogicalBridge), "vlan")[1])
	brIntf, err := nlink.LinkByName(ctx, br_tenant)
	if err != nil {
		log.Println("LGM : Failed to get link information for %s: %v\n", br_tenant, err)
		return "", false
	}
	if err = nlink.BridgeVlanAdd(ctx, brIntf, uint16(vid), true, false, false, false); err != nil {
		log.Println("LGM : Failed to add VLAN %d to bridge interface %s: %v\n", vid, br_tenant, err)
		return "", false
	}
	/*
		CP, err := run([]string{"bridge", "vlan", "add", "dev", br_tenant, "vid", vid ,"self"},false)
		if err != 0 {
			log.Println("LGM: Error in executing command %s %s\n", "bridge vlan add dev ", br_tenant)
			log.Println("%s\n", CP)
			return "", false
		}*/
	log.Println("LGM Executed : bridge vlan add dev %s vid %s self\n", br_tenant, vid)
	vlanLink := &netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: link_svi, ParentIndex: brIntf.Attrs().Index}, VlanId: int(vid)}
	if err = nlink.LinkAdd(ctx, vlanLink); err != nil {
		log.Println("LGM : Failed to add VLAN sub-interface %s: %v\n", link_svi, err)
		return "", false
	}
	/*
		CP, err = run([]string{"ip", "link", "add", "link", br_tenant, "name", link_svi, "type", "vlan", "id", vid}, false)
		if err != 0 {
			log.Println("LGM: Error in executing command %s %s %s\n", "ip link add link",br_tenant, link_svi)
			log.Println("%s\n", CP)
			return "", false
		}*/
	log.Println("LGM Executed : ip link add link %s name %s type vlan id %s\n", br_tenant, link_svi, vid)
	if err = nlink.LinkSetHardwareAddr(ctx, vlanLink, *SVI.Spec.MacAddress); err != nil {
		log.Println("LGM : Failed to set link %s: %v\n", vlanLink, err)
		return "", false
	}
	/*
		CP, err = run([]string{"ip", "link", "set", link_svi, "address", MacAddress}, false)
		if err != 0 {
			log.Println("LGM: Error in executing command %s %s\n", "ip link set", link_svi)
			log.Println("%s\n", CP)
			return "", false
		}*/
	log.Println("LGM Executed : ip link set %s address %s\n", link_svi, MacAddress)
	vrfIntf, err := nlink.LinkByName(ctx, path.Base(SVI.Spec.Vrf))
	if err != nil {
		log.Println("LGM : Failed to get link information for %s: %v\n", path.Base(SVI.Spec.Vrf), err)
		return "", false
	}
	if err = nlink.LinkSetMaster(ctx, vlanLink, vrfIntf); err != nil {
		log.Println("LGM : Failed to set master for %s: %v\n", vlanLink, err)
		return "", false
	}
	if err = nlink.LinkSetUp(ctx, vlanLink); err != nil {
		log.Println("LGM : Failed to set up link for %s: %v\n", vlanLink, err)
		return "", false
	}
	if err = nlink.LinkSetMTU(ctx, vlanLink, ip_mtu); err != nil {
		log.Println("LGM : Failed to set MTU for %s: %v\n", vlanLink, err)
		return "", false
	}
	/*
		CP, err = run([]string{"ip", "link", "set", link_svi, "master", path.Base(SVI.Spec.Vrf), "up", "mtu", ip_mtu}, false)
		if err != 0 {
			log.Println("LGM: Error in executing command %s %s\n", "ip link set", link_svi)
			log.Println("%s\n", CP)
			return "", false
		}*/
	log.Println("LGM Executed :  ip link set %s master %s up mtu %s\n", link_svi, path.Base(SVI.Spec.Vrf), ip_mtu)
	command := fmt.Sprintf("net.ipv4.conf.%s.arp_accept=1", link_svi)
	CP, err1 := run([]string{"sysctl", "-w", command}, false)
	if err1 != 0 {
		log.Println("LGM: Error in executing command %s %s\n", "sysctl -w net.ipv4.conf.link_svi.arp_accept=1", link_svi)
		log.Println("%s\n", CP)
		return "", false
	}
	for _, ip_intf := range SVI.Spec.GatewayIPs {
		addr := &netlink.Addr{
			IPNet: &net.IPNet{
				IP:   ip_intf.IP,
				Mask: ip_intf.Mask,
			},
		}
		if err := nlink.AddrAdd(ctx, vlanLink, addr); err != nil {
			log.Println("LGM: Failed to add ip address %s to %s: %v\n", addr)
			return "", false
		}
		/*
			IP := fmt.Sprintf("+%v", ip_intf.IP.To4())
			CP, err = run([]string{"ip", "address", "add", IP, "dev", link_svi}, false)
			if err != 0 {
				log.Println("LGM: Error in executing command %s %s\n","ip address add",ip_intf.IP.To4())
				log.Println("%s\n", CP)
				return "", false
			}*/
		log.Println("LGM Executed :  ip address add %s dev %s\n", addr, vlanLink)
	}
	return "", true
}

func GenerateMac() net.HardwareAddr {
	buf := make([]byte, 5)
	var mac net.HardwareAddr
	_, err := rand.Read(buf)
	if err != nil {
	}

	// Set the local bit
	//  buf[0] |= 8

	mac = append(mac, 00, buf[0], buf[1], buf[2], buf[3], buf[4])

	return mac
}

func NetMaskToInt(mask int) (netmaskint [4]int64) {
	var binarystring string

	for ii := 1; ii <= mask; ii++ {
		binarystring = binarystring + "1"
	}
	for ii := 1; ii <= (32 - mask); ii++ {
		binarystring = binarystring + "0"
	}
	oct1 := binarystring[0:8]
	oct2 := binarystring[8:16]
	oct3 := binarystring[16:24]
	oct4 := binarystring[24:]
	// var netmaskint [4]int
	netmaskint[0], _ = strconv.ParseInt(oct1, 2, 64)
	netmaskint[1], _ = strconv.ParseInt(oct2, 2, 64)
	netmaskint[2], _ = strconv.ParseInt(oct3, 2, 64)
	netmaskint[3], _ = strconv.ParseInt(oct4, 2, 64)

	// netmaskstring = strconv.Itoa(int(ii1)) + "." + strconv.Itoa(int(ii2)) + "." + strconv.Itoa(int(ii3)) + "." + strconv.Itoa(int(ii4))
	return netmaskint
}

type valid_ip struct {
	IP   string
	Mask int
}

func get_ip_address(dev string) net.IPNet {
	link, err := nlink.LinkByName(ctx, dev)
	if err != nil {
		log.Println("LGM: Error in LinkByName %+v\n", err)
		return net.IPNet{
			IP: net.ParseIP("0.0.0.0"),
		}
	}

	addrs, err := nlink.AddrList(ctx, link, netlink.FAMILY_V4) // ip address show
	if err != nil {
		log.Println("LGM: Error in AddrList\n")
		return net.IPNet{
			IP: net.ParseIP("0.0.0.0"),
		}
	}
	var address = &net.IPNet{
		IP:   net.IPv4(127, 0, 0, 0),
		Mask: net.CIDRMask(8, 32)}
	var addr = &netlink.Addr{IPNet: address}
	var valid_ips []netlink.Addr
	for index := 0; index < len(addrs); index++ {
		if !addr.Equal(addrs[index]) {
			valid_ips = append(valid_ips, addrs[index])
		}
	}
	return *valid_ips[0].IPNet
}

func tear_down_vrf(VRF *infradb.Vrf) bool {
	Ifname := strings.Split(VRF.Name, "/")
	ifwlen := len(Ifname)
	VRF.Name = Ifname[ifwlen-1]
	link, err1 := nlink.LinkByName(ctx, VRF.Name)
	if err1 != nil {
		log.Println("LGM : Link %s not found %+v\n", VRF.Name, err1)
		return true
	}

	if VRF.Name == "GRD" {
		return true
	}
	routing_table := *VRF.Metadata.RoutingTable[0]
	// Delete the Linux networking artefacts in reverse order
	if !reflect.ValueOf(VRF.Spec.Vni).IsZero() {

		link_vxlan, link_err := nlink.LinkByName(ctx, "vxlan-"+VRF.Name)
		if link_err != nil {
			log.Println("LGM : Link vxlan-%s not found %+v\n", VRF.Name, link_err)
			return false
		}
		delerr := nlink.LinkDel(ctx, link_vxlan)
		if delerr != nil {
			log.Println("LGM: Error in delete vxlan %+v\n", delerr)
			return false
		}
		log.Println("LGM : Delete vxlan-%s\n", VRF.Name)

		link_br, linkbr_err := nlink.LinkByName(ctx, "br-"+VRF.Name)
		if linkbr_err != nil {
			log.Println("LGM : Link br-%s not found %+v\n", VRF.Name, linkbr_err)
			return false
		}
		delerr = nlink.LinkDel(ctx, link_br)
		if delerr != nil {
			log.Println("LGM: Error in delete br %+v\n", delerr)
			return false
		}
		log.Println("LGM : Delete br-%s\n", VRF.Name)

		route_table := fmt.Sprintf("%+v", routing_table)
		flusherr := nlink.RouteFlushTable(ctx, route_table)
		if flusherr != nil {
			log.Println("LGM: Error in flush table  %+v\n", route_table)
			return false
		}
		log.Println("LGM Executed : ip route flush table %s\n", route_table)

		delerr = nlink.LinkDel(ctx, link)
		if delerr != nil {
			log.Println("LGM: Error in delete br %+v\n", delerr)
			return false
		}
		log.Println("LGM :link delete  %s\n", VRF.Name)
	}
	return true
}

func tear_down_svi(SVI *infradb.Svi) bool {
	link_svi := fmt.Sprintf("%+iv-%+v", path.Base(SVI.Spec.Vrf), strings.Split(path.Base(SVI.Spec.LogicalBridge), "vlan")[1])
	Intf, err := nlink.LinkByName(ctx, link_svi)
	if err != nil {
		log.Println("LGM : Failed to get link %s: %v\n", link_svi)
		return true
	}
	/*
		CP, err := run([]string{"ifconfig", "-a", link_svi}, false)
		if err != 0 {
			log.Println("CP LGM %s\n", CP)
			return true
		}*/
	if err = nlink.LinkDel(ctx, Intf); err != nil {
		log.Println("LGM : Failed to delete link %s: %v\n", link_svi, err)
		return false
	}
	log.Println("LGM: Executed ip link delete %s\n", link_svi)
	/*
		CP, err = run([]string{"ip", "link", "del", link_svi}, false)
		if err != 0 {
			log.Println("LGM: Error in executing command %s %s\n","ip link del", link_svi)
			return false
		}*/
	return true
}

func tear_down_bridge(LB *infradb.LogicalBridge) bool {
	link := fmt.Sprintf("vxlan-%+v", LB.Spec.VlanID)
	if !reflect.ValueOf(LB.Spec.Vni).IsZero() {
		Intf, err := nlink.LinkByName(ctx, link)
		if err != nil {
			log.Println("LGM: Failed to get link %s: %v\n", link)
			return true
		}
		if err = nlink.LinkDel(ctx, Intf); err != nil {
			log.Println("LGM : Failed to delete link %s: %v\n", link, err)
			return false
		}
		log.Println("LGM: Executed ip link delete %s\n", link)
		/*
			CP, err := run([]string{"ip", "link", "del", link}, false)
			if err != 0 {
				log.Println("LGM:Error in executing command %s %s\n", "ip link del ", link)
				log.Println("%s\n", CP)
				return false
			}*/
		return true
	}
	return true
}
