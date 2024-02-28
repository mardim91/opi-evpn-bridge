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
	"github.com/vishvananda/netlink"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils"
	"context"
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
	BP.Metadata.VPort = vport
	muxIntf, err := nlink.LinkByName(ctx, port_mux)
        if err != nil {
                fmt.Printf("Failed to get link information for %s, error is %v\n", port_mux, err)
                return false
        }
	vlanLink := &netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: link, ParentIndex: muxIntf.Attrs().Index,},VlanId: vport_id, VlanProtocol: netlink.VLAN_PROTOCOL_8021AD,}
        if err = nlink.LinkAdd(ctx, vlanLink); err != nil {
                fmt.Printf("Failed to add VLAN sub-interface %s: %v\n", link, err)
                return false
        }
	fmt.Printf("LVM: Executed ip link add link %s name %s type vlan protocol 802.1ad id %s\n", port_mux, link, vport)
	brIntf, err := nlink.LinkByName(ctx, br_tenant)
        if err != nil {
                fmt.Printf("Failed to get link information for %s: %v\n", br_tenant, err)
                return false
        }
        if err = nlink.LinkSetMaster(ctx, vlanLink, brIntf); err != nil {
                fmt.Printf("Failed to set master for %s: %v\n", brIntf, err)
                return false
        }
        if err = nlink.LinkSetUp(ctx, vlanLink); err != nil {
                fmt.Printf("Failed to set up link for %s: %v\n", vlanLink, err)
                return false
        }
        if err = nlink.LinkSetMTU(ctx, vlanLink, ip_mtu); err != nil {
                fmt.Printf("Failed to set MTU for %s: %v\n", vlanLink, err)
                return false
        }
	fmt.Printf("LVM: Executed ip link set %s master %s up mtu %s\n", link, br_tenant, ip_mtu)
	for _,vlan := range BP.Spec.LogicalBridges {
		vid, err := strconv.Atoi(strings.Split(path.Base(vlan),"vlan")[1])
		if err != nil {
			fmt.Printf("Failed to convert LogicalBridges %s to integer: %v\n", vlan, err)
			return false
		}
		if err = nlink.BridgeVlanAdd(ctx, vlanLink, uint16(vid), true, false, false, false); err != nil {
			fmt.Printf("Failed to add VLAN %d to bridge interface %s: %v\n", vport_id, link, err)
			return false
		}
		fmt.Printf("LVM: Executed bridge vlan add dev %s vid %s\n", link, vid)
	}
	if err = nlink.BridgeFdbAdd(ctx, link, MacAddress); err != nil {
		fmt.Printf("LVM: Error in executing command %s %s with error %s\n","bridge fdb add",link,err)
		return false
	}
	fmt.Printf("LVM: Executed bridge fdb add %s dev %s master static extern_learn\n", MacAddress, link)
	return true
}

func tear_down_bp(BP *infradb.BridgePort)(bool){
	vport_id := MactoVport(BP.Spec.MacAddress)
	link := fmt.Sprintf("vport-%+v", vport_id)
	Intf, err := nlink.LinkByName(ctx, link)
        if err != nil {
                fmt.Printf("Failed to get link %s: %v\n", link)
                return true
        }
	if err = nlink.LinkDel(ctx, Intf); err != nil {
                fmt.Printf("Failed to delete link %s: %v\n", link, err)
                return false
        }
	fmt.Printf(" LVM: Executed ip link delete %s\n", link)
	return true
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
	vlanIntf := fmt.Sprintf("rep-%+v",path.Base(VRF.Name))
	if path.Base(VRF.Name) == "GRD" {
		disable_rp_filter("rep-" + path.Base(VRF.Name))
		return true
	}
	muxIntf, err := nlink.LinkByName(ctx, vrf_mux)
	if err != nil {
		fmt.Printf("Failed to get link information for %s, error is %v\n", vrf_mux, err)
		return false
	}
	vlanLink := &netlink.Vlan{LinkAttrs: netlink.LinkAttrs{Name: vlanIntf, ParentIndex: muxIntf.Attrs().Index,},VlanId: int(*VRF.Spec.Vni),}
	if err = nlink.LinkAdd(ctx, vlanLink); err != nil {
		fmt.Printf("Failed to add VLAN sub-interface %s: %v\n", vlanIntf, err)
		return false
	}
	fmt.Printf(" LVM: Executed ip link add link %s name rep-%s type vlan id %s\n", vrf_mux, path.Base(VRF.Name), strconv.Itoa(int(*VRF.Spec.Vni)))
	vrfIntf, err := nlink.LinkByName(ctx, path.Base(VRF.Name))
	if err != nil {
		fmt.Printf("Failed to get link information for %s: %v\n", path.Base(VRF.Name), err)
		return false
	}
	if err = nlink.LinkSetMaster(ctx, vlanLink, vrfIntf); err != nil {
		fmt.Printf("Failed to set master for %s: %v\n", vlanIntf, err)
		return false
	}
	if err = nlink.LinkSetUp(ctx, vlanLink); err != nil {
		fmt.Printf("Failed to set up link for %s: %v\n", vlanLink, err)
		return false
	}
	if err = nlink.LinkSetMTU(ctx, vlanLink, ip_mtu); err != nil {
		fmt.Printf("Failed to set MTU for %s: %v\n", vlanLink, err)
		return false
	}
	fmt.Printf(" LVM: Executed ip link set rep-%s master %s up mtu %s\n", path.Base(VRF.Name), path.Base(VRF.Name), ip_mtu)
	disable_rp_filter("rep-" + path.Base(VRF.Name))
	return true
}

func tear_down_vrf(VRF *infradb.Vrf) bool {
	vlanIntf := fmt.Sprintf("rep-%+v",path.Base(VRF.Name))
	if path.Base(VRF.Name) == "GRD" {
		return true
	}
	Intf, err := nlink.LinkByName(ctx, vlanIntf)
	if err != nil {
		fmt.Printf("Failed to get link %s: %v\n", vlanIntf)
		return true
	}
	if err = nlink.LinkDel(ctx, Intf); err != nil {
		fmt.Printf("Failed to delete link %s: %v\n", vlanIntf, err)
		return false
	}
	fmt.Printf(" LVM: Executed ip link delete rep-%s\n", path.Base(VRF.Name))
	return true
}

var ip_mtu int
var br_tenant string
var ctx context.Context
var nlink utils.Netlink
func Init() {
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
	ctx = context.Background()
	nlink = utils.NewNetlinkWrapper()
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
