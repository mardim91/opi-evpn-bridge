
package frr
//package main // frr_module

import(
    "fmt"
    "gopkg.in/yaml.v3"
    "os/exec"
    "strings"
    "log"
    "strconv"
    "io/ioutil"
    "reflect"
    "time"
    "net"
    "encoding/json"	
    "github.com/opiproject/opi-evpn-bridge/pkg/infradb/subscriber_framework/event_bus"
    "github.com/opiproject/opi-evpn-bridge/pkg/infradb" 
    "github.com/opiproject/opi-evpn-bridge/pkg/infradb/common" 
)

type SubscriberConfig struct {
    Name     string   `yaml:"name"`
    Priority int      `yaml:"priority"`
    Events   []string `yaml:"events"`
}

/*
type Config_t struct {
	P4 struct {
		Enable bool `yaml:"enabled"`
	} `yaml: "p4"`
	Frr_module struct {
		Enable       bool   `yaml:"enabled"`
		Default_vtep string `yaml:"default_vtep"`
		Port_mux     string `yaml:"port_mux"`
		Vrf_mux      string `yaml:"vrf_mux"`
		Br_tenant    int    `yaml:"br_tenant"`
	} `yaml:"linux_frr"`
	Netlink struct {
		Enable        bool `yaml:"enabled"`
		Poll_interval int  `yaml:"poll_interval"`
		Phy_ports     []struct {
			Name string `yaml:"name"`
			Vsi  int    `yaml:"vsi"`
		} `yaml:"phy_ports"`
	} `yaml:"netlink"`
}
*/
type Linux_frrConfig struct {
		Enable       bool   `yaml:"enabled"`
		Default_vtep string `yaml:"default_vtep"`
		Port_mux     string `yaml:"port_mux"`
		Vrf_mux      string `yaml:"vrf_mux"`
		Br_tenant    int    `yaml:"br_tenant"`
}

type Config struct {
    Subscribers []SubscriberConfig `yaml:"subscribers"`
    Linux_frr Linux_frrConfig `yaml:"linux_frr"`	    
}

type ModulefrrHandler struct{}


func (h *ModulefrrHandler) HandleEvent(eventType string, objectData *event_bus.ObjectData) {
	switch eventType {
		case "vrf"://"VRF_added":
			  fmt.Printf("FRR recevied %s %s\n",eventType,objectData.Name)
	                  handlevrf(objectData)
		case "svi":
			  fmt.Printf("FRR recevied %s %s\n",eventType,objectData.Name)
	                  handlesvi(objectData)
		default:
			fmt.Println("error: Unknown event type %s", eventType)
	}
}


func handlesvi(objectData *event_bus.ObjectData){
			SVI,_ := infradb.GetSvi(objectData.Name)
	 //          if (SVI.componant.operstatus!="TO_BE_DELETE"){
                                set_up_svi(&SVI)
                                //          } else {
                                //    case "SVI_deleted":
                                tear_down_svi(&SVI)
                                //          }
}	

func handlevrf(objectData *event_bus.ObjectData){
			var comp common.Component
				VRF,err := infradb.GetVrf(objectData.Name)
				if err != nil {
					fmt.Printf("GetVRF error: %s %s\n", err,objectData.Name)
						return
				} else {
					fmt.Printf("FRR :GetVRF Name: %s\n", VRF.Name)
				}
				if (len(VRF.Status.Components) != 0 ){
        		            for i:=0;i<len(VRF.Status.Components);i++ {
	                              if (VRF.Status.Components[i].Name == "frr") {
                                 	 comp = VRF.Status.Components[i]
                       		       }
                		    }
		                }
			if (VRF.Status.VrfOperStatus !=infradb.VRF_OPER_STATUS_TO_BE_DELETED){
				detail,status := set_up_vrf(VRF)
						comp.Name= "frr"
					if (status == true) {
						comp.Details= detail
						comp.CompStatus= common.COMP_STATUS_SUCCESS
						comp.Timer = 0
					} else {
						if comp.Timer ==0 {  // wait timer is 2 powerof natural numbers ex : 1,2,3... 	
							comp.Timer=2 * time.Second
						} else {
							comp.Timer=comp.Timer*2
						}
						comp.CompStatus= common.COMP_STATUS_ERROR
					}
				fmt.Printf("%+v\n",comp) 	
					infradb.UpdateVrfStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationId,nil,comp)
			} else {
				status :=tear_down_vrf(VRF)
                                                comp.Name= "frr"
				 if (status == true) {
                                                comp.CompStatus= common.COMP_STATUS_SUCCESS
                                                comp.Timer = 0
                                        } else {
                                                if comp.Timer ==0 {  // wait timer is 2 powerof natural numbers ex : 1,2,3...
                                                        comp.Timer=2 * time.Second

                                                } else {
                                                        comp.Timer=comp.Timer*2
                                                }
                                                comp.CompStatus= common.COMP_STATUS_ERROR
                                        }
                        	        fmt.Printf("%+v\n",comp)
                                        infradb.UpdateVrfStatus(objectData.Name,objectData.ResourceVersion,objectData.NotificationId,nil,comp)
			}
}			

func run(cmd []string,flag bool) (string, int) {
//  fmt.Println("FRR: Executing command", cmd)
    var out []byte
    var err error
//  out, err = exec.Command("sudo",cmd...).Output()
    out, err = exec.Command("sudo",cmd...).CombinedOutput()
    if err != nil {
            if flag == true {
		   panic(fmt.Sprintf("FRR: Command %s': exit code %s;",out,err.Error()))
            }
	    fmt.Printf("FRR: Command %s': exit code %s;",out,err)	
	    return "Error",-1	
    }
    output := string(out[:])
    return output,0
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



var logger, default_vtep, port_mux, vrf_mux string
var br_tenant int
func subscribe_infradb(config *Config) {

    eb := event_bus.EBus
    for _, subscriberConfig := range config.Subscribers {
            if subscriberConfig.Name == "frr" {
                    for _, eventType := range subscriberConfig.Events {
                            eb.StartSubscriber(subscriberConfig.Name, eventType, subscriberConfig.Priority, &ModulefrrHandler{})
            }
        }
    }
}    

 
func  set_up_tenant_bridge() {
//	run([]string{"ip","-br","l"},false)
	run([]string{"ip","link","add",/*strconv.Itoa(br_tenant)*/"br_tenant","type","bridge","vlan_default_pvid","0","vlan_filtering","1","vlan_protocol", "802.1Q"},false)
//	fmt.Println("Venky ",CP,err)
        run([]string{"ip","link","set","br_tenant",/*"strconv.Itoa(br_tenant)",*/"up"},false)
	//fmt.Println("Venky1 ",CP,err)
}   

//func main(){
func Init(){
	config, err := readConfig("config.yaml")
	if err != nil {
		log.Fatal(err)
		//os.Exit(0)
	}
	frr_enabled := config.Linux_frr.Enable
	if frr_enabled != true {
		log.Println("FRR Module disabled")
		return
	}
	default_vtep = config.Linux_frr.Default_vtep
	br_tenant = config.Linux_frr.Br_tenant
	port_mux = config.Linux_frr.Port_mux
	vrf_mux = config.Linux_frr.Vrf_mux
	// Subscribe to InfraDB notifications
	subscribe_infradb(config)
	//Set up the static configuration parts
	set_up_tenant_bridge()
	//Make sure IPv4 forwarding is enabled.
	run([]string{"sysctl","-w"," net.ipv4.ip_forward=1"},false)
}


func routing_table_busy(table uint32) bool{
    CP,err := run([]string{"ip","route","show","table", strconv.Itoa(int(table))}, false)
    if (err != 0){
	 fmt.Println(CP)	
	 return false
    }
    //fmt.Printf("route table busy %s %s\n",CP,err)
    // Table is busy if it exists and contains some routes
    return true //reflect.ValueOf(CP).IsZero() && len(CP)!= 0
}

type VRF struct{
	Name string
	Vni int
	Routing_tables []uint32
	Loopback  net.IP 
	//Routing_tables uint32
}


type Bgp_l2vpn_cmd struct {
	Vni int 
	Type string 
	InKernel string
	Rd string
	OriginatorIp  string
	AdvertiseGatewayMacip string
	AdvertiseSviMacIp   string
	AdvertisePip   string
	SysIP string
	SysMac  string
	Rmac string
	ImportRts []string
	ExportRts []string
}
type route struct{}
type Bgp_vrf_cmd struct {
	VrfId int
	VrfName  string
	TableVersion uint
	RouterId  string
	DefaultLocPrf uint
	LocalAS int
	Routes route
}


func set_up_vrf(VRF *infradb.Vrf)(string,bool) {
	//This function must not be executed for the VRF representing the GRD
	Ifname := strings.Split(VRF.Name,"/")
	ifwlen := len(Ifname)
	VRF.Name  = Ifname[ifwlen-1]	
	if VRF.Name == "GRD"{
		return "", false
	}
        bgp_vrf_name := fmt.Sprintf("router bgp 65000 vrf %s",VRF.Name)
	if  (!reflect.ValueOf(VRF.Spec.Vni).IsZero()){
		// Configure the VRF in FRR and set up BGP EVPN for it
                  vrf_name := fmt.Sprintf("vrf %s",VRF.Name)
		  vni_id := fmt.Sprintf("vni %s", strconv.Itoa(int(VRF.Spec.Vni)))
		  CP ,err := run([]string{"vtysh","-c","conf","t","-c",vrf_name,"-c",vni_id,"-c","exit-vrf","-c","exit"},false)
		  if (err != 0 || check_frr_result(CP,false)){
			  fmt.Printf("FRR: Error in conf VRF/VNI conf VRF/VNI %s %s command %s\n",vrf_name,vni_id,CP)
				  return "",false
		  }
		  fmt.Printf("FRR: Executed frr config t %s %s exit-vrf exit\n",vrf_name,vni_id ) 	
	  var LbiP string
		  if (reflect.ValueOf(VRF.Spec.LoopbackIP).IsZero()){
			  LbiP = "0.0.0.0"	
		  } else {
			  LbiP = fmt.Sprintf("%+v",VRF.Spec.LoopbackIP.IP)
		  }
		      bgp_route_id := fmt.Sprintf("bgp router-id %s",LbiP)//VRF.Spec.LoopbackIP.String())
		      CP,err = run([]string{"vtysh","-c","conf","t","-c",bgp_vrf_name,"-c",bgp_route_id,"-c","no bgp ebgp-requires-policy","-c","no bgp hard-administrative-reset","-c","no bgp graceful-restart notification","-c","address-family ipv4 unicast","-c","redistribute connected","-c","redistribute static","-c","exit-address-family","-c","address-family l2vpn evpn","-c","advertise ipv4 unicast","-c","exit-address-family","-c","exit"},false)
		      if (err!=0 || check_frr_result(CP,false)){
			      fmt.Printf("FRR: Error in conf bgp command %s\n",CP)
				      return "",false
		      }
	       fmt.Printf("FRR: Executed config t bgp_vrf_name %s bgp_route_id %s no bgp ebgp-requires-policy exit-vrf exit\n",bgp_vrf_name,bgp_route_id) 	
	      // Update the VRF with attributes from FRR
cmd :=fmt.Sprintf("show bgp l2vpn evpn vni %s json", strconv.Itoa(int(VRF.Spec.Vni)))
	     CP,err =run([]string{"vtysh","-c",cmd},false)
	     if (err != 0 || check_frr_result(CP,true)){
		     fmt.Printf("FRR: Error in evpn evpn command %s\n",cmd)
			     return "",false
	     } 
	     fmt.Printf("FRR: Executed show bgp l2vpn evpn vni %s json\n",strconv.Itoa(int(VRF.Spec.Vni))) 	
	     if len(CP)!=7 {
		     CP = CP[2:len(CP)-2]
	     }else {
		     fmt.Printf("FRR: unable to get the command %s\n",cmd)
			     return "",false
	     }
	     var bgp_l2vpn Bgp_l2vpn_cmd
	     err1 := json.Unmarshal([]byte(fmt.Sprintf("{%v}",CP)), &bgp_l2vpn)
	     if err1 != nil{
		     log.Println("error-",err)
	     }
             cmd = fmt.Sprintf("show bgp vrf %s json",VRF.Name)
	     CP,err = run([]string{"vtysh", "-c",cmd},false)
	     if (err != 0 || check_frr_result(CP,true)){
		     fmt.Printf("FRR: Error in show bgp command %s\n",cmd)
			     return "",false
	     }
    // fmt.Printf("FRR CP4 %s \n",CP)
     var bgp_vrf Bgp_vrf_cmd
	     if len(CP)!=7 {
		     CP =CP[1:len(CP)-3]
	     }else {
		     fmt.Printf("FRR: unable to get the command \"%s\"\n",cmd)
			     return "",false
	     }
     err1 = json.Unmarshal([]byte(fmt.Sprintf("{%v}",CP)), &bgp_vrf)
	     if err1 != nil{
		     log.Println("error-",err)
	     }
	     fmt.Printf("FRR: Executed show bgp vrf %s json\n",VRF.Name) 	
details := fmt.Sprintf("{ \"rd\":\"%s\",\"rmac\":\"%s\",\"importRts\":[\"%s\"],\"exportRts\":[\"%s\"],\"localAS\":%d }",bgp_l2vpn.Rd,bgp_l2vpn.Rmac,bgp_l2vpn.ImportRts,bgp_l2vpn.ExportRts,bgp_vrf.LocalAS)
		 fmt.Printf("FRR Details %s\n",details)
		 return details,true 
	}
	return "",false
}

func check_frr_result(CP string,show bool)bool {
	return ( (show && reflect.ValueOf(CP).IsZero()) || strings.Contains(CP,"warning") || strings.Contains(CP,"unknown") || strings.Contains(CP,"Unknown") || strings.Contains(CP,"Warning") || strings.Contains(CP,"Ambiguous"))
}

func tear_down_vrf(VRF *infradb.Vrf)(bool) {//interface{}){
        //This function must not be executed for the VRF representing the GRD
	Ifname := strings.Split(VRF.Name,"/")
	ifwlen := len(Ifname)
	VRF.Name  = Ifname[ifwlen-1]	
	if VRF.Name == "GRD"{
		return false
	}
	// Clean up FRR last
	if  (!reflect.ValueOf(VRF.Spec.Vni).IsZero()){
		fmt.Printf("FRR %s\n","Deleted event")
			del_cmd1 := fmt.Sprintf("no router bgp 65000 vrf %s",VRF.Name)
			del_cmd2 := fmt.Sprintf("no vrf %s", VRF.Name)
			CP ,err := run([]string{"vtysh","-c","conf","t","-c",del_cmd1,"-c",del_cmd2,"-c","exit"},false)
			if (err == -1 || check_frr_result(CP,false)){
				fmt.Printf("FRR: Error in conf Delete VRF/VNI command %s\n",CP)
					return false
			}
 	} 
  return true
}

func set_up_svi(SVI *infradb.Svi){//interface{}){
//	fmt.Printf("FRR:  ADDED event received %f %s\n",SVI.ResourceVer,SVI.Name)
    

}

func tear_down_svi(SVI *infradb.Svi){//interface{}){
//	fmt.Printf("FRR: SVI deleted event received %f %s\n",SVI.ResourceVer,SVI.Name)

}

