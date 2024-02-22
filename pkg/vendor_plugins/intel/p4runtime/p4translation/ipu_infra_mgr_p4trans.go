package p4translation

import (
	"encoding/json"
	"fmt"

	// "io/ioutil"
	"log"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/opiproject/opi-evpn-bridge/pkg/config"
	nm "github.com/opiproject/opi-evpn-bridge/pkg/netlink"
	eb "github.com/opiproject/opi-evpn-bridge/pkg/vendor_plugins/event_bus"
	p4client "github.com/opiproject/opi-evpn-bridge/pkg/vendor_plugins/intel/p4runtime/p4driverAPI"
	"google.golang.org/grpc"

	// "gopkg.in/yaml.v2"

	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
)

var L3 L3Decoder
var Vxlan VxlanDecoder
var Pod PodDecoder

// var decoders []interface{}
// var decoders = []interface{}{L3, Vxlan, Pod}

/*
	type SubscriberConfig struct {
		Name     string   `yaml:"name"`
		Priority int      `yaml:"priority"`
		Events   []string `yaml:"events"`
	}

	type Config struct {
		Subscribers []SubscriberConfig `yaml:"subscribers"`
	}
*/
type ModuleipuHandler struct{}

func isValidMAC(mac string) bool {
	macPattern := `^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$`

	match, _ := regexp.MatchString(macPattern, mac)
	return match
}
func getMac(dev string) string {
	cmd := exec.Command("ip", "-d", "-j", "link", "show", dev)
	out, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("Error running command: %v", err)
		return ""
	}

	var links []struct {
		Address string `json:"address"`
	}
	if err := json.Unmarshal(out, &links); err != nil {
		log.Printf("Error unmarshaling JSON: %v", err)
		return ""
	}

	if len(links) > 0 {
		mac := links[0].Address
		return mac
	}

	return ""
}

func vport_from_mac(mac string) int {
	mbyte := strings.Split(mac, ":")
	if len(mbyte) < 5 {
		return -1
	}
	byte0, _ := strconv.ParseInt(mbyte[0], 16, 64)
	byte1, _ := strconv.ParseInt(mbyte[1], 16, 64)

	return int(byte0<<8 + byte1)
}

func ids_of(value string) (string, string, error) {
	if isValidMAC(value) {
		return strconv.Itoa(vport_from_mac(value)), value, nil
	}
	mac := getMac(value)
	vsi := vport_from_mac(mac)
	return strconv.Itoa(vsi), mac, nil
}

var (
	defaultAddr = fmt.Sprintf("127.0.0.1:9559")
	Conn        *grpc.ClientConn
)

func startSubscriber(eventBus *eb.EventBus, eventType string) {
	subscriber := eventBus.Subscribe(eventType)

	go func() {
		for {
			select {
			case event := <-subscriber.Ch:
				fmt.Printf("Subscriber for %s received event: \n", eventType)
				switch eventType {
				case "route_added":
					handleRouteAdded(event)
				case "route_updated":
					handleRouteUpdated(event)
				case "route_deleted":
					handleRouteDeleted(event)
				case "nexthop_added":
					handleNexthopAdded(event)
				case "nexthop_updated":
					handleNexthopUpdated(event)
				case "nexthop_deleted":
					handleNexthopDeleted(event)
				case "fdb_entry_added":
					handleFbdEntryAdded(event)
				case "fdb_entry_updated":
					handleFbdEntryUpdated(event)
				case "fdb_entry_deleted":
					handleFbdEntryDeleted(event)
				case "l2_nexthop_added":
					handleL2NexthopAdded(event)
				case "l2_nexthop_updated":
					handleL2NexthopUpdated(event)
				case "l2_nexthop_deleted":
					handleL2NexthopDeleted(event)
				}
			case <-subscriber.Quit:
				return
			}
		}
	}()
}

func handleRouteAdded(route interface{}) {
	var entries []interface{}
	routeData, _ := route.(nm.Route_struct)
	// for _, decoder := range decoders {
	//        entries = append(entries, L3.translate_added_route(routeData))
	entries = L3.translate_added_route(routeData)
	//}
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry:-", e)
		}
	}
}

func handleRouteUpdated(route interface{}) {
	var entries []interface{}
	routeData, _ := route.(nm.Route_struct)
	// for _, decoder := range decoders {
	// entries = append(entries, L3.translate_deleted_route(routeData))
	entries = L3.translate_deleted_route(routeData)
	//}
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	// for _, decoder := range decoders {
	entries = append(entries, L3.translate_added_route(routeData))
	//}
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}
func handleRouteDeleted(route interface{}) {
	var entries []interface{}
	routeData, _ := route.(nm.Route_struct)
	// for _, decoder := range decoders {
	// entries = append(entries, L3.translate_deleted_route(routeData))
	entries = L3.translate_deleted_route(routeData)
	//}
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}

func handleNexthopAdded(nexthop interface{}) {
	var entries []interface{}
	nexthopData, _ := nexthop.(nm.Nexthop_struct)
	// for _, decoder := range decoders {
	// entries = append(entries, L3.translate_added_nexthop(nexthopData))
	// entries = append(entries, Vxlan.translate_added_nexthop(nexthopData))
	entries = L3.translate_added_nexthop(nexthopData)
	//}

	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Vxlan.translate_added_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}
func handleNexthopUpdated(nexthop interface{}) {
	var entries []interface{}
	nexthopData, _ := nexthop.(nm.Nexthop_struct)
	// for _, decoder := range decoders {
	// entries = append(entries, L3.translate_deleted_nexthop(nexthopData))
	// entries = append(entries, Vxlan.translate_deleted_nexthop(nexthopData))
	//}
	entries = L3.translate_deleted_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Vxlan.translate_deleted_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	// for _, decoder := range decoders {
	// entries = append(entries, L3.translate_added_nexthop(nexthopData))
	// entries = append(entries, Vxlan.translate_added_nexthop(nexthopData))
	//}
	entries = L3.translate_added_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Vxlan.translate_added_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}

func handleNexthopDeleted(nexthop interface{}) {
	var entries []interface{}
	nexthopData, _ := nexthop.(nm.Nexthop_struct)
	// nexthopData, ok := nexthop.(nm.Nexthop)
	// for _, decoder := range decoders {
	// entries = append(entries, L3.translate_deleted_nexthop(nexthopData))
	// entries = append(entries, Vxlan.translate_deleted_nexthop(nexthopData))
	//}
	entries = L3.translate_deleted_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Vxlan.translate_deleted_nexthop(nexthopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}
func handleFbdEntryAdded(fbdEntry interface{}) {
	var entries []interface{}
	fbdEntryData, _ := fbdEntry.(nm.FdbEntry_struct)
	// for _, decoder := range decoders {
	// entries = append(entries, Vxlan.translate_added_fdb(fbdEntryData))
	// entries = append(entries, Pod.translate_added_fdb(fbdEntryData))
	//}
	entries = Vxlan.translate_added_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_added_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}

func handleFbdEntryUpdated(fdbEntry interface{}) {
	var entries []interface{}
	fbdEntryData, _ := fdbEntry.(nm.FdbEntry_struct)
	// for _, decoder := range decoders {
	// entries = append(entries, Vxlan.translate_deleted_fdb(fbdEntryData))
	// entries = append(entries, Pod.translate_deleted_fdb(fbdEntryData))
	// }
	entries = Vxlan.translate_deleted_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_deleted_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}

	// for _, decoder := range decoders {
	// entries = append(entries, Vxlan.translate_added_fdb(fbdEntryData))
	// entries = append(entries, Pod.translate_added_fdb(fbdEntryData))
	//}
	entries = Vxlan.translate_added_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_added_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}
func handleFbdEntryDeleted(fdbEntry interface{}) {
	var entries []interface{}
	fbdEntryData, _ := fdbEntry.(nm.FdbEntry_struct)
	// for _, decoder := range decoders {
	// entries = append(entries, Vxlan.translate_deleted_fdb(fbdEntryData))
	// entries = append(entries, Pod.translate_deleted_fdb(fbdEntryData))
	//}
	entries = Vxlan.translate_deleted_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_deleted_fdb(fbdEntryData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}

func handleL2NexthopAdded(l2NextHop interface{}) {
	var entries []interface{}
	l2NextHopData, _ := l2NextHop.(nm.L2Nexthop_struct)

	// for _, decoder := range decoders {
	// entries = append(entries, Vxlan.translate_added_l2_nexthop(l2NextHopData))
	// entries = append(entries, Pod.translate_added_l2_nexthop(l2NextHopData))
	//}
	entries = Vxlan.translate_added_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_added_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}
func handleL2NexthopUpdated(l2NextHop interface{}) {
	var entries []interface{}
	l2NextHopData, _ := l2NextHop.(nm.L2Nexthop_struct)
	//        for _, decoder := range decoders {
	// entries = append(entries, Vxlan.translate_deleted_l2_nexthop(l2NextHopData))
	// entries = append(entries, Pod.translate_deleted_l2_nexthop(l2NextHopData))
	//      }
	entries = Vxlan.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	//        for _, decoder := range decoders {
	// entries = append(entries, Vxlan.translate_added_l2_nexthop(l2NextHopData))
	// entries = append(entries, Pod.translate_added_l2_nexthop(l2NextHopData))
	//      }
	entries = Vxlan.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}

func handleL2NexthopDeleted(l2NextHop interface{}) {
	var entries []interface{}
	l2NextHopData, _ := l2NextHop.(nm.L2Nexthop_struct)
	// for _, decoder := range decoders {
	// entries = append(entries, Vxlan.translate_deleted_l2_nexthop(l2NextHopData))
	// entries = append(entries, Pod.translate_deleted_l2_nexthop(l2NextHopData))
	//}
	entries = Vxlan.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	entries = Pod.translate_deleted_l2_nexthop(l2NextHopData)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}

// InfraDB event Handler
func (h *ModuleipuHandler) HandleEvent(eventType string, objectData *event_bus.ObjectData) {
	switch eventType {
	case "vrf":
		var comp common.Component
		VRF, err := infradb.GetVrf(objectData.Name)
		if err != nil {
			fmt.Printf("IPU: GetVRF error: %s %s\n", err, objectData.Name)
			return
		} else {
			fmt.Printf("IPU : GetVRF Name: %s\n", VRF.Name)
		}
		if objectData.ResourceVersion != VRF.ResourceVersion {
			fmt.Printf("IPU: Mismatch in resoruce version %+v\n and VRF resource version %+v\n", objectData.ResourceVersion, VRF.ResourceVersion)
			comp.Name = "ipu"
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
				if VRF.Status.Components[i].Name == "ipu" {
					comp = VRF.Status.Components[i]
				}
			}
		}
		if VRF.Status.VrfOperStatus != infradb.VRF_OPER_STATUS_TO_BE_DELETED {
			details, status := offload_vrf(VRF)
			if status == true {
				comp.Details = details
				comp.CompStatus = common.COMP_STATUS_SUCCESS
				comp.Name = "ipu"
				comp.Timer = 0
			} else {
				if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
					comp.Timer = 2 * time.Second
				} else {
					comp.Timer = comp.Timer * 2 * time.Second
				}
				comp.Name = "ipu"
				comp.CompStatus = common.COMP_STATUS_ERROR
			}
			fmt.Printf("ipu: %+v\n", comp)
			infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, VRF.Metadata, comp)
		} else {
			status := tear_down_vrf(VRF)
			if status == true {
				comp.CompStatus = common.COMP_STATUS_SUCCESS
				comp.Name = "ipu"
				comp.Timer = 0
			} else {
				comp.CompStatus = common.COMP_STATUS_ERROR
				comp.Name = "ipu"
				if comp.Timer == 0 { // wait timer is 2 powerof natural numbers ex : 1,2,3...
					comp.Timer = 2
				} else {
					comp.Timer = comp.Timer * 2
				}
			}
			fmt.Printf("ipu: %+v\n", comp)
			infradb.UpdateVrfStatus(objectData.Name, objectData.ResourceVersion, objectData.NotificationId, nil, comp)
		}
	case "svi":
		// handlesvi(objectData.Name)
	default:
		fmt.Println("error: Unknown event type %s", eventType)
	}
}

func offload_vrf(VRF *infradb.Vrf) (string, bool) {
	if path.Base(VRF.Name) == "GRD" {
		return "", true
	}
	var entries []interface{}
	entries = Vxlan.translate_added_vrf(VRF)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry:-", e)
			return "", false
		}
	}
	return "", true
}

func tear_down_vrf(VRF *infradb.Vrf) bool {
	if path.Base(VRF.Name) == "GRD" {
		return true
	}
	var entries []interface{}
	entries = Vxlan.translate_deleted_vrf(VRF)
	for _, entry := range entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
			return false
		}
	}
	return true
}

func Init() {
	// Netlink Listener
	startSubscriber(nm.EventBus, "route_added")

	startSubscriber(nm.EventBus, "route_updated")
	startSubscriber(nm.EventBus, "route_deleted")
	startSubscriber(nm.EventBus, "nexthop_added")
	startSubscriber(nm.EventBus, "nexthop_updated")
	startSubscriber(nm.EventBus, "nexthop_deleted")
	startSubscriber(nm.EventBus, "fdb_entry_added")
	startSubscriber(nm.EventBus, "fdb_entry_updated")
	startSubscriber(nm.EventBus, "fdb_entry_deleted")
	startSubscriber(nm.EventBus, "l2_nexthop_added")
	startSubscriber(nm.EventBus, "l2_nexthop_updated")
	startSubscriber(nm.EventBus, "l2_nexthop_deleted")

	// InfraDB Listener

	/*config, err := readConfig("config.yaml")
	if err != nil {
		log.Println(err)
	}*/
	eb := event_bus.EBus
	for _, subscriberConfig := range config.GlobalConfig.Subscribers {
		if subscriberConfig.Name == "ipu" {
			for _, eventType := range subscriberConfig.Events {
				eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModuleipuHandler{})
			}
		}
	}
	// Setup p4runtime connection
	Conn, err := grpc.Dial(defaultAddr, grpc.WithInsecure())
	if err != nil {
		log.Fatalf("Cannot connect to server: %v", err)
	}
	// read config and load the pipeline using p4runtime
	/*configFile, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		fmt.Println("Error reading config file:", err)
		return
	}
	var configMap map[string]interface{}
	err = yaml.Unmarshal(configFile, &configMap)
	if err != nil {
		fmt.Println("Error parsing config:", err)
		return
	}
	p4 := configMap["p4"].(map[interface{}]interface{})
	p4config := p4["config"].(map[interface{}]interface{})
	infoFile, ok := p4config["p4info_file"].(string)
	if !ok {
		log.Fatal("Error accessing info_file")
	}
	binFile, ok := p4config["bin_file"].(string)
	if !ok {
		log.Fatal("Error accessing bin_file")
	}*/

	err1 := p4client.NewP4RuntimeClient(config.GlobalConfig.P4.Config.Bin_file, config.GlobalConfig.P4.Config.P4info_file, Conn)
	if err1 != nil {
		log.Fatalf("Failed to create P4Runtime client: %v", err1)
	}
	// add static rules into the pipeline of representators read from config
	representors := make(map[string][2]string)
	/*for k, v := range p4["representors"].(map[interface{}]interface{}) {
		vsi, mac, err := ids_of(v.(string))
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		representors[k.(string)] = [2]string{vsi, mac}
	}*/
	for k, v := range config.GlobalConfig.P4.Representors {
		vsi, mac, err := ids_of(v.(string))
		if err != nil {
			fmt.Println("Error:", err)
			return
		}
		representors[k] = [2]string{vsi, mac}
	}
	fmt.Println(" REPRESENTORS %+v", representors)
	L3 = L3.L3DecoderInit(representors)
	Pod = Pod.PodDecoderInit(representors)
	// decoders = []interface{}{L3, Vxlan, Pod}
	Vxlan = Vxlan.VxlanDecoderInit(representors)
	L3entries := L3.Static_additions()
	for _, entry := range L3entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	Podentries := Pod.Static_additions()
	for _, entry := range Podentries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Add_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
}

func Exit() {
	L3entries := L3.Static_deletions()
	for _, entry := range L3entries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
	}
	Podentries := Pod.Static_deletions()
	for _, entry := range Podentries {
		if e, ok := entry.(p4client.TableEntry); ok {
			p4client.Del_entry(e)
		} else {
			fmt.Println("Entry is not of type p4client.TableEntry")
		}
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
