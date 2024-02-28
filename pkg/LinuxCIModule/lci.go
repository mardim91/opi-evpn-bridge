package LinuxGeneralModule
import (
        "fmt"
        "io/ioutil"
        "log"
        "reflect"
        "time"
        "github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
        "github.com/opiproject/opi-evpn-bridge/pkg/infradb"
        "github.com/opiproject/opi-evpn-bridge/pkg/infradb/common"
	nl "github.com/vishvananda/netlink"
        "gopkg.in/yaml.v2"
        "os/exec"
        "encoding/json"
        "math/rand"
        "encoding/binary"
        "net"
        "strings"
        "strconv"
        "github.com/opiproject/opi-evpn-bridge/pkg/utils"
        "context"
)

type ModulelciHandler struct{}

type SubscriberConfig struct {
        Name     string   `yaml:"name"`
        Priority int      `yaml:"priority"`
        Events   []string `yaml:"events"`
}

type Config struct {
    Subscribers []SubscriberConfig `yaml:"subscribers"`
}

func (h *ModulelciHandler) HandleEvent(eventType string, objectData *event_bus.ObjectData) {
        switch eventType {
	case "bp":
		fmt.Printf("LCI recevied %s %s\n",eventType,objectData.Name)
		handlebp(objectData)
        default:
                fmt.Println("LCI: error: Unknown event type %s", eventType)
}
}


func handlebp(objectData *event_bus.ObjectData){
	var comp common.Component
	BP, err := infradb.GetBP(objectData.Name)
	if err != nil {
                fmt.Printf("LCI : GetBP error: %s\n", err)
                return
        }
	if (len(BP.Status.Components) != 0 ){
                for i:=0;i<len(BP.Status.Components);i++ {
                        if (BP.Status.Components[i].Name == "lci") {
                                comp = BP.Status.Components[i]
                        }
                }
        }
	if (BP.Status.BPOperStatus !=infradb.BP_OPER_STATUS_TO_BE_DELETED){
		status := set_up_bp(BP)
		comp.Name= "lci"
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
		fmt.Printf("LCI: %+v \n",comp)
		infradb.UpdateBpStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationId,BP.Metadata,comp)
	}
	else {
		status := tear_down_bp(BP)
		comp.Name= "lci"
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
		fmt.Printf("LCI: %+v \n",comp)
		infradb.UpdateBPStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationId,nil,comp)
	}
}

func set_up_bp(BP *infradb.BridgePort)(bool){
	resourceID := path.Base(BP.Name)
	bridge,err := nlink.LinkByName(ctx,"br-tenant")
	if err != nil{
		fmt.Printf("LCI: Unable to find key br-tenant\n")
		return false
	}
	iface, err := nlink.LinkByName(ctx, resourceID)
	if err != nil {
		fmt.Printf("LCI: Unable to find key %s\n", resourceID)
		return false
	}
	if err:= nlink.LinkSetMaster(ctx, iface, bridge); err != nil {
		fmt.Printf("LCI: Failed to add iface to bridge: %v", err)
		return false
	}
	for _,bridgeRefName := range BP.Spec.LogicalBridges {
		BrObj,err := infradb.GetLB(bridgeRefName)
		if err != nil {
			fmt.Printf("LCI: unable to find key %s and error is %v", bridgeRefName,err)
			return false
		}
		vid := uint16(BrObj.Spec.VlanId)
		switch BP.Spec.Ptype {
			case infradb.ACCESS:
				if err := nlink.BridgeVlanAdd(ctx, iface, vid, true, true, false, false); err != nil {
					fmt.Printf("Failed to add vlan to bridge: %v", err)
					return false
				}
			case infradb.TRUNK:
			// Example: bridge vlan add dev eth2 vid 20
				if err := nlink.BridgeVlanAdd(ctx, iface, vid, false, false, false, false); err != nil {
					fmt.Printf("Failed to add vlan to bridge: %v", err)
					return false
				}
			default:
				fmt.printf("Only ACCESS or TRUNK supported and not (%d)", BP.Spec.Ptype)
				return false
		}
	}
	if err := nlink.LinkSetUp(ctx, iface); err != nil {
		fmt.Printf("Failed to up iface link: %v", err)
		return false
	}
	return true
}

func tear_down_bp(BP *infradb.BridgePort)(bool){
	resourceID := path.Base(BP.Name)
	iface, err := nlink.LinkByName(ctx, resourceID)
	if err != nil {
		fmt.Printf("LCI: Unable to find key %s\n", resourceID)
		return false
	}
	if err := nlink.LinkSetDown(ctx, iface); err != nil {
		fmt.Printf("LCI: Failed to down link: %v", err)
		return false
	}
	for _,bridgeRefName := range BP.Spec.LogicalBridges {
		BrObj,err := infradb.GetLB(bridgeRefName)
		if err != nil {
			fmt.Printf("LCI: unable to find key %s and error is %v", bridgeRefName,err)
			return false
		}
		vid := uint16(BrObj.Spec.VlanId)
		if err := nlink.BridgeVlanDel(ctx, iface, vid, true, true, false, false); err != nil {
			fmt.Printf("LCI: Failed to delete vlan to bridge: %v", err)
			return false
		}
	}
	if err := nlink.LinkDel(ctx, iface); err != nil {
		fmt.Printf("Failed to delete link: %v", err)
		return false
	}
	return true
}
var ctx context.Context
var nlink utils.Netlink
func Init() {
        config, err := readConfig("config.yaml")
        if err != nil {
                log.Fatal(err)
        }
        eb := event_bus.EBus
        for _, subscriberConfig := range config.Subscribers {
                if subscriberConfig.Name == "lci" {
                        for _, eventType := range subscriberConfig.Events {
                                eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModulelciHandler{})
                        }
                }
        }
	ctx = context.Background()
	nlink = utils.NewNetlinkWrapper()
}
