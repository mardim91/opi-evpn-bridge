package LinuxGeneralModule
import (
        "fmt"
        "io/ioutil"
        "log"
	"reflect"
	"time"
        "github.com/opiproject/opi-evpn-bridge/pkg/infradb/subsrciber_framework/event_bus"
        "github.com/opiproject/opi-evpn-bridge/pkg/infradb"
        "gopkg.in/yaml.v2"
        "os/exec"
	"encoding/json"
	"math/rand"
	"encoding/binary"
	"net"
        "strings"
        "strconv"
)

type ModulelvmHandler struct{}

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

func run(cmd []string,flag bool) (string, int) {
    var out []byte
    var err error
    out, err = exec.Command("sudo",cmd...).CombinedOutput()
    if err != nil {
            if flag {
                   panic(fmt.Sprintf("LGM: Command %s': exit code %s;",out,err.Error()))
            }
            fmt.Printf("LGM: Command %s': exit code %s;",out,err)
            return "Error",-1
    }
    output := string(out[:])
    return output,0
}


func (h *ModulelvmHandler) HandleEvent(eventType string, eventData *event_bus.EventData) {
        switch eventType {
        case "VRF":
		var comp infradb.Component
			VRF,err := infradb.GetVrf(eventData.Name)
			if err != nil {
				fmt.Printf("LGM: GetVRF error: %s %s\n", err,eventData.Name)
					return
			} else {
				fmt.Printf("LGM : GetVRF Name: %s\n", VRF.Name)
			}
		if (VRF.Status.VrfOperStatus !=infradb.VRF_OPER_STATUS_TO_BE_DELETED){
                        detail,status := set_up_vrf(&VRF)
					 if (status == "Success") {
                                                comp.Details= detail
                                                comp.CompStatus= infradb.COMP_STATUS_SUCCESS
                                                comp.Name= "LGM"
                                                comp.Timer = 0
                                         } else {
                                                if comp.Timer ==0 {  // wait timer is 2 powerof natural numbers ex : 1,2,3...
                                                        comp.Timer=2
                                                } else {
                                                        comp.Timer=comp.Timer*2
                                                }
                                                comp.CompStatus= infradb.COMP_STATUS_ERROR
                                         }
                                fmt.Printf(" LGM: %+v\n",comp)
                                        infradb.UpdateVrfStatus(eventData.Name,eventData.ResourceVer,comp)
		} else {
		 tear_down_vrf(&VRF)
		}
        case "SVI":
                //handlesvi(eventData.Name)
        default:
                fmt.Println("LGM: error: Unknown event type %s", eventType)
}
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


var default_vtep string
var ip_mtu int 
func Init() {
        config, err := readConfig("config.yaml")
        if err != nil {
                log.Fatal(err)
        }
        eb := event_bus.EBus
        for _, subscriberConfig := range config.Subscribers {
                if subscriberConfig.Name == "lgm" {
                        for _, eventType := range subscriberConfig.Events {
                                eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModulelvmHandler{})
                	}
        	}
	}
	default_vtep = config.Linux_frr.Default_vtep
	ip_mtu = config.Linux_frr.Ip_mtu

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
            break
	}
    }
    if !rp_filter_disabled{
        fmt.Sprintf("Failed to disable rp_filtering on interface %s\n",Interface)
    }
}



func routing_table_busy(table uint32) bool{
    CP,err := run([]string{"ip","route","show","table", strconv.Itoa(int(table))}, false)
    if (err != 0){
         fmt.Println(CP)
         return false
    }
    return true //reflect.ValueOf(CP).IsZero() && len(CP)!= 0
}


func set_up_vrf(VRF *infradb.Vrf)(string,string) {
	vtip := fmt.Sprintf("%+v",VRF.Spec.VtepIP)
	routing_table  := fmt.Sprintf("%+v",VRF.Spec.RoutingTable) 	
	Ip_Mtu := fmt.Sprintf("%+v",ip_mtu) 	
	if VRF.Name == "GRD"{
                return "", "Error"
        }
	if !routing_table_busy(VRF.Spec.RoutingTable) {
		fmt.Printf("LGM :Routing table %s is not empty\n",VRF.Spec.RoutingTable)	
		return "","Error"
	}
	if reflect.ValueOf(VRF.Spec.VtepIP).IsZero(){
        	// Verify that the specified VTEP IP exists as local IP
	        _,err := run([]string{"ip","route","list","exact", vtip,"table","local"}, false)
        	if (err != 0) {
	            	fmt.Printf(" LGM: VTEP IP not found: %+v\n",VRF.Spec.VtepIP)
			return "" , "Error"
		}
	} else {
        // Pick the IP of interface default VTEP interface
        	//fmt.Printf("LGM: VTEP iP %+v\n",get_ip_address(default_vtep))
        	VRF.Spec.VtepIP = get_ip_address(default_vtep)
	}
	// Create the VRF interface for the specified routing table and add loopback address
    	CP,err :=run([]string{"ip","link","add",VRF.Name,"type","vrf","table",routing_table},false)
	if err!=0 {
		fmt.Printf("LGM: Error in exectuing command %s %s","link add VRF type vrf table ",routing_table)
		fmt.Printf("%s\n",CP)	
		return "", "Error"	
	}
        CP,err = run([]string{"ip","link","set",VRF.Name,"up","mtu",Ip_Mtu},false)
	if err!=0 {
		fmt.Printf("LGM:Error in exectuing command %s %s","link set VRF MTU ",Ip_Mtu)	
		fmt.Printf("%s\n",CP)	
		return "", "Error"	
	}
	CP,err =run([]string{"ip","address","add",string(VRF.Spec.LoopbackIP.IP),"dev",VRF.Name},false)
	if err!=0 {
		fmt.Printf("LGM: Error in exectuing command %s %s","address add LoopbackIP",string(VRF.Spec.LoopbackIP.IP))	
		fmt.Printf("%s\n",CP)	
		return "", "Error"	
	}
    //Add low-prio default route. Otherwise a miss leads to lookup in the next higher table
        CP,err =run([]string{"ip","route","add","throw","default","table",routing_table,"proto","ipu_infra_mgr","metric","9999"},false)
	if err!=0 {
		fmt.Printf("LGM: Error in exectuing command %s %s","route add throw default table ",routing_table)	
		fmt.Printf("%s\n",CP)	
		return "", "Error"	
	}
	// Disable reverse-path filtering to accept ingress traffic punted by the pipeline
	disable_rp_filter("rep-"+VRF.Name)
   // Configuration specific for VRFs associated with L3 EVPN
    if (!reflect.ValueOf(VRF.Spec.Vni).IsZero()){
        // Create bridge for external VXLAN under VRF
        // Linux apparently creates a deterministic MAC address for a bridge type link with a given
        // name. We need to assign a true random MAC address to avoid collisions when pairing two
        // IPU servers.
        rmac := fmt.Sprintf("%+v",GenerateMac()) // str(macaddress.MAC(b'\x00'+random.randbytes(5))).replace("-", ":")
	CP, err:=run([]string{"ip","link","add","br-"+VRF.Name,"address",rmac,"type","bridge"},false)
	if err !=0 {
		fmt.Printf("LGM: Error in exectuing command %s %s\n","ip link add address rmac",CP)
		return "","Error"	
	}
        CP,err = run([]string{"ip","link","set","br-"+VRF.Name,"master",VRF.Name,"up","mtu",Ip_Mtu},false)
         if err !=0 {
                fmt.Printf("LGM: Error in exectuing command %s %s\n","ip link set master VRF mtu",CP)
                return "","Error"
        }	
	// Create the VXLAN link in the external bridge
         CP,err = run([]string{"ip","link","add","vxlan-"+VRF.Name,"type","vxlan","id",string(VRF.Spec.Vni),"local", vtip,"dstport","4789","nolearning proxy"},false)
	if err !=0 {
                fmt.Printf("LGM: Error in exectuing command %s %s\n","ip link add vxlan id local VTEIP dstport",CP)
                return "","Error"
        }
        CP,err = run([]string{"ip","link","set","vxlan-"+VRF.Name,"master","br-"+VRF.Name,"up","mtu",Ip_Mtu},false)
	if err !=0 {
                fmt.Printf("LGM: Error in exectuing command %s %s\n","ip link set master BR up mtu",CP)
                return "","Error"
        }
   }	
	return "", "Success"	
}

func GenerateMac() (net.HardwareAddr) {
        buf := make([]byte, 6)
        var mac net.HardwareAddr
        _, err  := rand.Read(buf)
        if err != nil {}
	
        // Set the local bit 
        buf[0] |= 2

        mac = append(mac, buf[0], buf[1], buf[2], buf[3], buf[4], buf[5])

        return mac 
} 


type Addr_show_dev struct{
	Ifindex int 
	Ifname string
	Flags []string
	Mtu int
	Qdisc string 
	Operstate string
	Group string
	Txqlen int
	Link_type string
	Address string
	Broadcast string
	Addr_info []AddrInfo
}

type AddrInfo struct{
	Family string
	Local string
	Prefixlen int
	Broadcast string
	Scope string
	Noprefixroute bool
	Label string
	Valid_life_time uint64
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
	//var netmaskint [4]int
	netmaskint[0], _ = strconv.ParseInt(oct1, 2, 64)
	netmaskint[1], _ = strconv.ParseInt(oct2, 2, 64)
	netmaskint[2], _ = strconv.ParseInt(oct3, 2, 64)
	netmaskint[3], _ = strconv.ParseInt(oct4, 2, 64)
	
	//netmaskstring = strconv.Itoa(int(ii1)) + "." + strconv.Itoa(int(ii2)) + "." + strconv.Itoa(int(ii3)) + "." + strconv.Itoa(int(ii4))
	return netmaskint
}

type valid_ip struct {
	IP string
	Mask int	
}	

func get_ip_address(dev string)net.IPNet{
	var valid_ips  []valid_ip
	CP,err := run([]string{"ip","-j","address","show","dev",dev},false)
		if (err !=0){
			fmt.Printf("LGM:Error in executing \n")	
				return  net.IPNet{
         			       		IP: net.ParseIP("0.0.0.0"),
       					 } 
		}
	//Res := CP[2:len(CP)-3]
	Res := strings.Split(CP[2:len(CP)-3], "]},{")
	fmt.Printf("JSON1 %+v \n",Res[0])
	// From the only interface in the list pick the first IP address
	// outside 127.0.0.0/8 loopback network.
	for i := 0; i<len(Res); i++{
		var Asd Addr_show_dev
		err := json.Unmarshal([]byte(fmt.Sprintf("{%v}",Res[i])), &Asd)
		if err != nil{
			log.Println("error-",err)
		}
		//var ips []string
		for addr:=0; addr<len(Asd.Addr_info); addr++ {
			//ips=append(ips,Asd.Addr_info[addr].Local)
			if (Asd.Addr_info[addr].Local != "127.0.0.0/8") {
			      var VIp valid_ip
			      VIp.IP = Asd.Addr_info[addr].Local
			      VIp.Mask = Asd.Addr_info[addr].Prefixlen		 	
			      valid_ips = append(valid_ips,VIp)
			}
		}
	}
	mtoip := NetMaskToInt(valid_ips[0].Mask)
       b3 := make([]byte,8)  // Converting int64 to byte
       binary.LittleEndian.PutUint64(b3, uint64(mtoip[3]))
       b2 := make([]byte,8)
       binary.LittleEndian.PutUint64(b2, uint64(mtoip[2]))
       b1 := make([]byte,8)
       binary.LittleEndian.PutUint64(b1, uint64(mtoip[1]))
       b0 := make([]byte,8)
       binary.LittleEndian.PutUint64(b0, uint64(mtoip[0]))
       nIP := net.IPNet{
		IP: net.ParseIP(valid_ips[0].IP),
	    	Mask: net.IPv4Mask(b0[0],b1[0],b2[0],b3[0]),
	} 
       return nIP	
}


func tear_down_vrf(VRF *infradb.Vrf)(string,string) {
	if VRF.Name == "GRD"{
                return "", "Error"
        }
    // Delete the Linux networking artefacts in reverse order
    run([]string{"ip","link","delete","rep-"+VRF.Name},false)
    if (!reflect.ValueOf(VRF.Spec.Vni).IsZero()){
        CP,err :=run([]string{"ip","link","delete","vxlan-"+VRF.Name}, false)
	if err !=0 {
                fmt.Printf("LGM: Error in exectuing command %s %s\n","ip link deleted vxlan ",CP)
                return "","Error"
        }
        CP,err =run([]string{"ip","link","delete","br-"+VRF.Name},false)
	if err !=0 {
                fmt.Printf("LGM: Error in exectuing command %s %s\n","ip link delete br-vrf ",CP)
                return "","Error"
        }
        CP,err =run([]string{"ip","route","flush","table",string(VRF.Spec.RoutingTable)},false)
	if err !=0 {
                fmt.Printf("LGM: Error in exectuing command %s %s\n","ip route flush table",CP)
                return "","Error"
        }
        CP,err =run([]string{"ip","link","delete",VRF.Name},false)
	if err !=0 {
                fmt.Printf("LGM: Error in exectuing command %s %s\n","ip link delete VRF",CP)
                return "","Error"
        }
    }	

	return "","Success"
}
