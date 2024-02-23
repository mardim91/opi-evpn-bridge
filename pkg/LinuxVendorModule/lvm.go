package LinuxVendorModule

import (
	"fmt"
	"io/ioutil"

	// "log"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"github.com/opiproject/opi-evpn-bridge/pkg/config"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
	"gopkg.in/yaml.v2"
	"net"
	"path"
)

type SubscriberConfig struct {
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
}

var port_mux string
var vrf_mux string

type ModulelvmHandler struct{}

func run(cmd []string, flag bool) (string, int) {
	var out []byte
	var err error
	out, err = exec.Command("sudo", cmd...).CombinedOutput()
	if err != nil {
		if flag {
			panic(fmt.Sprintf("LVM: Command %s': exit code %s;", out, err.Error()))
		}
		fmt.Printf("LVM: Command %s': exit code %s;\n", out, err)
		return "Error", -1
	}
	output := string(out)
	return output, 0
}

func (h *ModulelvmHandler) HandleEvent(eventType string, objectData *event_bus.ObjectData) {
	switch eventType {
	case "vrf":
		fmt.Printf("LVM recevied %s %s\n", eventType, objectData.Name)
		handlevrf(objectData)
	case "bridge-port":
		fmt.Printf("LVM recevied %s %s\n", eventType, objectData.Name)
		handlebp(objectData)
	default:
		fmt.Println("error: Unknown event type %s", eventType)
	}
}

func handlebp(objectData *event_bus.ObjectData){
        var comp common.Component
        BP, err := infradb.GetBP(objectData.Name)
        if err != nil {
                fmt.Printf("LVM : GetBP error: %s\n", err)
                return
        }
        if (len(BP.Status.Components) != 0 ){
                for i:=0;i<len(BP.Status.Components);i++ {
                        if (BP.Status.Components[i].Name == "lvm") {
                                comp = BP.Status.Components[i]
                        }
                }
        }
        if (BP.Status.BPOperStatus !=infradb.BP_OPER_STATUS_TO_BE_DELETED){
                status := set_up_bp(BP)
                comp.Name= "lvm"
                if (status == true) {
                        comp.Details = ""
                        comp.CompStatus= common.COMP_STATUS_SUCCESS
                        comp.Timer = 0
                } else {
                        if comp.Timer ==0 {
                                comp.Timer=2 * time.Second
                        } else {
                                comp.Timer=comp.Timer*2
                        }
                        comp.CompStatus = common.COMP_STATUS_ERROR
                }
                fmt.Printf("LVM: %+v \n",comp)
                infradb.UpdateBPStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationId,BP.Metadata,comp)
        }else {
                status := tear_down_bp(BP)
                comp.Name= "lvm"
                if (status == true) {
                        comp.CompStatus= common.COMP_STATUS_SUCCESS
                        comp.Timer = 0
                } else {
                        if comp.Timer ==0 {
                                comp.Timer=2 * time.Second
                        } else {
                                comp.Timer=comp.Timer*2
                        }
                        comp.CompStatus = common.COMP_STATUS_ERROR
                }
                fmt.Printf("LVM: %+v \n",comp)
                infradb.UpdateBPStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationId,nil,comp)
        }
}

func MactoVport(mac *net.HardwareAddr)int {
	byte0 := int((*mac)[0])
	byte1 := int((*mac)[1])
	return (byte0 << 8) + byte1
}

func set_up_bp(BP *infradb.BridgePort)(bool){
	MacAddress := fmt.Sprintf("%+v", BP.Spec.MacAddress)
	vport_id := MactoVport(BP.Spec.MacAddress)
	link := fmt.Sprintf("vport-%+v", vport_id)
	vport := fmt.Sprintf("%+v", vport_id)
	ip_mtu := fmt.Sprintf("%+v", ip_mtu)
	BP.Metadata.VPort = vport
	CP, err := run([]string{"ip", "link", "add", "link", port_mux, "name", link, "type", "vlan", "protocol", "802.1ad", "id", vport}, false)
	if err !=0 {
		fmt.Printf("LVM: Error in executing command %s %s with error %s\n","ip link add link", link, CP)
		return false
	}
	CP, err = run([]string{"ip", "link", "set", link, "master", br_tenant, "up", "mtu", ip_mtu}, false)
	if err !=0 {
		fmt.Printf("LVM: Error in executing command %s %s with error %s\n","ip link set link",link,CP)
		return false
	}
	for _,vlan := range BP.Spec.LogicalBridges {
		vid := strings.Split(path.Base(vlan),"vlan")[1]
		//vid := fmt.Sprintf("%+v", vlan)
		CP, err = run([]string{"bridge", "vlan", "add", "dev", link, "vid", vid}, false)
		if err !=0 {
			fmt.Printf("LVM: Error in executing command %s %s with error %s\n","bridge vlan add",link,CP)
			return false
		}
	}
	CP, err = run([]string{"bridge", "fdb", "add", MacAddress, "dev", link, "master", "static", "extern_learn"}, false)
	if err !=0 {
		fmt.Printf("LVM: Error in executing command %s %s with error %s\n","bridge fdb add",link,CP)
		return false
	}
	return true
}

func tear_down_bp(BP *infradb.BridgePort)(bool){
	vport_id := MactoVport(BP.Spec.MacAddress)
	link := fmt.Sprintf("vport-%+v", vport_id)
	CP, err := run([]string{"ifconfig", "-a", link}, false)
	if err != 0 {
		fmt.Printf("CP LVM %s\n", CP)
		return true
	}
	CP, err = run([]string{"ip", "link", "del", link}, false)
	if err != 0 {
		fmt.Printf("LVM: Error in executing command %s %s with error %s\n","ip link del",link,CP)
		return false
	}
	return true
}


func handlesvi(eventName string) {
	fmt.Printf("dummy %s\n", eventName)
}

func handlevrf(objectData *event_bus.ObjectData) {
	var comp common.Component
	VRF, err := infradb.GetVrf(objectData.Name)
	if err != nil {
		fmt.Printf("LVM : GetVrf error: %s\n", err)
		return
	}
	if objectData.ResourceVersion != VRF.ResourceVersion {
		fmt.Printf("LVM: Mismatch in resoruce version %+v\n and VRF resource version %+v\n", objectData.ResourceVersion, VRF.ResourceVersion)
		comp.Name = "lvm"
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
			if VRF.Status.Components[i].Name == "lvm" {
				comp = VRF.Status.Components[i]
			}
		}
	}
	if VRF.Status.VrfOperStatus != infradb.VRF_OPER_STATUS_TO_BE_DELETED {
		status_update := set_up_vrf(VRF)
		comp.Name = "lvm"
		if status_update == true {
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
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
	} else {
		comp.Name = "lvm"
		if tear_down_vrf(VRF) {
			comp.CompStatus = common.COMP_STATUS_SUCCESS
		} else {
			if comp.Timer == 0 {
				comp.Timer = 2 * time.Second
			} else {
				comp.Timer = comp.Timer * 2
			}
			comp.CompStatus = common.COMP_STATUS_ERROR
		}
		infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
	}
}

func disable_rp_filter(Interface string) {
	// Work-around for the observation that sometimes the sysctl -w command did not take effect.
	rp_filter_disabled := false
	for i := 0; i < 3; i++ {
		rp_disable := fmt.Sprintf("net.ipv4.conf.%s.rp_filter=0", Interface)
		run([]string{"sysctl", "-w", rp_disable}, false)
		time.Sleep(2 * time.Millisecond)
		rp_disable = fmt.Sprintf("net.ipv4.conf.%s.rp_filter", Interface)
		CP, err := run([]string{"sysctl", "-n", rp_disable}, false)
		if err == 0 && strings.HasPrefix(CP, "0") {
			rp_filter_disabled = true
			fmt.Printf("LVM: rp_filter_disabled: %+v\n", rp_filter_disabled)
			break
		}
	}
	if !rp_filter_disabled {
		fmt.Sprintf("Failed to disable rp_filtering on interface %s\n", Interface)
	}
}

func set_up_vrf(VRF *infradb.Vrf) bool {
	fmt.Printf("LVM configure linux function \n")
	Ip_Mtu := fmt.Sprintf("%+v", ip_mtu)
	Ifname := strings.Split(VRF.Name, "/")
	ifwlen := len(Ifname)
	VRF.Name = Ifname[ifwlen-1]
	if VRF.Name == "GRD" {
		disable_rp_filter("rep-" + VRF.Name)
		return true
	}
	out, err := run([]string{"ip", "link", "add", "link", vrf_mux, "name", "rep-" + VRF.Name, "type", "vlan", "id", strconv.Itoa(int(VRF.Spec.Vni))}, false)
	if err != 0 {
		fmt.Printf("LVM configure linux function ip link add link %s name rep-%s type vlan id %s : %s\n", vrf_mux, VRF.Name, strconv.Itoa(int(VRF.Spec.Vni)), out)
		return false
	}
	fmt.Printf(" LVM: Executed ip link add link %s name rep-%s type vlan id %s\n", vrf_mux, VRF.Name, strconv.Itoa(int(VRF.Spec.Vni)))
	out, err = run([]string{"ip", "link", "set", "rep-" + VRF.Name, "master", VRF.Name, "up", "mtu", Ip_Mtu}, false)
	if err != 0 {
		fmt.Printf("LVM configure linux function ip link set rep-%s master %s: %s\n", VRF.Name, VRF.Name, out)
		return false
	}
	fmt.Printf(" LVM: Executed ip link set rep-%s master %s up mtu %s\n", VRF.Name, VRF.Name, Ip_Mtu)
	disable_rp_filter("rep-" + VRF.Name)
	return true
}

func tear_down_vrf(VRF *infradb.Vrf) bool {
	Ifname := strings.Split(VRF.Name, "/")
	ifwlen := len(Ifname)
	VRF.Name = Ifname[ifwlen-1]
	CP, err := run([]string{"ifconfig", "-a", "rep-" + VRF.Name}, false)
	if err != 0 {
		fmt.Printf("CP LVM %s\n", CP)
		return true
	}
	if VRF.Name == "GRD" {
		return true
	}
	CP, err = run([]string{"ip", "link", "delete", "rep-" + VRF.Name}, false)
	if err != 0 {
		fmt.Printf("LVM: Error in command ip link delete rep-%s: %s\n", VRF.Name, CP)
		return false
	}
	fmt.Printf(" LVM: Executed ip link delete rep-%s\n", VRF.Name)
	return true
}

var ip_mtu int
var br_tenant string
func Init() {
	/*config, err := readConfig("config.yaml")
	if err != nil {
		log.Fatal(err)
	}*/
	eb := event_bus.EBus
	for _, subscriberConfig := range config.GlobalConfig.Subscribers {
		if subscriberConfig.Name == "lvm" {
			for _, eventType := range subscriberConfig.Events {
				eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModulelvmHandler{})
			}
		}
	}
	port_mux = config.GlobalConfig.Linux_frr.Port_mux
	vrf_mux = config.GlobalConfig.Linux_frr.Vrf_mux
	ip_mtu = config.GlobalConfig.Linux_frr.Ip_mtu
	br_tenant = "br-tenant"
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
