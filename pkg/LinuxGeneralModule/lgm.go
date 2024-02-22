package LinuxGeneralModule

import (
	"encoding/binary"
	"encoding/json"
	"fmt"

	// "io/ioutil"
	"log"
	"math/rand"
	"net"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/opiproject/opi-evpn-bridge/pkg/config"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
	// "gopkg.in/yaml.v2"
)

type ModulelgmHandler struct{}

/*type SubscriberConfig struct {
	Name     string   `yaml:"name"`
	Priority int      `yaml:"priority"`
	Events   []string `yaml:"events"`
}

type Linux_frrConfig struct {
	Enable       bool   `yaml:"enabled"`
	Default_vtep string `yaml:"default_vtep"`
	Port_mux     string `yaml:"port_mux"`
	Vrf_mux      string `yaml:"vrf_mux"`
	Ip_mtu       int    `yaml:"ip_mtu"`
}

type Config struct {
	Subscribers []SubscriberConfig `yaml:"subscribers"`
	Linux_frr   Linux_frrConfig    `yaml:"linux_frr"`
}*/

func run(cmd []string, flag bool) (string, int) {
	var out []byte
	var err error
	out, err = exec.Command("sudo", cmd...).CombinedOutput()
	if err != nil {
		if flag {
			panic(fmt.Sprintf("LGM: Command %s': exit code %s;", out, err.Error()))
		}
		fmt.Printf("LGM: Command %s': exit code %s;\n", out, err)
		return "Error", -1
	}
	output := string(out)
	return output, 0
}

func (h *ModulelgmHandler) HandleEvent(eventType string, objectData *event_bus.ObjectData) {
	switch eventType {
	case "vrf":
		fmt.Printf("LGM recevied %s %s\n", eventType, objectData.Name)
		handlevrf(objectData)
	case "svi":
		fmt.Printf("LGM recevied %s %s\n", eventType, objectData.Name)
		handlesvi(objectData.Name)
	case "logical-bridge":
		fmt.Printf("LGM recevied %s %s\n", eventType, objectData.Name)
		handleLB(objectData)
	default:
		fmt.Println("LGM: error: Unknown event type %s", eventType)
	}
}

func handleLB(objectData *event_bus.ObjectData) {
	var comp common.Component
	LB, err := infradb.GetLB(objectData.Name)
	if err != nil {
		fmt.Printf("LGM: GetLB error: %s %s\n", err, objectData.Name)
		return
	} else {
		fmt.Printf("LGM : GetLB Name: %s\n", LB.Name)
	}
	if len(LB.Status.Components) != 0 {
		for i := 0; i < len(LB.Status.Components); i++ {
			if LB.Status.Components[i].Name == "lgm" {
				comp = LB.Status.Components[i]
			}
		}
	}
	if LB.Status.LBOperStatus != infradb.LB_OPER_STATUS_TO_BE_DELETED {
		status := set_up_bridge(LB)
		comp.Name = "lgm"
		if status == true {
			comp.Details = ""
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
		fmt.Printf("LGM: %+v \n", comp)
		infradb.UpdateLBStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
	} else {
		status := tear_down_bridge(LB)
		comp.Name = "lgm"
		if status == true {
			comp.CompStatus = common.COMP_STATUS_SUCCESS
			comp.Timer = 0
		} else {
			comp.CompStatus = common.COMP_STATUS_ERROR
			if comp.Timer == 0 {
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
		}
		fmt.Printf("LGM: %+v\n", comp)
		infradb.UpdateLBStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
	}
}

func handlesvi(eventName string) {
	fmt.Printf("dummy %s\n", eventName)
}

func handlevrf(objectData *event_bus.ObjectData) {
	var comp common.Component
	VRF, err := infradb.GetVrf(objectData.Name)
	if err != nil {
		fmt.Printf("LGM: GetVRF error: %s %s\n", err, objectData.Name)
		return
	} else {
		fmt.Printf("LGM : GetVRF Name: %s\n", VRF.Name)
	}
	if objectData.ResourceVersion != VRF.ResourceVersion {
		fmt.Printf("LGM: Mismatch in resoruce version %+v\n and VRF resource version %+v\n", objectData.ResourceVersion, VRF.ResourceVersion)
		comp.Name = "lgm"
		comp.CompStatus = common.COMP_STATUS_ERROR
		if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
			comp.Timer = 2 * time.Second
		} else {
			comp.Timer = comp.Timer * 2
		}
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
		return
	}
	if len(VRF.Status.Components) != 0 {
		for i := 0; i < len(VRF.Status.Components); i++ {
			if VRF.Status.Components[i].Name == "lgm" {
				comp = VRF.Status.Components[i]
			}
		}
	}
	if VRF.Status.VrfOperStatus != infradb.VRF_OPER_STATUS_TO_BE_DELETED {
		details, status := set_up_vrf(VRF)
		comp.Name = "lgm"
		if status == true {
			comp.Details = details
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
		fmt.Printf("LGM: %+v \n", comp)
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, VRF.Metadata, comp)
	} else {
		status := tear_down_vrf(VRF)
		comp.Name = "lgm"
		if status == true {
			comp.CompStatus = common.COMP_STATUS_SUCCESS
			comp.Timer = 0
		} else {
			comp.CompStatus = common.COMP_STATUS_ERROR
			if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
		}
		fmt.Printf("LGM: %+v\n", comp)
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
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

func Init() {
	/*config, err := readConfig("config.yaml")
	if err != nil {
		log.Fatal(err)
	}*/
	eb := event_bus.EBus
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
}

func routing_table_busy(table string) bool {
	_, err := run([]string{"ip", "route", "show", "table", table}, false)
	if err != 0 {
		// fmt.Println("%s\n",CP)
		return false
	}
	return true // reflect.ValueOf(CP).IsZero() && len(CP)!= 0
}

func set_up_bridge(LB *infradb.LogicalBridge) bool {
	link := fmt.Sprintf("vxlan-%+v", LB.Spec.VlanId)
	if !reflect.ValueOf(LB.Spec.Vni).IsZero() {
		Vni := fmt.Sprintf("%+v", *LB.Spec.Vni)
		VtepIP := fmt.Sprintf("%+v", LB.Spec.VtepIP.IP)
		Vlanid := fmt.Sprintf("%+v", LB.Spec.VlanId)
		ip_mtu := fmt.Sprintf("%+v", ip_mtu)
		fmt.Printf("%v %v %v %v %v\n", Vni, VtepIP, Vlanid, ip_mtu)
		CP, err := run([]string{"ip", "link", "add", link, "type", "vxlan", "id", Vni, "local", VtepIP, "dstport", "4789", "nolearning", "proxy"}, false)
		if err != 0 {
			fmt.Printf("LGM:Error in executing command %s %s\n", "link add ", link)
			fmt.Printf("%s\n", CP)
			return false
		}
		CP, err = run([]string{"ip", "link", "set", link, "master", br_tenant, "up", "mtu", ip_mtu}, false)
		if err != 0 {
			fmt.Printf("LGM:Error in executing command %s %s\n", "link set ", link)
			fmt.Printf("%s\n", CP)
			return false
		}
		CP, err = run([]string{"bridge", "vlan", "add", "dev", link, "vid", Vlanid, "pvid", "untagged"}, false)
		if err != 0 {
			fmt.Printf("LGM:Error in executing command %s %s\n", "bridge vlan add dev", link)
			fmt.Printf("%s\n", CP)
			return false
		}
		CP, err = run([]string{"bridge", "link", "set", "dev", link, "neigh_suppress", "on"}, false)
		if err != 0 {
			fmt.Printf("LGM:Error in executing command %s %s\n", "bridge link set dev link neigh_suppress on", link)
			fmt.Printf("%s\n", CP)
			return false
		}
		return true
	}
	return false
}

func set_up_vrf(VRF *infradb.Vrf) (string, bool) {
	vtip := fmt.Sprintf("%+v", VRF.Spec.VtepIP.IP)
	VNI := fmt.Sprintf("%+v", VRF.Spec.Vni)
	routing_table := fmt.Sprintf("%+v", VRF.Spec.Vni)
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
	VRF.Metadata.RoutingTable = make([]*uint32, 1)
	VRF.Metadata.RoutingTable[0] = new(uint32)
	if routing_table_busy(routing_table) {
		fmt.Printf("LGM :Routing table %s is not empty\n", routing_table)
		// return "Error"
	}
	if reflect.ValueOf(VRF.Spec.VtepIP).IsZero() {
		// Verify that the specified VTEP IP exists as local IP
		_, err := run([]string{"ip", "route", "list", "exact", vtip, "table", "local"}, false)
		if err != 0 {
			fmt.Printf(" LGM: VTEP IP not found: %+v\n", VRF.Spec.VtepIP)
			return "", false
		}
	} else {
		// Pick the IP of interface default VTEP interface
		// fmt.Printf("LGM: VTEP iP %+v\n",get_ip_address(default_vtep))
		VRF.Spec.VtepIP = get_ip_address(default_vtep)
	}
	// Create the VRF interface for the specified routing table and add loopback address
	CP, err := run([]string{"ip", "link", "add", VRF.Name, "type", "vrf", "table", routing_table}, false)
	if err != 0 {
		fmt.Printf("LGM: Error in executing command %s %s\n", "link add VRF type vrf table ", routing_table)
		fmt.Printf("%s\n", CP)
		return "", false
	}
	fmt.Printf("LGM Executed : ip link add %s type vrf table %s\n", VRF.Name, routing_table)
	CP, err = run([]string{"ip", "link", "set", VRF.Name, "up", "mtu", Ip_Mtu}, false)
	if err != 0 {
		fmt.Printf("LGM:Error in executing command %s %s\n", "link set VRF MTU ", Ip_Mtu)
		fmt.Printf("%s\n", CP)
		return "", false
	}
	fmt.Printf("LGM Executed : ip link set %s up mtu  %s\n", VRF.Name, Ip_Mtu)
	Lbip := fmt.Sprintf("%+v", VRF.Spec.LoopbackIP.IP)
	CP, err = run([]string{"ip", "address", "add", Lbip, "dev", VRF.Name}, false)
	if err != 0 {
		fmt.Printf("LGM: Error in executing command %s %s\n", "address add LoopbackIP", Lbip)
		fmt.Printf("%s\n", CP)
		return "", false
	}
	fmt.Printf("LGM Executed : ip address add %s dev %s\n", Lbip, VRF.Name)
	// Add low-prio default route. Otherwise a miss leads to lookup in the next higher table
	CP, err = run([]string{"ip", "route", "add", "throw", "default", "table", routing_table, "proto", "ipu_infra_mgr", "metric", "9999"}, false)
	if err != 0 {
		fmt.Printf("LGM: Error in executing command %s %s\n", "route add throw default table ", routing_table)
		fmt.Printf("%s\n", CP)
		return "", false
	}
	fmt.Printf("LGM Executed : ip route add throw default table  %s proto ipu_infra_gmr metric 9999\n", routing_table)
	// Disable reverse-path filtering to accept ingress traffic punted by the pipeline
	// disable_rp_filter("rep-"+VRF.Name)
	// Configuration specific for VRFs associated with L3 EVPN
	if !reflect.ValueOf(VRF.Spec.Vni).IsZero() {
		// Create bridge for external VXLAN under VRF
		// Linux apparently creates a deterministic MAC address for a bridge type link with a given
		// name. We need to assign a true random MAC address to avoid collisions when pairing two
		// IPU servers.
		rmac := fmt.Sprintf("%+v", GenerateMac()) // str(macaddress.MAC(b'\x00'+random.randbytes(5))).replace("-", ":")
		CP, err := run([]string{"ip", "link", "add", "br-" + VRF.Name, "address", rmac, "type", "bridge"}, false)
		if err != 0 {
			fmt.Printf("LGM Rmac : %s\n", rmac)
			fmt.Printf("LGM: Error in executing command %s %s\n", "ip link add address rmac", CP)
			return "", false
		}
		fmt.Printf("LGM Executed : ip link add br-%s address %s tyoe bridge\n", VRF.Name, rmac)
		CP, err = run([]string{"ip", "link", "set", "br-" + VRF.Name, "master", VRF.Name, "up", "mtu", Ip_Mtu}, false)
		if err != 0 {
			fmt.Printf("LGM: Error in executing command %s %s\n", "ip link set master VRF mtu", CP)
			return "", false
		}
		fmt.Printf("LGM Executed : ip link set  br-%s master  %s up mtu \n", VRF.Name, Ip_Mtu)
		// Create the VXLAN link in the external bridge
		CP, err = run([]string{"ip", "link", "add", "vxlan-" + VRF.Name, "type", "vxlan", "id", VNI, "local", vtip, "dstport", "4789", "nolearning", "proxy"}, false)
		if err != 0 {
			fmt.Printf("LGM: Error in executing command ip link add vxlan-%s type vxlan id %s local %s dstport 4789 nolearning proxy\n", VRF.Name, VNI, vtip, CP)
			return "", false
		}
		fmt.Printf("LGM Executed : ip link add vxlan-%s type vxlan id %s local %s dstport 4789 nolearning proxy\n", VRF.Name, VNI, vtip)
		CP, err = run([]string{"ip", "link", "set", "vxlan-" + VRF.Name, "master", "br-" + VRF.Name, "up", "mtu", Ip_Mtu}, false)
		if err != 0 {
			fmt.Printf("LGM: Error in executing command %s %s\n", "ip link set master BR up mtu", CP)
			return "", false
		}
		fmt.Printf("LGM Executed : ip link set vxlan-%s master br-%s up mtu %s\n", VRF.Name, VRF.Name, Ip_Mtu)
	}
	details := fmt.Sprintf("{\"routing_table\":\"%s\"}", routing_table)
	*VRF.Metadata.RoutingTable[0] = VRF.Spec.Vni
	return details, true
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

type Addr_show_dev struct {
	Ifindex   int
	Ifname    string
	Flags     []string
	Mtu       int
	Qdisc     string
	Operstate string
	Group     string
	Txqlen    int
	Link_type string
	Address   string
	Broadcast string
	Addr_info []AddrInfo
}

type AddrInfo struct {
	Family              string
	Local               string
	Prefixlen           int
	Broadcast           string
	Scope               string
	Noprefixroute       bool
	Label               string
	Valid_life_time     uint64
	Preferred_life_time uint64
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
	var valid_ips []valid_ip
	CP, err := run([]string{"ip", "-j", "address", "show", "dev", dev}, false)
	if err != 0 {
		fmt.Printf("LGM:Error in executing \n")
		return net.IPNet{
			IP: net.ParseIP("0.0.0.0"),
		}
	}
	// Res := CP[2:len(CP)-3]
	Res := strings.Split(CP[2:len(CP)-3], "]},{")
	// fmt.Printf("JSON1 %+v \n",Res[0])
	// From the only interface in the list pick the first IP address
	// outside 127.0.0.0/8 loopback network.
	for i := 0; i < len(Res); i++ {
		var Asd Addr_show_dev
		err := json.Unmarshal([]byte(fmt.Sprintf("{%v}", Res[i])), &Asd)
		if err != nil {
			log.Println("error-", err)
		}
		// var ips []string
		for addr := 0; addr < len(Asd.Addr_info); addr++ {
			// ips=append(ips,Asd.Addr_info[addr].Local)
			if Asd.Addr_info[addr].Local != "127.0.0.0/8" {
				var VIp valid_ip
				VIp.IP = Asd.Addr_info[addr].Local
				VIp.Mask = Asd.Addr_info[addr].Prefixlen
				valid_ips = append(valid_ips, VIp)
			}
		}
	}
	mtoip := NetMaskToInt(valid_ips[0].Mask)
	b3 := make([]byte, 8) // Converting int64 to byte
	binary.LittleEndian.PutUint64(b3, uint64(mtoip[3]))
	b2 := make([]byte, 8)
	binary.LittleEndian.PutUint64(b2, uint64(mtoip[2]))
	b1 := make([]byte, 8)
	binary.LittleEndian.PutUint64(b1, uint64(mtoip[1]))
	b0 := make([]byte, 8)
	binary.LittleEndian.PutUint64(b0, uint64(mtoip[0]))
	nIP := net.IPNet{
		IP:   net.ParseIP(valid_ips[0].IP),
		Mask: net.IPv4Mask(b0[0], b1[0], b2[0], b3[0]),
	}
	return nIP
}

func tear_down_vrf(VRF *infradb.Vrf) bool {
	routing_table := fmt.Sprintf("%+v", VRF.Spec.Vni)
	Ifname := strings.Split(VRF.Name, "/")
	ifwlen := len(Ifname)
	VRF.Name = Ifname[ifwlen-1]
	CP, err := run([]string{"ifconfig", "-a", VRF.Name}, false)
	if err != 0 {
		fmt.Printf("CP LGM %s\n", CP)
		return true
	}
	if VRF.Name == "GRD" {
		return true
	}
	// Delete the Linux networking artefacts in reverse order
	if !reflect.ValueOf(VRF.Spec.Vni).IsZero() {
		CP, err = run([]string{"ip", "link", "delete", "vxlan-" + VRF.Name}, false)
		if err != 0 {
			fmt.Printf("LGM: Error in executing command %s %s\n", "ip link deleted vxlan ", CP)
			return false
		}
		fmt.Printf("LGM Executed : ip link delete vxlan-%s\n", VRF.Name)
		CP, err = run([]string{"ip", "link", "delete", "br-" + VRF.Name}, false)
		if err != 0 {
			fmt.Printf("LGM: Error in executing command %s %s\n", "ip link delete br-vrf ", CP)
			return false
		}
		fmt.Printf("LGM Executed : ip link delete br-%s\n", VRF.Name)
		CP, err = run([]string{"ip", "route", "flush", "table", routing_table}, false)
		if err != 0 {
			fmt.Printf("LGM: Error in executing command ip route flush table %s\n", routing_table, CP)
			return false
		}
		fmt.Printf("LGM Executed : ip route flush table %s\n", routing_table)
		CP, err = run([]string{"ip", "link", "delete", VRF.Name}, false)
		if err != 0 {
			fmt.Printf("LGM: Error in executing command ip link delete %s: %s\n", VRF.Name, CP)
			return false
		}
		fmt.Printf("LGM Executed : ip link delete  %s\n", VRF.Name)
	}
	return true
}

func tear_down_bridge(LB *infradb.LogicalBridge) bool {
	link := fmt.Sprintf("vxlan-%+v", LB.Spec.VlanId)
	if !reflect.ValueOf(LB.Spec.Vni).IsZero() {
		CP, err := run([]string{"ip", "link", "del", link}, false)
		if err != 0 {
			fmt.Printf("LGM:Error in executing command %s %s\n", "ip link del ", link)
			fmt.Printf("%s\n", CP)
			return false
		}
		return true
	}
	return false
}
