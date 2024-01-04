package LinuxVendorModule
import (
        "fmt"
        "io/ioutil"
        "log"
        "github.com/opiproject/opi-evpn-bridge/pkg/infradb/subsrciber_framework/event_bus"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
        "gopkg.in/yaml.v2"
	"os/exec"
	//"strings"
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

func run(cmd []string,flag bool) (string, error) {
    fmt.Println("LVM: Executing command", cmd)
    var out []byte
    var err error
    out, err = exec.Command("sudo",cmd...).CombinedOutput()
    if err != nil {
            if flag == true {
                   panic(fmt.Sprintf("LVM: Command %s': exit code %s;",out,err.Error()))
            }
    }
    output := string(out[:])
    return output,err
}

func (h *ModulelvmHandler) HandleEvent(eventType string, eventData *event_bus.EventData) {
        switch eventType {
        case "VRF":
                handlevrf(eventData.Name)
        case "SVI":
                handlesvi(eventData.Name)
        default:
                fmt.Println("error: Unknown event type %s", eventType)
}
}

func handlesvi(eventName string){
	fmt.Printf("dummy %s\n", eventName)
}

func handlevrf(eventName string){
	 Vrf, err := infradb.GetVrf(eventName)
	 if err != nil {
                    fmt.Printf("GetVrf error: %s\n", err)
                    return
	    }
	    status_update := configure_linux(&Vrf)
	    if status_update != true {
		    return
	    }
	    infradb.UpdateVrfStatus(eventName,"Frr")
}

func configure_linux(VRF *infradb.Vrf)bool{
	out,err := run([]string{"ip","link","add","link", vrf_mux, "name", "rep-"+VRF.Name, "type", "vlan", "id", strconv.Itoa(int(VRF.Spec.Vni))}, false)
	if (err != nil){
		fmt.Println(out)
		return false
	}
	out,err = run([]string{"ip","link","set","rep-"+VRF.Name,"master",VRF.Name,"up"},false)
	if (err != nil){
		fmt.Println(out)
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
