package LinuxVendorModule
import (
	"fmt"
	"io/ioutil"
	"log"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"gopkg.in/yaml.v2"
	"os/exec"
	"strings"
	"time"
	"strconv"
)
type SubscriberConfig struct {
	Name     string   `yaml:"name"`
	Priority int      `yaml:"priority"`
	Events   []string `yaml:"events"`
}

type Linux_frrConfig struct {
	Enable       bool     `yaml:"enabled"`
	Default_vtep string   `yaml:"default_vtep"`
	Port_mux     string   `yaml:"port_mux"`
	Vrf_mux      string   `yaml:"vrf_mux"`
	Ip_mtu       int      `yaml:"ip_mtu"`
}

type Config struct {
	Subscribers []SubscriberConfig `yaml:"subscribers"`
	Linux_frr Linux_frrConfig `yaml:"linux_frr"`
}

var port_mux string
var vrf_mux string

type ModulelvmHandler struct{}


func run(cmd []string,flag bool) (string, int) {
	var out []byte
	var err error
	out, err = exec.Command("sudo",cmd...).CombinedOutput()
	if err != nil {
		if flag {
			panic(fmt.Sprintf("LVM: Command %s': exit code %s;",out,err.Error()))
		}
		fmt.Printf("LVM: Command %s': exit code %s;\n",out,err)
		return "Error",-1
	}
	output := string(out[:])
	return output,0
}

func (h *ModulelvmHandler) HandleEvent(eventType string, objectData *event_bus.ObjectData) {
	switch eventType {
	case "vrf":
		fmt.Printf("LVM recevied %s %s\n",eventType,objectData.Name)
		handlevrf(objectData)
	case "svi":
		handlesvi(objectData.Name)
	default:
		fmt.Println("error: Unknown event type %s", eventType)
	}
}

func handlesvi(eventName string){
	fmt.Printf("dummy %s\n", eventName)
}

func handlevrf(objectData *event_bus.ObjectData){
	var comp common.Component
	VRF, err := infradb.GetVrf(objectData.Name)
	if err != nil {
		fmt.Printf("LVM : GetVrf error: %s\n", err)
		return
	}
	if (len(VRF.Status.Components) != 0 ){
		for i:=0;i<len(VRF.Status.Components);i++ {
			if (VRF.Status.Components[i].Name == "lvm") {
				comp = VRF.Status.Components[i]
			}
		}	
	}
	if (VRF.Status.VrfOperStatus !=infradb.VRF_OPER_STATUS_TO_BE_DELETED){
		status_update := set_up_vrf(VRF)
			comp.Name= "lvm"
		if status_update == true {
			comp.Details= ""
			comp.CompStatus= common.COMP_STATUS_SUCCESS
			comp.Timer = 0
		} else {
			if comp.Timer == 0 {
				comp.Timer=2 * time.Second
			}else {
				comp.Timer = comp.Timer*2 
			}
			comp.CompStatus = common.COMP_STATUS_ERROR
		}
		infradb.UpdateVrfStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationId,nil,comp)
	} else {
			comp.Name= "lvm"
		if tear_down_vrf(VRF) {
			comp.CompStatus= common.COMP_STATUS_SUCCESS
		} else {
			if comp.Timer == 0 {
				comp.Timer=2
			} else {
				comp.Timer = comp.Timer*2
			}
			comp.CompStatus = common.COMP_STATUS_ERROR
			infradb.UpdateVrfStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationId,nil,comp)
		}
	}
}

func disable_rp_filter(Interface string ){
	// Work-around for the observation that sometimes the sysctl -w command did not take effect.
	rp_filter_disabled := false
	for i:=0; i<3 ; i++{
		rp_disable := fmt.Sprintf("net.ipv4.conf.%s.rp_filter=0",Interface)
		run([]string{"sysctl","-w",rp_disable},false)
		time.Sleep(2 * time.Millisecond)
		rp_disable = fmt.Sprintf("net.ipv4.conf.%s.rp_filter",Interface)
		CP,err := run([]string{"sysctl","-n",rp_disable},false)
		if err ==0 && strings.HasPrefix(CP, "0"){
			rp_filter_disabled = true
			fmt.Printf("LVM: rp_filter_disabled: %+v\n",rp_filter_disabled)
			break
		}
	}
	if !rp_filter_disabled{
		fmt.Sprintf("Failed to disable rp_filtering on interface %s\n",Interface)
	}
}


func set_up_vrf(VRF *infradb.Vrf)bool{
	fmt.Printf("LVM configure linux function \n")
	Ip_Mtu :=fmt.Sprintf("%+v",ip_mtu)	
	Ifname := strings.Split(VRF.Name,"/")
	ifwlen := len(Ifname)
	VRF.Name  = Ifname[ifwlen-1]
	if VRF.Name == "GRD" {
		disable_rp_filter("rep-"+VRF.Name)
		return true
	}
	out,err := run([]string{"ip","link","add","link", vrf_mux, "name", "rep-"+VRF.Name, "type", "vlan", "id", strconv.Itoa(int(VRF.Spec.Vni))}, false)
	if (err != 0){
		fmt.Printf("LVM configure linux function ip link add link %s name rep-%s type vlan id %s : %s\n",vrf_mux,VRF.Name,strconv.Itoa(int(VRF.Spec.Vni)),out)
		return false
	}
	fmt.Printf(" LVM: Executed ip link add link %s name rep-%s type vlan id %s\n",vrf_mux,VRF.Name,strconv.Itoa(int(VRF.Spec.Vni)))
	out,err = run([]string{"ip","link","set","rep-"+VRF.Name,"master",VRF.Name,"up","mtu",Ip_Mtu},false)
	if (err != 0){
		fmt.Printf("LVM configure linux function ip link set rep-%s master %s: %s\n",VRF.Name,VRF.Name,out)
		return false
	}
	fmt.Printf(" LVM: Executed ip link set rep-%s master %s up mtu %s\n",VRF.Name,VRF.Name,Ip_Mtu)
	disable_rp_filter("rep-"+VRF.Name)
	return true
}

func tear_down_vrf(VRF *infradb.Vrf)bool {
	Ifname := strings.Split(VRF.Name,"/")
	ifwlen := len(Ifname)
	VRF.Name  = Ifname[ifwlen-1]	
	if VRF.Name == "GRD"{
		return true
	}
	CP,err :=run([]string{"ip","link","delete","rep-"+VRF.Name},false)
	if(err !=0){
		fmt.Printf("LVM: Error in command ip link delete rep-%s: %s\n",VRF.Name,CP)
		return false
	}
	return true
}

var ip_mtu int
func Init() {
	config, err := readConfig("config.yaml")
	if err != nil {
		log.Fatal(err)
	}
	eb := event_bus.EBus
	for _, subscriberConfig := range config.Subscribers {
		if subscriberConfig.Name == "lvm" {
			for _, eventType := range subscriberConfig.Events {
				eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModulelvmHandler{})
			}
		}
	}
	port_mux = config.Linux_frr.Port_mux
	vrf_mux = config.Linux_frr.Vrf_mux
	ip_mtu = config.Linux_frr.Ip_mtu	
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
