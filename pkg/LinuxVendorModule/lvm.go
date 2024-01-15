package LinuxVendorModule
import (
	"fmt"
	"io/ioutil"
	"log"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subsrciber_framework/event_bus"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
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

func (h *ModulelvmHandler) HandleEvent(eventType string, eventData *event_bus.EventData) {
	switch eventType {
	case "VRF":
		fmt.Printf("LVM recevied %s %s\n",eventType,eventData.Name)
		handlevrf(eventData)
	case "SVI":
		handlesvi(eventData.Name)
	default:
		fmt.Println("error: Unknown event type %s", eventType)
	}
}

func handlesvi(eventName string){
	fmt.Printf("dummy %s\n", eventName)
}

func handlevrf(eventData *event_bus.EventData){
	var comp infradb.Component
	VRF, err := infradb.GetVrf(eventData.Name)
	if err != nil {
		fmt.Printf("LVM : GetVrf error: %s\n", err)
		return
	}
	if (len(VRF.Status.Components) != 0 ){
		for i:=0;i<len(VRF.Status.Components);i++ {
			if (VRF.Status.Components[i].Name == "LVM") {
				comp = VRF.Status.Components[i]
			}
		}	
	}
	if (VRF.Status.VrfOperStatus !=infradb.VRF_OPER_STATUS_TO_BE_DELETED){
		status_update := configure_linux(&VRF)
		if status_update == true {
			comp.Details= ""
			comp.CompStatus= infradb.COMP_STATUS_SUCCESS
			comp.Name= "LVM"
			comp.Timer = 0
		} else {
			if comp.Timer == 0 {
				comp.Timer=2
			}else {
				comp.Timer = comp.Timer*2
			}
			comp.Name= "LVM"
			comp.CompStatus = infradb.COMP_STATUS_ERROR
		}
		infradb.UpdateVrfStatus(eventData.Name,eventData.ResourceVer,comp)
	} else {
		if tear_down_vrf(&VRF) {
			comp.CompStatus= infradb.COMP_STATUS_SUCCESS
			comp.Name= "LVM"
		} else {
			if comp.Timer == 0 {
				comp.Timer=2
			} else {
				comp.Timer = comp.Timer*2
			}
			comp.Name= "LVM"
			comp.CompStatus = infradb.COMP_STATUS_ERROR
			infradb.UpdateVrfStatus(eventData.Name,eventData.ResourceVer,comp)
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


func configure_linux(VRF *infradb.Vrf)bool{
	fmt.Printf("LVM configure linux function \n")
	out,err := run([]string{"ip","link","add","link", vrf_mux, "name", "rep-"+VRF.Name, "type", "vlan", "id", strconv.Itoa(int(VRF.Spec.Vni))}, false)
	if (err != 0){
		fmt.Printf("LVM configure linux function ip link add link %s name rep-%s type vlan id %s : %s\n",vrf_mux,VRF.Name,strconv.Itoa(int(VRF.Spec.Vni)),out)
		return false
	}
	fmt.Printf(" LVM: Executed ip link add link %s name rep-%s type vlan id %s\n",vrf_mux,VRF.Name,strconv.Itoa(int(VRF.Spec.Vni)))
	out,err = run([]string{"ip","link","set","rep-"+VRF.Name,"master",VRF.Name,"up"},false)
	if (err != 0){
		fmt.Printf("LVM configure linux function ip link set rep-%s master %s: %s\n",VRF.Name,VRF.Name,out)
		return false
	}
	fmt.Printf(" LVM: Executed ip link set rep-%s master %s up\n",VRF.Name,VRF.Name)
	disable_rp_filter("rep-"+VRF.Name)
	return true
}

func tear_down_vrf(VRF *infradb.Vrf)bool {
	if VRF.Name == "GRD"{
		return false
	}
	CP,err :=run([]string{"ip","link","delete","rep-"+VRF.Name},false)
	if(err !=0){
		fmt.Printf("LVM: Error in command ip link delete rep-%s: %s\n",VRF.Name,CP)
		return false
	}
	return true
}

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