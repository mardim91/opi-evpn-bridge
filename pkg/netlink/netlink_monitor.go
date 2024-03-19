// SPDX-License-Identifier: Apache-2.0
// Copyright (c) 2022-2023 Intel Corporation, or its subsidiaries.
// Copyright (C) 2023 Nordix Foundation.

// Package netlink handles the netlink related functionality
package netlink

import (
	//	"fmt"
	"fmt"
	"log"
	"os"
	"context"
	// "sync"
	"regexp"
	"strconv"
	"strings"
	"time"

	//	"unicode"
	"reflect"
	//	"sort"
	//        "unsafe"
	"encoding/binary"
	"encoding/json"
	"net"

	"golang.org/x/sys/unix"

	// ipu_db "xpu/ipu_db"
	vn "github.com/vishvananda/netlink"

	// "io/ioutil"
	"path"

	"github.com/opiproject/opi-evpn-bridge/pkg/config"
	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	eb "github.com/opiproject/opi-evpn-bridge/pkg/vendor_plugins/event_bus"
	"github.com/opiproject/opi-evpn-bridge/pkg/utils"
	// "gopkg.in/yaml.v3"
)

var ctx context.Context
var nlink utils.Netlink
// dbLock variable
// var dbLock int

// GRD variable
var GRD int

// pollInterval variable
var pollInterval int

// dbphyPortslock variable
var phyPorts = make(map[string]int)

// brTenant variable
// var brTenant string

// stopMonitoring variable
var stopMonitoring bool

// EventBus variable
var EventBus = eb.NewEventBus()

// strNone variable
var strNone = "NONE"

// Config_t Structure
/*type Config_t struct {
	P4 struct {
		Enable bool `yaml:"enabled"`
	} `yaml: "p4"`
	Linux_frr struct {
		Enable       bool   `yaml:"enabled"`
		Default_vtep string `yaml:"default_vtep"`
		Port_mux     string `yaml:"port_mux"`
		Vrf_mux      string `yaml:"vrf_mux"`
		Br_tenant    string `yaml:"brTenant"`
	} `yaml:"linux_frr"`
	Netlink struct {
		Enable        bool `yaml:"enabled"`
		Poll_interval int  `yaml:"pollInterval"`
		Phy_ports     []struct {
			Name string `yaml:"name"`
			Vsi  int    `yaml:"vsi"`
		} `yaml:"phyPorts"`
	} `yaml:"netlink"`
}
*/
// Route Direction
const ( // Route direction
	None int = iota
	RX
	TX
	RXTX
)

// Nexthop type
const ( // NexthopStruct TYPE & L2NEXTHOP TYPE & FDBentry
	PHY = iota
	SVI
	ACC
	VXLAN
	BRIDGEPORT
	OTHER
)

// RTNNeighbor
const (
	RTNNeighbor = 1111
)

// Subscribers    list[Subscriber] = []

// NeighKey strcture of neighbor
type NeighKey struct {
	Dst     string
	VrfName string
	Dev     int
}

// RouteKey structure of route description
type RouteKey struct {
	Table int
	Dst   string
}

// NexthopKey structure of nexthop
type NexthopKey struct {
	VrfName string
	Dst     string
	Dev     int
	Local   bool
}

// NeighIPStruct nighbor ip structure
type NeighIPStruct struct {
	Dst         string
	Dev         string
	Lladdr      string
	ExternLearn string
	State       []string
	Protocol    string
}

// FDBKey structure key for sorting theFDB entries
type FDBKey struct {
	VlanID int
	Mac    string
}

// L2NexthopKey is l2 neighbor key
type L2NexthopKey struct {
	Dev    string
	VlanID int
	Dst    string
}

// FdbIPStruct fdb ip structure
type FdbIPStruct struct {
	Mac    string
	Ifname string
	Vlan   int
	Flags  []string
	Master string
	State  string
	Dst    string
}

// Routes Variable
var Routes = make(map[RouteKey]RouteStruct)

// Nexthops Variable
var Nexthops = make(map[NexthopKey]NexthopStruct)

// Neighbors Variable
var Neighbors = make(map[NeighKey]NeighStruct)

// FDB Variable
var FDB = make(map[FDBKey]FdbEntryStruct)

// L2Nexthops Variable
var L2Nexthops = make(map[L2NexthopKey]L2NexthopStruct)

// LatestRoutes Variable
var LatestRoutes = make(map[RouteKey]RouteStruct)

// LatestNexthop Variable
var LatestNexthop = make(map[NexthopKey]NexthopStruct)

// LatestNeighbors Variable
var LatestNeighbors = make(map[NeighKey]NeighStruct)

// LatestFDB Variable
var LatestFDB = make(map[FDBKey]FdbEntryStruct)

// LatestL2Nexthop Variable
var LatestL2Nexthop = make(map[L2NexthopKey]L2NexthopStruct)
/*--------------------------------------------------------------------------
###  Route Database Entries
###
###  In the internal Route table, there is one entry per VRF and IP prefix
###  to be installed in the routing table of the P4 pipeline. If there are
###  multiple routes in the Linux  route database for the same VRF and
###  prefix, we pick the one with the lowest metric (as does the Linux
###  forwarding plane).
###  The key of the internal Route table consists of (vrf, dst prefix) and
###  corresponds to the match fields in the P4 routing table. The rx/tx
###  direction match field of the MEV P4 pipeline and the necessary
###  duplication of some route entries is a technicality the MEV P4 pipeline
###  and must be handled by the p4ctrl module.
--------------------------------------------------------------------------*/

// Route structure has route info
type Route interface {
	Route_store(*infradb.Vrf, map[string]string)
}

// RouteStruct structure has route info
type RouteStruct struct {
	Route0   vn.Route
	Vrf      *infradb.Vrf
	Nexthops []NexthopStruct
	Metadata map[interface{}]interface{}
	NlType   string
	Key      RouteKey
	Err      error
}

// RouteList list has route info
type RouteList struct {
	RS []RouteStruct
}

// NexthopStruct contains nexthop structure
type NexthopStruct struct {
	nexthop vn.NexthopInfo
	Vrf     *infradb.Vrf
	Local   bool
	Weight  int
	// dst net.IP
	Metric    int
	ID        int
	Scope     int
	Protocol  int
	RouteRefs []RouteStruct
	Key       NexthopKey
	Resolved  bool
	Neighbor  *NeighStruct // ???
	NhType    int
	Metadata  map[interface{}]interface{}
}

// NetMaskToInt convert network mask to int
func NetMaskToInt(mask int) (netmaskint [4]int64) {
	var binarystring string

	for ii := 1; ii <= mask; ii++ {
		binarystring += "1"
	}
	for ii := 1; ii <= (32 - mask); ii++ {
		binarystring += "0"
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

// RtnType map of string key as RTN Type
var RtnType = map[string]int{
	"unspec":      unix.RTN_UNSPEC,
	"unicast":     unix.RTN_UNICAST,
	"local":       unix.RTN_LOCAL,
	"broadcast":   unix.RTN_BROADCAST,
	"anycast":     unix.RTN_ANYCAST,
	"multicast":   unix.RTN_MULTICAST,
	"blackhole":   unix.RTN_BLACKHOLE,
	"unreachable": unix.RTN_UNREACHABLE,
	"prohibit":    unix.RTN_PROHIBIT,
	"throw":       unix.RTN_THROW,
	"nat":         unix.RTN_NAT,
	"xresolve":    unix.RTN_XRESOLVE,
	"neighbor":    RTNNeighbor,
}

// RtnProto map of string key as RTN Type
var RtnProto = map[string]int{
	"unspec":        unix.RTPROT_UNSPEC,
	"redirect":      unix.RTPROT_REDIRECT,
	"kernel":        unix.RTPROT_KERNEL,
	"boot":          unix.RTPROT_BOOT,
	"static":        unix.RTPROT_STATIC,
	"bgp":           int('B'),
	"ipu_infra_mgr": int('I'),
	"196":           196,
}

// RtnScope map of string key as RTN scope
var RtnScope = map[string]int{
	"global":  unix.RT_SCOPE_UNIVERSE,
	"site":    unix.RT_SCOPE_SITE,
	"link":    unix.RT_SCOPE_LINK,
	"local":   unix.RT_SCOPE_HOST,
	"nowhere": unix.RT_SCOPE_NOWHERE,
}

// flagstring strucure
type flagstring struct {
	f int
	s string
}

// testFlag array of flag string
var testFlag = []flagstring{
	{f: unix.RTNH_F_ONLINK, s: "onlink"},
	{f: unix.RTNH_F_PERVASIVE, s: "pervasive"},
}

// getFlags gets the flag
func getFlags(s string) int {
	f := 0
	for _, F := range testFlag {
		if s == F.s {
			f |= F.f
		}
	}
	return f
}

// getFlagString return flag of type string
func getFlagString(flag int) string {
	f := ""
	for _, F := range testFlag {
		if F.f == flag {
			str := F.s
			return str
		}
	}
	return f
}

// NhIDCache Variable
var NhIDCache = make(map[NexthopKey]int)

// NhNextID Variable
var NhNextID = 16

// NHAssignID returns the nexthop id
func NHAssignID(key NexthopKey) int {
	id := NhIDCache[key]
	if id == 0 {
		// Assigne a free id and insert it into the cache
		id = NhNextID
		NhIDCache[key] = id
		NhNextID++
	}
	return id
}

// NHParse parses the neighbor
func NHParse(v *infradb.Vrf, rc RouteCmdInfo) NexthopStruct {
	var nh NexthopStruct
	nh.Weight = 1
	nh.Vrf = v
	if !reflect.ValueOf(rc.Dev).IsZero() {
		vrf, _ := vn.LinkByName(rc.Dev)
		nh.nexthop.LinkIndex = vrf.Attrs().Index
		NameIndex[nh.nexthop.LinkIndex] = vrf.Attrs().Name
	}
	if len(rc.Flags) != 0 {
		nh.nexthop.Flags = getFlags(rc.Flags[0])
	}
	if !reflect.ValueOf(rc.Gateway).IsZero() {
		nIP := &net.IPNet{
			IP: net.ParseIP(rc.Gateway),
		}
		nh.nexthop.Gw = nIP.IP
	}
	if !reflect.ValueOf(rc.Protocol).IsZero() {
		nh.Protocol = RtnProto[rc.Protocol]
	}
	if !reflect.ValueOf(rc.Scope).IsZero() {
		nh.Scope = RtnScope[rc.Scope]
	}
	if !reflect.ValueOf(rc.Type).IsZero() {
		nh.NhType = RtnType[rc.Type]
		if nh.NhType == unix.RTN_LOCAL {
			nh.Local = true
		} else {
			nh.Local = false
		}
	}
	if !reflect.ValueOf(rc.Weight).IsZero() {
		nh.Weight = rc.Weight
	}
	nh.Key = NexthopKey{nh.Vrf.Name, nh.nexthop.Gw.String(), nh.nexthop.LinkIndex, nh.Local}
	return nh
}

// checkRtype checks the route type
func checkRtype(rType string) bool {
	var Types = [6]string{"connected", "evpn-vxlan", "static", "bgp", "local", "neighbor"}
	for _, v := range Types {
		if v == rType {
			return true
		}
	}
	return false
}

// preFilterRoute pre filter the routes
func preFilterRoute(r RouteStruct) bool {
	if checkRtype(r.NlType) && !r.Route0.Dst.IP.IsLoopback() && strings.Compare(r.Route0.Dst.IP.String(), "0.0.0.0") != 0 {
		return true
	}

	return false
}

// checkProto checks the proto type
func checkProto(proto int) bool {
	var protos = [3]int{unix.RTPROT_BOOT, unix.RTPROT_STATIC, 196}
	for _, v := range protos {
		if proto == v {
			return true
		}
	}
	return false
}

// annotate function annonates the entries
func (route RouteStruct) annotate() RouteStruct {
	route.Metadata = make(map[interface{}]interface{})
	for i := 0; i < len(route.Nexthops); i++ {
		nexthop := route.Nexthops[i]
		// route.Metadata["nh_ids"] = append(route.Metadata["nh_ids"], string(nexthop.id))
		route.Metadata["nh_ids"] = nexthop.ID
	}
	if route.Vrf.Spec.Vni != nil {
		route.Metadata["vrf_id"] = *route.Vrf.Spec.Vni
	} else {
		route.Metadata["vrf_id"] = 0
	}
	if len(route.Nexthops) != 0 {
		nexthop := route.Nexthops[0]
		if route.Vrf.Spec.Vni == nil { // GRD
			/*if nexthop.NhType == PHY {
				route.Metadata["direction"] = RXTX
			} else if nexthop.NhType == ACC {
				route.Metadata["direction"] = RX
			} else {
				route.Metadata["direction"] = None
			}*/
			switch nexthop.NhType {
			case PHY:
				route.Metadata["direction"] = RXTX
			case ACC:
				route.Metadata["direction"] = RX
			default:
				route.Metadata["direction"] = None
			}
		} else {
			/*if nexthop.NhType == VXLAN {
				route.Metadata["direction"] = RXTX
			} else if nexthop.NhType == SVI || nexthop.NhType == ACC {
				route.Metadata["direction"] = RXTX
			} else {
				route.Metadata["direction"] = None
			}*/
			switch nexthop.NhType {
			case VXLAN:
				route.Metadata["direction"] = RXTX
			case SVI, ACC:
				route.Metadata["direction"] = RXTX
			default:
				route.Metadata["direction"] = None
			}
		}
	} else {
		route.Metadata["direction"] = None
	}
	return route
}

//nolint
func setRouteType(rs RouteStruct, v *infradb.Vrf) string {
	if rs.Route0.Type == unix.RTN_UNICAST && rs.Route0.Protocol == unix.RTPROT_KERNEL && rs.Route0.Scope == unix.RT_SCOPE_LINK && len(rs.Nexthops) == 1 {
		// Connected routes are proto=kernel and scope=link with a netdev as single nexthop
		return "connected"
	} else if rs.Route0.Type == unix.RTN_UNICAST && int(rs.Route0.Protocol) == int('B') && rs.Route0.Scope == unix.RT_SCOPE_UNIVERSE {
		// EVPN routes to remote destinations are proto=bgp, scope global withipu_infra_mgr_db
		// all Nexthops residing on the br-<VRF name> bridge interface of the VRF.
		var devs []string
		if len(rs.Nexthops) != 0 {
			for _, d := range rs.Nexthops {
				devs = append(devs, NameIndex[d.nexthop.LinkIndex])
			}
			if len(devs) == 1 && devs[0] == "br-"+v.Name {
				return "evpn-vxlan"
			}
			return "bgp"
		}
	} else if rs.Route0.Type == unix.RTN_UNICAST && checkProto(int(rs.Route0.Protocol)) && rs.Route0.Scope == unix.RT_SCOPE_UNIVERSE {
		return "static"
	} else if rs.Route0.Type == unix.RTN_LOCAL {
		return "local"
	} else if rs.Route0.Type == RTNNeighbor {
		// Special /32 or /128 routes for Resolved neighbors on connected subnets
		return "neighbor"
	}
	return "unknown"
}

// RouteSlice empty route structure slice
var RouteSlice []RouteStruct

// Parse_Route parse the routes
//nolint
func Parse_Route(v *infradb.Vrf, Rm []RouteCmdInfo, t int) RouteList {
	var route RouteList
	for _, Ro := range Rm {
		if reflect.ValueOf(Ro.Type).IsZero() && (!reflect.ValueOf(Ro.Dev).IsZero() || !reflect.ValueOf(Ro.Gateway).IsZero()) {
			Ro.Type = "local"
		}
		var rs RouteStruct
		rs.Vrf = v
		if !reflect.ValueOf(Ro.Nhid).IsZero() || !reflect.ValueOf(Ro.Gateway).IsZero() || !reflect.ValueOf(Ro.Dev).IsZero() {
			rs.Nexthops = append(rs.Nexthops, NHParse(v, Ro))
		}
		rs.NlType = "unknown"
		rs.Route0.Table = t
		rs.Route0.Priority = 1
		if !reflect.ValueOf(Ro.Dev).IsZero() {
			dev, _ := vn.LinkByName(Ro.Dev)
			rs.Route0.LinkIndex = dev.Attrs().Index
		}
		if !reflect.ValueOf(Ro.Dst).IsZero() {
			var Mask int
			split := Ro.Dst
			if strings.Contains(Ro.Dst, "/") {
				split4 := strings.Split(Ro.Dst, "/")
				Mask, _ = strconv.Atoi(split4[1])
				split = split4[0]
			} else {
				Mask = 32
			}
			var nIP *net.IPNet
			if Ro.Dst == "default" {
				nIP = &net.IPNet{
					IP:   net.ParseIP("0.0.0.0"),
					Mask: net.IPv4Mask(0, 0, 0, 0),
				}
			} else {
				mtoip := NetMaskToInt(Mask)
				b3 := make([]byte, 8) // Converting int64 to byte
				binary.LittleEndian.PutUint64(b3, uint64(mtoip[3]))
				b2 := make([]byte, 8)
				binary.LittleEndian.PutUint64(b2, uint64(mtoip[2]))
				b1 := make([]byte, 8)
				binary.LittleEndian.PutUint64(b1, uint64(mtoip[1]))
				b0 := make([]byte, 8)
				binary.LittleEndian.PutUint64(b0, uint64(mtoip[0]))
				nIP = &net.IPNet{
					IP:   net.ParseIP(split),
					Mask: net.IPv4Mask(b0[0], b1[0], b2[0], b3[0]),
				}
			}
			rs.Route0.Dst = nIP
		}
		if !reflect.ValueOf(Ro.Metric).IsZero() {
			rs.Route0.Priority = Ro.Metric
		}
		if !reflect.ValueOf(Ro.Protocol).IsZero() {
			if RtnProto[Ro.Protocol] != 0 {
				rs.Route0.Protocol = vn.RouteProtocol(RtnProto[Ro.Protocol])
			} else {
				rs.Route0.Protocol = 0
			}
		}
		if !reflect.ValueOf(Ro.Type).IsZero() {
			rs.Route0.Type = RtnType[Ro.Type]
		}
		if len(Ro.Flags) != 0 {
			rs.Route0.Flags = getFlags(Ro.Flags[0])
		}
		if !reflect.ValueOf(Ro.Scope).IsZero() {
			rs.Route0.Scope = vn.Scope(RtnScope[Ro.Scope])
		}
		if !reflect.ValueOf(Ro.Prefsrc).IsZero() {
			nIP := &net.IPNet{
				IP: net.ParseIP(Ro.Prefsrc),
			}
			rs.Route0.Src = nIP.IP
		}
		if !reflect.ValueOf(Ro.Gateway).IsZero() {
			nIP := &net.IPNet{
				IP: net.ParseIP(Ro.Gateway),
			}
			rs.Route0.Gw = nIP.IP
		}
		if !reflect.ValueOf(Ro.VRF).IsZero() {
			rs.Vrf, _ = infradb.GetVrf(Ro.VRF.Name)
		}
		if !reflect.ValueOf(Ro.Table).IsZero() {
			rs.Route0.Table = Ro.Table
		}
		rs.NlType = setRouteType(rs, v)
		rs.Key = RouteKey{Table: rs.Route0.Table, Dst: rs.Route0.Dst.String()}
		if preFilterRoute(rs) {
			route.RS = append(route.RS, rs)
		}
	}
	//	RouteSlice = route.RS
	//	sort.Slice(RouteSlice, comparekey)
	//	route.RS = RouteSlice
	//	log.Printf("%+v",route)
	return route
}

/*func comparekey(i, j int) bool {
	return RouteSlice[i].Key.Table > RouteSlice[j].Key.Table && RouteSlice[i].Key.Dst > RouteSlice[j].Key.Dst
}*/

//--------------------------------------------------------------------------
//###  NexthopStruct Database Entries
//--------------------------------------------------------------------------

// TryResolve function
type TryResolve func(map[string]string)

// --------------------------------------------------------------------------
// ###  Bridge MAC Address Database
// ###
// ###  We split the Linux FDB entries into DMAC and L2 NexthopStruct tables similar
// ###  to routes and L3 nexthops, Thus, all remote EVPN DMAC entries share a
// ###  single VXLAN L2 nexthop table entry.
// ###
// ###  TODO: Support for dynamically learned MAC addresses on BridgePorts
// ###  (e.g. for pod interfaces operating in promiscuous mode).
// --------------------------------------------------------------------------

// L2NexthopStruct structure
type L2NexthopStruct struct {
	Dev    string
	VlanID int
	Dst    net.IP
	Key    L2NexthopKey
	// lb
	// bp
	ID       int
	FdbRefs  []FdbEntryStruct
	Resolved bool
	// id_cache map[L2NexthopKey]int
	Type     int
	Metadata map[interface{}]interface{}
}

// FdbEntryStruct structure
type FdbEntryStruct struct {
	// Route0 netlink.Route
	VlanID int
	Mac    string
	Key    FDBKey
	State  string
	// lb
	// bp
	Nexthop  L2NexthopStruct
	Type     int
	Metadata map[interface{}]interface{}
	Err      error
}

// FDBEntryList list
type FDBEntryList struct {
	FS []FdbEntryStruct
}

// ParseFdb parse the fdb
func ParseFdb(fdbIP FdbIPStruct, fdbentry FdbEntryStruct) FdbEntryStruct {
	fdbentry.VlanID = fdbIP.Vlan
	fdbentry.Mac = fdbIP.Mac
	fdbentry.Key = FDBKey{fdbIP.Vlan, fdbIP.Mac}
	fdbentry.State = fdbIP.State
	/*   //Need to complete InfraDB
		fdbentry.lb = InfraDB.get_LB(fdbentry.VlanID)
	        // TODO: This only handles the case of the VF Mac address itself,
	        // not any Mac addresses used over the VF (in promiscuous mode)
	        if !(reflect.ValueOf(fdbentry.lb).IsZero()){
			bp = fdbentry.lb.lookup_Mac(fdbentry.Mac)
		}
	*/
	Dev := fdbIP.Ifname
	dst := fdbIP.Dst
	fdbentry.Nexthop = fdbentry.Nexthop.ParseL2NH(fdbentry.VlanID, Dev, dst /*, lb, bp*/)
	fdbentry.Type = fdbentry.Nexthop.Type
	return fdbentry
}

// ParseL2NH parse the l2hn
func (l2n L2NexthopStruct) ParseL2NH(vlanID int, dev string, dst string /*, LB, BP */) L2NexthopStruct {
	l2n.Dev = dev
	l2n.VlanID = vlanID
	l2n.Dst = net.IP(dst)
	l2n.Key = L2NexthopKey{l2n.Dev, l2n.VlanID, string(l2n.Dst)}
	// l2n.lb: ipu_db.LogicalBridge = LB
	// l2n.bp: ipu_db.BridgePort = BP
	if l2n.Dev == fmt.Sprintf("svi-%d", l2n.VlanID) {
		l2n.Type = SVI
	} else if l2n.Dev == fmt.Sprintf("vxlan-%d", l2n.VlanID) {
		l2n.Type = VXLAN
	} // else if l2n.bp {
	// TODO
	/*l2n.Type = BRIDGEPORT
		} else {
	            l2n.Type = None
	    	}
	*/
	return l2n
}

// l2nexthopID
var l2nexthopID = 16

// l2NhIDCache
var l2NhIDCache = make(map[L2NexthopKey]int)

// L2NHAssignID get nexthop id
func L2NHAssignID(key L2NexthopKey) int {
	id := l2NhIDCache[key]
	if id == 0 {
		// Assigne a free id and insert it into the cache
		id = l2nexthopID
		l2NhIDCache[key] = id
		l2nexthopID++
	}
	return id
}

// addFdbEntry add fdb entries
func addFdbEntry(m FdbEntryStruct) {
	m = addL2Nexthop(m)
	// TODO
	// logger.debug(f"Adding {m.format()}.")
	LatestFDB[m.Key] = m
}

// addL2Nexthop add the l2 nexthop
func addL2Nexthop(m FdbEntryStruct) FdbEntryStruct {
	if reflect.ValueOf(LatestL2Nexthop).IsZero() {
		log.Fatal("L2Nexthop DB empty\n")
		return FdbEntryStruct{}
	}
	latestNexthops := LatestL2Nexthop[m.Nexthop.Key]
	if !(reflect.ValueOf(latestNexthops).IsZero()) {
		latestNexthops.FdbRefs = append(latestNexthops.FdbRefs, m) // latestNexthops.fdb_refs.append(r) --- what is r here??????
		m.Nexthop = latestNexthops
	} else {
		latestNexthops = m.Nexthop
		latestNexthops.FdbRefs = append(latestNexthops.FdbRefs, m)
		latestNexthops.ID = L2NHAssignID(latestNexthops.Key)
		// latestNexthops.assign_id()
		//		log.Printf("VV %d\n",latestNexthops.ID)
		LatestL2Nexthop[latestNexthops.Key] = latestNexthops
		m.Nexthop = latestNexthops
		// log.Printf("in add function %+v\n",m)
	}
	return m
}

//--------------------------------------------------------------------------
//###  Neighbor Database Entries
//--------------------------------------------------------------------------

// NeighInit neighbor init function
type NeighInit func(int, map[string]string)

// linkTable wg sync.WaitGroup
var linkTable []vn.Link

// vrfList netlink libarary var
var vrfList []vn.Link

// deviceList netlink libarary var
var deviceList []vn.Link

// vlanList netlink libarary var
var vlanList []vn.Link

// bridgeList netlink libarary var
var bridgeList []vn.Link

// vxlanList netlink libarary var
var vxlanList []vn.Link

// linkList netlink libarary var
var linkList []vn.Link

// NameIndex netlink libarary var
var NameIndex = make(map[int]string)

// getlink get the link
func getlink() {
	links, err := vn.LinkList()
	if err != nil {
		log.Fatal(err)
	}
	for i := 0; i < len(links); i++ {
		linkTable = append(linkTable, links[i])
		NameIndex[links[i].Attrs().Index] = links[i].Attrs().Name
		/*if reflect.DeepEqual(links[i].Type(), "vrf") {
			vrfList = append(vrfList, links[i])
		} else if reflect.DeepEqual(links[i].Type(), "device") {
			deviceList = append(deviceList, links[i])
		} else if reflect.DeepEqual(links[i].Type(), "vlan") {
			vlanList = append(vlanList, links[i])
		} else if reflect.DeepEqual(links[i].Type(), "bridge") {
			bridgeList = append(bridgeList, links[i])
		} else if reflect.DeepEqual(links[i].Type(), "vxlan") {
			vxlanList = append(vxlanList, links[i])
		}*/
		switch links[i].Type() {
		case "vrf":
			vrfList = append(vrfList, links[i])
		case "device":
			deviceList = append(deviceList, links[i])
		case "vlan":
			vlanList = append(vlanList, links[i])
		case "bridge":
			bridgeList = append(bridgeList, links[i])
		case "vxlan":
			vxlanList = append(vxlanList, links[i])
		default:
		}
		linkList = append(linkList, links[i])
	}
}

// readLatestNetlinkState reads the latest netlink state
func readLatestNetlinkState() {
	vrfs, _ := infradb.GetAllVrfs()
	for _, v := range vrfs {
		readNeighbors(v) // viswanantha library
		readRoutes(v)    // Viswantha library
	}
	m := readFDB()
	for i := 0; i < len(m); i++ {
		addFdbEntry(m[i])
	}
	dumpDBs()
}

// dumpDBs dumps the databse
func dumpDBs() {
	file, err := os.OpenFile("netlink_dump", os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		panic(err)
	}
	if err := os.Truncate("netlink_dump", 0); err != nil {
		log.Printf("Failed to truncate: %v", err)
	}
	// defer file.Close()
	str := dumpRouteDB()
	log.Printf("\n")
	str += dumpNexthDB()
	log.Printf("\n")
	str += dumpNeighDB()
	log.Printf("\n")
	str += dumpFDB()
	log.Printf("\n")
	str += dumpL2NexthDB()
	_, err = file.WriteString(str)
	if err != nil {
		log.Println(err)
	}
	err = file.Close()
	if err != nil {
		log.Println(err)
	}
}

/*func ensureIndex(link *vn.LinkAttrs) {
	if link != nil && link.Index == 0 {
		newlink, _ := vn.LinkByName(link.Name)
		if newlink != nil {
			link.Index = newlink.Attrs().Index
		}
	}
}*/

// NeighStruct structure
type NeighStruct struct {
	Neigh0   vn.Neigh
	Protocol string
	VrfName  string
	Type     int
	Dev      string
	Err      error
	Key      NeighKey
	Metadata map[string]string
}

// NeighList structure
type NeighList struct {
	NS []NeighStruct
}

func neighborAnnotate(neighbor NeighStruct) NeighStruct {
	if strings.HasPrefix(neighbor.Dev, path.Base(neighbor.VrfName)) && neighbor.Protocol != "zebra" {
		pattern := fmt.Sprintf(`%s-\d+$`, path.Base(neighbor.VrfName))
		mustcompile := regexp.MustCompile(pattern)
		s := mustcompile.FindStringSubmatch(neighbor.Dev)
		vlanID := strings.Split(s[0], "-")[1]
		// TODO
		// LB = InfraDB.get_LB(vlanID)
		//BP: ipu_db.BridgePort = LB.lookup_mac(self.lladdr)
		// if BP{
		neighbor.Type = SVI
		neighbor.Metadata["vport_id"] = "0xa" // BP.vport_id
		neighbor.Metadata["vlanID"] = vlanID
		neighbor.Metadata["portType"] = "host" // BP.type
		/*else{
		          neighbor.Type = None;
		  }
		  logger.exception(f"Failed to lookup egress vport for SVI neighbor {self}")*/
	} else if path.Base(neighbor.VrfName) == "GRD" && neighbor.Protocol != "zebra" {
		for d := range phyPorts {
			if neighbor.Dev == d {
				neighbor.Type = PHY
				// neighbor.Metadata["vport_id"] = string(phyPorts[d]) // neighbor.Dev]
				neighbor.Metadata["vport_id"] = fmt.Sprint(phyPorts[d])
			}
		}
		// logger.debug(f"Annotated {self}: type={self.type} extra={self.metadata}")
	}
	return neighbor
}

// CheckNdup checks the duplication of neighbor
func CheckNdup(tmpKey NeighKey) bool {
	var dup = false
	for k := range LatestNeighbors {
		if k == tmpKey {
			dup = true
			break
		}
	}
	return dup
}

// CheckRdup checks the duplication of routes
func CheckRdup(tmpKey RouteKey) bool {
	var dup = false
	for j := range LatestRoutes {
		if j == tmpKey {
			dup = true
			break
		}
	}
	return dup
}

/*func Check_Vrfdup(Vrf_tmp *infradb.Vrf) bool {
	var dup = false
	for _,tmp := range vrf_table {
			if tmp.Name == Vrf_tmp.Name {
		   		dup = true
		   		break
				}
	}
	return dup
}*/

// addNeigh adds the neigh
func addNeigh(dump NeighList) {
	for _, n := range dump.NS {
		n = neighborAnnotate(n)
		if len(LatestNeighbors) == 0 {
			LatestNeighbors[n.Key] = n
		} else if !CheckNdup(n.Key) {
			LatestNeighbors[n.Key] = n
		}
	}
}

// getStateStr gets the state from int
func getStateStr(s int) string {
	neighState := map[int]string{
		vn.NUD_NONE:       "NONE",
		vn.NUD_INCOMPLETE: "INCOMPLETE",
		vn.NUD_REACHABLE:  "REACHABLE",
		vn.NUD_STALE:      "STALE",
		vn.NUD_DELAY:      "DELAY",
		vn.NUD_PROBE:      "PROBE",
		vn.NUD_FAILED:     "FAILED",
		vn.NUD_NOARP:      "NOARP",
		vn.NUD_PERMANENT:  "PERMANENT",
	}
	return neighState[s]
}

func printNeigh(neigh *NeighStruct) string {
	var Proto string
	// N :=NeighStruct{}
	if neigh == nil {
		return strNone
	}
	if neigh.Protocol == "" {
		Proto = strNone
	} else {
		Proto = neigh.Protocol
	}
	str := fmt.Sprintf("Neighbor(vrf=%s dst=%s lladdr=%s dev=%s proto=%s state=%s) ", neigh.VrfName, neigh.Neigh0.IP.String(), neigh.Neigh0.HardwareAddr.String(), NameIndex[neigh.Neigh0.LinkIndex], Proto, getStateStr(neigh.Neigh0.State))
	// log.Println(str)
	return str
}

// dumpRouteDB dump the route database
func dumpRouteDB() string {
	var s string
	log.Printf("len %d\n", len(LatestRoutes))
	log.Printf("Route table:\n")
	s = "Route table:\n"
	for _, n := range LatestRoutes {
		var via string
		if n.Route0.Gw.String() == "<nil>" {
			via = strNone
		} else {
			via = n.Route0.Gw.String()
		}
		str := fmt.Sprintf("Route(vrf=%s dst=%s type=%s proto=%s metric=%d  via=%s dev=%s nhid= %d Table= %d)", n.Vrf.Name, n.Route0.Dst.String(), n.NlType, getProto(n), n.Route0.Priority, via, NameIndex[n.Route0.LinkIndex], n.Nexthops[0].ID, n.Route0.Table)
		log.Println(str)
		s += str
		s += "\n"
	}
	log.Printf("\n\n\n")
	s += "\n\n"
	return s
}

// dumpL2NexthDB dump the l2 nexthop entries
func dumpL2NexthDB() string {
	var s string
	log.Printf("L2 Nexthop table:\n")
	log.Printf("len %d\n", len(LatestL2Nexthop))
	s = "L2 Nexthop table:\n"
	var ip string
	for _, n := range LatestL2Nexthop {
		if n.Dst.String() == "<nil>" {
			ip = strNone
		} else {
			ip = n.Dst.String()
		}
		str := fmt.Sprintf("L2Nexthop(id=%d dev=%s vlan=%d dst=%s type=%d #FDB entries=%d Resolved=%t) ", n.ID, n.Dev, n.VlanID, ip, n.Type, len(n.FdbRefs), n.Resolved)
		log.Println(str)
		s += str
		s += "\n"
	}
	log.Printf("\n\n\n")
	s += "\n\n"
	return s
}

// dumpFDB dump the fdb entries
func dumpFDB() string {
	var s string
	log.Printf("FDB table:\n")
	log.Printf("len %d\n", len(LatestFDB))
	s = "FDB table:\n"
	for _, n := range LatestFDB {
		str := fmt.Sprintf("MacAddr(vlan=%d mac=%s state=%s type=%d l2nh_id=%d) ", n.VlanID, n.Mac, n.State, n.Type, n.Nexthop.ID)
		log.Println(str)
		s += str
		s += "\n"
	}
	log.Printf("\n\n\n")
	s += "\n\n"
	return s
}

// dumpNexthDB dump the nexthop entries
func dumpNexthDB() string {
	var s string
	log.Printf("Nexthop table:\n")
	s = "Nexthop table:\n"
	log.Printf("len %d\n", len(LatestNexthop))
	for _, n := range LatestNexthop {
		str := fmt.Sprintf("Nexthop(id=%d vrf=%s dst=%s dev=%s Local=%t weight=%d flags=[%s] #routes=%d Resolved=%t neighbor=%s) ", n.ID, n.Vrf.Name, n.nexthop.Gw.String(), NameIndex[n.nexthop.LinkIndex], n.Local, n.Weight, getFlagString(n.nexthop.Flags), len(n.RouteRefs), n.Resolved, printNeigh(n.Neighbor))
		log.Println(str)
		s += str
		s += "\n"
	}
	log.Printf("\n\n\n")
	s += "\n\n"
	return s
}

// dumpNeighDB dump the neighbor entries
func dumpNeighDB() string {
	var s string
	log.Printf("Neighbor table:\n")
	s = "Neighbor table:\n"
	log.Printf("len %d\n", len(LatestNeighbors))
	for _, n := range LatestNeighbors {
		var Proto string
		if n.Protocol == "" {
			Proto = strNone
		} else {
			Proto = n.Protocol
		}
		str := fmt.Sprintf("Neighbor(vrf=%s dst=%s lladdr=%s dev=%s proto=%s state=%s Type : %d) ", n.VrfName, n.Neigh0.IP.String(), n.Neigh0.HardwareAddr.String(), NameIndex[n.Neigh0.LinkIndex], Proto, getStateStr(n.Neigh0.State), n.Type)
		log.Println(str)
		s += str
		s += "\n"
	}
	s += "\n\n"
	return s
}

// getProto gets the route protocol
func getProto(n RouteStruct) string {
	for p, i := range RtnProto {
		if i == int(n.Route0.Protocol) {
			return p
		}
	}
	return "0"
}

// getType get the type of route
/*func getType(n RouteStruct) string {
	for t, i := range RtnType {
		if i == n.Route0.Type {
			return t
		}
	}
	return fmt.Sprint(0)
}*/

// checkNeigh checks the nighbor
func checkNeigh(nk NeighKey) bool {
	for k := range LatestNeighbors {
		if k == nk {
			return true
		}
	}
	return false
}

// tryResolve resolves the neighbor
func tryResolve(nexhthopSt NexthopStruct) NexthopStruct {
	if len(nexhthopSt.nexthop.Gw) != 0 {
		// Nexthops with a gateway IP need resolution of that IP
		neighborKey := NeighKey{Dst: nexhthopSt.nexthop.Gw.String(), VrfName: nexhthopSt.Vrf.Name, Dev: nexhthopSt.nexthop.LinkIndex}
		ch := checkNeigh(neighborKey)
		if ch && LatestNeighbors[neighborKey].Neigh0.Type != 0 {
			nexhthopSt.Resolved = true
			nh := LatestNeighbors[neighborKey]
			nexhthopSt.Neighbor = &nh
			// log.Println(nexhthopSt.neighbor)
		} else {
			nexhthopSt.Resolved = false
			// nexhthopSt.Neighbor = NeighStruct{}
		}
	} else {
		nexhthopSt.Resolved = true
	}
	return nexhthopSt
}

// checkNhDB checks the neighbor database
func checkNhDB(nhKey NexthopKey) bool {
	for k := range LatestNexthop {
		if k == nhKey {
			return true
		}
	}
	return false
}

// addNexthop adds the nexthop
func addNexthop(nexthop NexthopStruct, r RouteStruct) RouteStruct {
	ch := checkNhDB(nexthop.Key)
	//	 log.Printf("CH %d\n",ch)
	if ch {
		NH0 := LatestNexthop[nexthop.Key]
		// Links route with existing nexthop
		NH0.RouteRefs = append(NH0.RouteRefs, r)
		r.Nexthops = append(r.Nexthops, NH0)
		// log.Printf("Adding route to %v\n",nh.Key)
	} else {
		// Create a new nexthop entry
		nexthop.RouteRefs = append(nexthop.RouteRefs, r)
		nexthop.ID = NHAssignID(nexthop.Key)
		nexthop = tryResolve(nexthop)
		LatestNexthop[nexthop.Key] = nexthop
		r.Nexthops = append(r.Nexthops, nexthop)
	}
	return r
}

// checkRoute checks the route
func checkRoute(r RouteStruct) bool {
	Rk := r.Key
	for k := range LatestRoutes {
		if k == Rk {
			return true
		}
	}
	return false
}

// deleteNH deletes the neighbor
func deleteNH(nexthop []NexthopStruct) []NexthopStruct {
	index := len(nexthop)
	if index == 1 {
		nexthop = append(nexthop[:0], nexthop[1:]...)
	} else {
		for i := 0; i < index-1; i++ {
			nexthop = append(nexthop[:0], nexthop[1:]...)
		}
	}
	return nexthop
}

// addRoute add the route
func addRoute(r RouteStruct) {
	ch := checkRoute(r)
	if ch {
		R0 := LatestRoutes[r.Key]
		if r.Route0.Priority >= R0.Route0.Priority {
			// Route with lower metric exists and takes precedence
			log.Printf("Ignoring %+v  with higher metric than %+v\n", r, R0)
		} else {
			log.Printf("conflicts %+v with higher metric %+v. Will ignore it", r, R0)
		}
	} else {
		Nexthops := r.Nexthops
		r.Nexthops = deleteNH(r.Nexthops)
		for _, nexthop := range Nexthops {
			r = addNexthop(nexthop, r)
		}
		LatestRoutes[r.Key] = r
	}
}

// cmdProcessNb process the neighbor command
func cmdProcessNb(nb string, v string) NeighList {
	var nbs []NeighIPStruct
	CPs := strings.Split(nb[2:len(nb)-3], "},{")
	for i := 0; i < len(CPs); i++ {
		var ni NeighIPStruct
		log.Println(CPs[i])
		err := json.Unmarshal([]byte(fmt.Sprintf("{%v}", CPs[i])), &ni)
		if err != nil {
			log.Println("error-", err)
		}
		nbs = append(nbs, ni)
	}
	Neigh := ParseNeigh(nbs, v)
	return Neigh
}

// getState gets the state for the neighbor
func getState(s string) int {
	neighState := map[string]int{
		"NONE":       vn.NUD_NONE,
		"INCOMPLETE": vn.NUD_INCOMPLETE,
		"REACHABLE":  vn.NUD_REACHABLE,
		"STALE":      vn.NUD_STALE,
		"DELAY":      vn.NUD_DELAY,
		"PROBE":      vn.NUD_PROBE,
		"FAILED":     vn.NUD_FAILED,
		"NOARP":      vn.NUD_NOARP,
		"PERMANENT":  vn.NUD_PERMANENT,
	}
	return neighState[s]
}

// preFilterNeighbor pre filter the neighbors
func preFilterNeighbor(n NeighStruct) bool {
	if n.Neigh0.State != vn.NUD_NONE && n.Neigh0.State != vn.NUD_INCOMPLETE && n.Neigh0.State != vn.NUD_FAILED && NameIndex[n.Neigh0.LinkIndex] != "lo" {
		return true
	}

	return false
}

// ParseNeigh parses the neigh
func ParseNeigh(nm []NeighIPStruct, v string) NeighList {
	var NL NeighList
	for _, ND := range nm {
		var ns NeighStruct
		ns.Neigh0.Type = OTHER
		ns.VrfName = v
		if !reflect.ValueOf(ND.Dev).IsZero() {
			vrf, _ := vn.LinkByName(ND.Dev)
			ns.Neigh0.LinkIndex = vrf.Attrs().Index
		}
		if !reflect.ValueOf(ND.Dst).IsZero() {
			ipnet := &net.IPNet{
				IP: net.ParseIP(ND.Dst),
			}
			ns.Neigh0.IP = ipnet.IP
		}
		if !reflect.ValueOf(ND.State).IsZero() {
			ns.Neigh0.State = getState(ND.State[0])
		}
		if !reflect.ValueOf(ND.Lladdr).IsZero() {
			ns.Neigh0.HardwareAddr, _ = net.ParseMAC(ND.Lladdr)
		}
		if !reflect.ValueOf(ND.Protocol).IsZero() {
			ns.Protocol = ND.Protocol
		}
		//	ns  =  neighborAnnotate(ns)   /* Need InfraDB to finish for fetching LB/BP information */
		ns.Key = NeighKey{VrfName: v, Dst: ns.Neigh0.IP.String(), Dev: ns.Neigh0.LinkIndex}
		if preFilterNeighbor(ns) {
			NL.NS = append(NL.NS, ns)
		}
	}
	return NL
}

// getNeighborRoutes gets the nighbor routes
func getNeighborRoutes() []RouteCmdInfo { // []map[string]string{
	// Return a list of /32 or /128 routes & Nexthops to be inserted into
	// the routing tables for Resolved neighbors on connected subnets
	// on physical and SVI interfaces.
	var neighborRoutes []RouteCmdInfo // []map[string]string
	for _, N := range LatestNeighbors {
		// if N.Type == PHY || N.Type == SVI {
		if (NameIndex[N.Neigh0.LinkIndex] == "enp0s1f0d1" || NameIndex[N.Neigh0.LinkIndex] == "enp0s1f0d3") && N.Neigh0.State == vn.NUD_REACHABLE {
			vrf, _ := infradb.GetVrf(N.VrfName)
			table := int(*vrf.Metadata.RoutingTable[0])

			//# Create a special route with dst == gateway to resolve
			//# the nexthop to the existing neighbor
			R0 := RouteCmdInfo{Type: "neighbor", Dst: N.Neigh0.IP.String(), Protocol: "ipu_infra_mgr", Scope: "global", Gateway: N.Neigh0.IP.String(), Dev: NameIndex[N.Neigh0.LinkIndex], VRF: vrf, Table: table}
			neighborRoutes = append(neighborRoutes, R0)
		}
	}
	return neighborRoutes
}

// readNeighbors reads the nighbors
func readNeighbors(v *infradb.Vrf) {
	var N NeighList
	var err error
	var Nb string
	if v.Spec.Vni == nil {
		/* No support for "ip neighbor show" command in netlink library Raised ticket https://github.com/vishvananda/netlink/issues/913 ,
		   so using ip command as WA */
		Nb, err = nlink.ReadNeigh(ctx, "")
		/*	neigh.NS.Neigh0 , neigh.NS.Err = netlink.NeighList(0, netlink.FAMILY_V4)
			if neigh.NS.Err != nil {
			    log.Print("Failed to NeighList: %v", neigh.NS.Err)
			}
		*/
	} else {
		Nb, err = nlink.ReadNeigh(ctx, path.Base(v.Name))
		/*     vrf, _ := netlink.LinkByName(v.Name)
		neigh.NS.Neigh0 , neigh.Err = netlink.NeighList(vrf.Attrs().Index, netlink.FAMILY_V4)
		if neigh.NS.Err != nil {
		    log.Print("Failed to NeighList: %v", neigh.NS.Err)
		}
		*/
	}
	if len(Nb) != 3 && err == nil {
		N = cmdProcessNb(Nb, v.Name)
	}
	addNeigh(N)
}

// NHRouteInfo neighbor route info
type NHRouteInfo struct {
	ID       int
	Gateway  string
	Dev      string
	Scope    string
	Protocol string
	Flags    []string
}

// RouteCmdInfo structure
type RouteCmdInfo struct {
	Type     string
	Dst      string
	Nhid     int
	Gateway  string
	Dev      string
	Protocol string
	Scope    string
	Prefsrc  string
	Metric   int
	Flags    []string
	Weight   int
	VRF      *infradb.Vrf
	Table    int
	NhInfo   NHRouteInfo // {id gateway Dev scope protocol flags}
}

// preFilterMac filter the mac
func preFilterMac(f FdbEntryStruct) bool {
	// TODO m.nexthop.dst
	// if f.VlanID != 0 || !(reflect.ValueOf(f.Nexthop.Dst).IsZero()){
	if f.VlanID != 0 { // || !(reflect.ValueOf(f.Nexthop.Dst).IsZero()){
		log.Printf("%d vlan \n", len(f.Nexthop.Dst.String()))
		return true
	}
	return false
}

// cmdProcessRt process the route command
func cmdProcessRt(v *infradb.Vrf, r string, t int) RouteList {
	var RouteData []RouteCmdInfo
	if len(r) <= 3 {
		log.Println("NL: Error in the cmd:", r)
		var route RouteList
		return route
	}
	CPs := strings.Split(r[2:len(r)-3], "},{")
	for i := 0; i < len(CPs); i++ {
		var ri RouteCmdInfo
		log.Println(CPs[i])
		err := json.Unmarshal([]byte(fmt.Sprintf("{%v}", CPs[i])), &ri)
		if err != nil {
			log.Println("error-", err)
		}
		RouteData = append(RouteData, ri)
	}
	route := Parse_Route(v, RouteData, t)
	return route
}

// readRouteFromIP reads the routes from ip
func readRouteFromIP(v *infradb.Vrf) {
	var Rl RouteList
	var rm []RouteCmdInfo // map[string]string
	var Rt1 int
	// TODO
	for _, routeSt := range v.Metadata.RoutingTable {
		Rt1 = int(*routeSt)
		Raw, err := nlink.ReadRoute(ctx, strconv.Itoa(Rt1))
		if err != nil {
			log.Printf("Err Command route\n")
			return
		}
		Rl = cmdProcessRt(v, Raw, Rt1)
		for _, r := range Rl.RS {
			addRoute(r)
		}
	}
	nl := getNeighborRoutes() // Add extra routes for Resolved neighbors on connected subnets
	for i := 0; i < len(nl); i++ {
		rm = append(rm, nl[i])
	}
	nr := Parse_Route(v, rm, 0)
	for _, r := range nr.RS {
		addRoute(r)
	}
}

// readRoutes reads the routes
func readRoutes(v *infradb.Vrf) {
	//	for _,str := range link_int  {
	//	 link,err := netlink.LinkByName(I.Attrs().Name)
	//		if err != nil {
	//			log.Println(err)
	//			return
	//		}
	// log.Printf("Ifname %s\n",str)
	// var routes RouteList
	//	routes.r,routes.Err = netlink.RouteList(nil, netlink.FAMILY_MPLS)
	//	routes.r,routes.Err = netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{
	//		LinkIndex: link.Attrs().Index,
	// Table :  int(v.Routing_tables[0]),
	//	}, netlink.RT_FILTER_IIF )
	//	if routes.Err != nil {
	//		log.Println(routes.Err)
	//	}
	//	log.Println(v.Routing_tables)
	readRouteFromIP(v)
	// dumpRouteDB()
}

func notifyAddDel(r interface{}, event string) {
	log.Printf("Notify event: %s\n", event)
	EventBus.Publish(event, r)
}

// notifyEvents array
var notifyEvents = []string{"_added", "_updated", "_deleted"}

//nolint
func notify_changes(new_db map[interface{}]interface{}, old_db map[interface{}]interface{}, event []string) {
	var DB1, DB2 = make(map[interface{}]interface{}), make(map[interface{}]interface{})
	DB2 = old_db
	DB1 = new_db
	/* Checking the Updated entries in the netlink db by comparing the individual keys and their corresponding values in old and new db copies
	   entries with same keys with different values and send the notification to vendor specific module */
	for k1, v1 := range DB1 {
		for k2, v2 := range DB2 {
			if k1 == k2 {
				if !reflect.DeepEqual(v1, v2) {
					// To Avoid in-correct update notification due to race condition in which metadata is nil in new entry and crashing in dcgw module
					if strings.Contains(event[1], "route") || strings.Contains(event[1], "nexthop") {
						var Rv RouteStruct
						var Nv NexthopStruct
						if strings.Contains(event[1], "route") {
							Rv = v1.(RouteStruct)
							if Rv.Vrf.Status.VrfOperStatus == infradb.VrfOperStatusToBeDeleted {
								notifyAddDel(Rv, event[2])
								delete(new_db, k1)
								delete(old_db, k2)
								break
							}
						} else {
							Nv = v1.(NexthopStruct)
							if Nv.Vrf.Status.VrfOperStatus == infradb.VrfOperStatusToBeDeleted {
								notifyAddDel(Nv, event[2])
								delete(new_db, k1)
								delete(old_db, k2)
								break
							}
						}
					}
					notifyAddDel(v1, event[1])
				}
				delete(new_db, k1)
				delete(old_db, k2)
				break
			}
		}
	}
	for _, r := range new_db { // Added entries notification cases
		notifyAddDel(r, event[0])
	}
	for _, r := range old_db { // Deleted entires notification cases
		notifyAddDel(r, event[2])
	}
}

// readFDB read the fdb from db
func readFDB() []FdbEntryStruct {
	var fdbs []FdbIPStruct
	var macs []FdbEntryStruct
	var fs FdbEntryStruct

	CP, err := nlink.ReadFDB(ctx)
	if err != nil || len(CP) == 3 {
		// log.Fatal("FDB: Command error\n") //TODO
		return macs
	}

	CPs := strings.Split(CP[2:len(CP)-3], "},{")
	for i := 0; i < len(CPs); i++ {
		var fi FdbIPStruct
		err := json.Unmarshal([]byte(fmt.Sprintf("{%v}", CPs[i])), &fi)
		if err != nil {
			log.Println("error-", err)
		}
		fdbs = append(fdbs, fi)
	}
	for _, m := range fdbs {
		fs = ParseFdb(m, fs)
		if preFilterMac(fs) {
			macs = append(macs, fs)
		}
	}
	return macs
}

// lookupRoute check the routes
func lookupRoute(dst net.IP, v *infradb.Vrf) RouteStruct {
	// FIXME: If the semantic is to return the current entry of the NetlinkDB
	//  routing table, a direct lookup in Linux should only be done as fallback
	//  if there is no match in the DB.
	var CP string
	var err error
	if v.Spec.Vni != nil {
		CP, err = nlink.RouteLookup(ctx, dst.String(), path.Base(v.Name))
	} else {
		CP, err = nlink.RouteLookup(ctx, dst.String(), "")
	}
	if err != nil {
		log.Fatal("Command error\n")
		return RouteStruct{}
	}
	r := cmdProcessRt(v, CP, int(*v.Metadata.RoutingTable[0]))
	log.Printf("%+v\n", r)
	if len(r.RS) != 0 {
		R1 := r.RS[0]
		// ###  Search the LatestRoutes DB snapshot if that exists, else
		// ###  the current DB Route table.
		var RouteTable map[RouteKey]RouteStruct
		if len(LatestRoutes) != 0 {
			RouteTable = LatestRoutes
		} else {
			RouteTable = Routes
		}
		RDB := RouteTable[R1.Key]
		if !reflect.ValueOf(RDB).IsZero() {
			// Return the existing route in the DB
			return RDB
		}
		// Return the just constructed non-DB route
		return R1
	}

	log.Printf("Failed to lookup route {dst} in VRF {v}")
	return RouteStruct{}
}

//nolint
func (nexthop NexthopStruct) annotate() NexthopStruct {
	nexthop.Metadata = make(map[interface{}]interface{})
	var phyFlag bool
	phyFlag = false
	for k := range phyPorts {
		if NameIndex[nexthop.nexthop.LinkIndex] == k {
			phyFlag = true
		}
	}
	if (!reflect.ValueOf(nexthop.nexthop.Gw).IsZero()) && nexthop.nexthop.LinkIndex != 0 && strings.HasPrefix(NameIndex[nexthop.nexthop.LinkIndex], path.Base(nexthop.Vrf.Name)+"-") && !nexthop.Local {
		nexthop.NhType = SVI
		link, _ := vn.LinkByName(NameIndex[nexthop.nexthop.LinkIndex])
		nexthop.Metadata["smac"] = link.Attrs().HardwareAddr.String()
		if !reflect.ValueOf(nexthop.Neighbor).IsZero() {
			if nexthop.Neighbor.Type == SVI {
				nexthop.Metadata["dmac"] = nexthop.Neighbor.Neigh0.HardwareAddr.String()
				nexthop.Metadata["egress_vport"] = nexthop.Neighbor.Metadata["vport_id"]
				nexthop.Metadata["vlanID"] = nexthop.Neighbor.Metadata["vlanID"]
				nexthop.Metadata["portType"] = nexthop.Neighbor.Metadata["portType"]
			}
		} else {
			nexthop.Resolved = false
			log.Printf("Failed to gather data for nexthop on physical port\n")
		}
	} else if (!reflect.ValueOf(nexthop.nexthop.Gw).IsZero()) && phyFlag && !nexthop.Local {
		nexthop.NhType = PHY
		link1, _ := vn.LinkByName(NameIndex[nexthop.nexthop.LinkIndex])
		if link1 == nil {
			return nexthop
		}
		nexthop.Metadata["smac"] = link1.Attrs().HardwareAddr.String()
		nexthop.Metadata["egress_vport"] = phyPorts[nexthop.nexthop.Gw.String()]
		if !reflect.ValueOf(nexthop.Neighbor).IsZero() {
			if nexthop.Neighbor.Type == PHY {
				nexthop.Metadata["dmac"] = nexthop.Neighbor.Neigh0.HardwareAddr.String()
			}
		} else {
			nexthop.Resolved = false
			log.Printf("Failed to gather data for nexthop on physical port")
		}
	} else if (!reflect.ValueOf(nexthop.nexthop.Gw).IsZero()) && NameIndex[nexthop.nexthop.LinkIndex] == fmt.Sprintf("br-%s", path.Base(nexthop.Vrf.Name)) && !nexthop.Local {
		nexthop.NhType = VXLAN
		G, _ := infradb.GetVrf(nexthop.Vrf.Name)
		var detail map[string]interface{}
		var Rmac net.HardwareAddr
		for _, com := range G.Status.Components {
			if com.Name == "frr" {
				err := json.Unmarshal([]byte(com.Details), &detail)
				if err != nil {
					log.Println("Error:", err)
				}
				rmac, found := detail["rmac"].(string)
				if !found {
					log.Println("Key 'rmac' not found")
					break
				}
				Rmac, err = net.ParseMAC(rmac)
				if err != nil {
					log.Println("Error parsing MAC address:", err)
				}
			}
		}
		nexthop.Metadata["inner_smac"] = Rmac.String()
		if reflect.ValueOf(Rmac).IsZero() {
			nexthop.Resolved = false
		}
		vtepip := G.Spec.VtepIP.IP
		nexthop.Metadata["local_vtep_ip"] = vtepip.String()
		nexthop.Metadata["remote_vtep_ip"] = nexthop.nexthop.Gw.String()
		nexthop.Metadata["vni"] = *nexthop.Vrf.Spec.Vni
		if !reflect.ValueOf(nexthop.Neighbor).IsZero() {
			// if nexthop.neighbor.Type == SVI{
			nexthop.Metadata["inner_dmac"] = nexthop.Neighbor.Neigh0.HardwareAddr.String()
			G, _ := infradb.GetVrf("//network.opiproject.org/vrfs/GRD")
			r := lookupRoute(nexthop.nexthop.Gw, G)
			if !reflect.ValueOf(r).IsZero() {
				// For now pick the first physical nexthop (no ECMP yet)
				phyNh := r.Nexthops[0]
				link, _ := vn.LinkByName(NameIndex[phyNh.nexthop.LinkIndex])
				nexthop.Metadata["phy_smac"] = link.Attrs().HardwareAddr.String()
				nexthop.Metadata["egress_vport"] = phyPorts[NameIndex[phyNh.nexthop.LinkIndex]]
				if !reflect.ValueOf(phyNh.Neighbor).IsZero() {
					nexthop.Metadata["phy_dmac"] = phyNh.Neighbor.Neigh0.HardwareAddr.String()
				} else {
					// The VXLAN nexthop can only be installed when the phy_nexthops are Resolved.
					nexthop.Resolved = false
				}
			}
		} else {
			nexthop.Resolved = false
			// return ""
		}
	} else {
		nexthop.NhType = ACC
		link1, err := vn.LinkByName("rep-" + path.Base(nexthop.Vrf.Name))
		if err != nil {
			log.Printf("Error in getting rep information\n")
			// return ""
		}
		if link1 == nil {
			return nexthop
		}

		nexthop.Metadata["dmac"] = link1.Attrs().HardwareAddr.String()
		nexthop.Metadata["egress_vport"] = 0xb // ipu_db.vport_id_from_mac_address(mac)
		if reflect.ValueOf(nexthop.Vrf.Spec.Vni).IsZero() {
			nexthop.Metadata["vlanID"] = uint32(4089)
		} else {
			nexthop.Metadata["vlanID"] = *nexthop.Vrf.Spec.Vni
		}
	}
	return nexthop
}

//nolint
func (l2n L2NexthopStruct) annotate() L2NexthopStruct {
	// Annotate certain L2 Nexthops with additional information from LB and GRD
	// TODO
	// LB := l2n.lb
	// if !(reflect.ValueOf(LB).IsZero()) {
	//  if l2n.Type == SVI {
	// MAC address learned on SVI interface of bridge
	//    if reflect.ValueOf(LB.Svi).IsZero() {
	//		log.Printf("Error in L2nexthop annotate\n")
	//		return
	//	}
	//      l2n.Metadata["vrf_id"] = LB.Svi.vni
	if l2n.Type == VXLAN {
		//# Remote EVPN MAC address learned on the VXLAN interface
		//# The L2 nexthop must have a destination IP address in dst
		l2n.Resolved = false
		l2n.Metadata["local_vtep_ip"] = "0.0.0.0" // LB.vtep
		l2n.Metadata["remote_vtep_ip"] = l2n.Dst
		l2n.Metadata["vni"] = 2000 // LB.vni
		//# The below physical nexthops are needed to transmit the VXLAN-encapsuleted packets
		//# directly from the nexthop table to a physical port (and avoid another recirculation
		//# for route lookup in the GRD table.)
		// GRD = InfraDB.get_VRF(vni=None)  TODO : need infraDB for fetching
		VRF, _ := infradb.GetVrf("//network.opiproject.org/vrfs/GRD")
		r := lookupRoute(l2n.Dst, VRF)
		if !reflect.ValueOf(r).IsZero() {
			//  # For now pick the first physical nexthop (no ECMP yet)
			phyNh := r.Nexthops[0]
			link, _ := vn.LinkByName(NameIndex[phyNh.nexthop.LinkIndex])
			l2n.Metadata["phy_smac"] = link.Attrs().HardwareAddr.String()
			l2n.Metadata["egress_vport"] = phyPorts[NameIndex[phyNh.nexthop.LinkIndex]]
			if !reflect.ValueOf(phyNh.Neighbor).IsZero() {
				if phyNh.Neighbor.Type == PHY {
					l2n.Metadata["phy_dmac"] = phyNh.Neighbor.Neigh0.HardwareAddr.String()
				} else {
					log.Printf("Error: Neighbor type not PHY\n")
				}
				l2n.Resolved = false
			}
		}
	} else if l2n.Type == BRIDGEPORT {
		// BridgePort as L2 nexthop
		l2n.Metadata["vport_id"] = "2000" // TODO l2n.bp.vport_id
		l2n.Metadata["portType"] = "host" // TODO l2n.bp.Type
	}
	//}
	return l2n
}

// annotate the route
func (fdb FdbEntryStruct) annotate() FdbEntryStruct {
	if fdb.VlanID == 0 {
		return fdb
	}
	// TODO
	// if not self.lb: return

	fdb.Metadata = make(map[interface{}]interface{})
	l2n := fdb.Nexthop
	if !reflect.ValueOf(l2n).IsZero() {
		fdb.Metadata["nh_id"] = l2n.ID
		// TODO
		/*if l2n.Type == VXLAN{
					sibling = NetlinkDB.LatestFDB.get((None, self.mac))
		                l2n.dst = sibling.nexthop.dst if sibling else None
		            # The relevant directions for the FDB entry are derived from the nexthop type

				}*/
		/*if l2n.Type == VXLAN {
			// fdb.Metadata["direction"] = string(TX)
			fdb.Metadata["direction"] = fmt.Sprint(TX)
		} else if l2n.Type == SVI || l2n.Type == BRIDGEPORT {
			// fdb.Metadata["direction"] = string(RXTX)
			fdb.Metadata["direction"] = fmt.Sprint(RXTX)
		} else {
			fdb.Metadata["direction"] = "NONE"
		}*/
		switch l2n.Type {
		case VXLAN:
			// fdb.Metadata["direction"] = string(TX)
			fdb.Metadata["direction"] = fmt.Sprint(TX)
		case BRIDGEPORT, SVI:
			// fdb.Metadata["direction"] = string(RXTX)
			fdb.Metadata["direction"] = fmt.Sprint(RXTX)

		default:
			fdb.Metadata["direction"] = "NONE"
		}
		// TODO
		// logger.debug(f"Annotated {self}: extra={self.Metadata}")
	}
	return fdb
}

// annotateDBEntries annonates the database entries
func annotateDBEntries() {
	for _, nexthop := range LatestNexthop {
		nexthop = nexthop.annotate()
		LatestNexthop[nexthop.Key] = nexthop
	}
	for _, r := range LatestRoutes {
		r = r.annotate()
		LatestRoutes[r.Key] = r
	}

	for _, m := range LatestFDB {
		m = m.annotate()
		LatestFDB[m.Key] = m
	}
	for _, l2n := range LatestL2Nexthop {
		l2n = l2n.annotate()
		LatestL2Nexthop[l2n.Key] = l2n
	}
}

// installFilterRoute install the route filter
func installFilterRoute(routeSt *RouteStruct) bool {
	var nh []NexthopStruct
	for _, n := range routeSt.Nexthops {
		if n.Resolved {
			nh = append(nh, n)
		}
	}
	routeSt.Nexthops = nh
	keep := checkRtype(routeSt.NlType) && len(nh) != 0 && strings.Compare(routeSt.Route0.Dst.IP.String(), "0.0.0.0") != 0
	return keep
}

// checkNhType checks the nighbor type
func checkNhType(nType int) bool {
	ntype := []int{PHY, SVI, ACC, VXLAN}
	for _, i := range ntype {
		if i == nType {
			return true
		}
	}
	return false
}

// installFilterNH install the neighbor filter
func installFilterNH(nh NexthopStruct) bool {
	check := checkNhType(nh.NhType)
	keep := check && nh.Resolved && len(nh.RouteRefs) != 0
	//	if !keep {
	//		log.Printf("install_filter: dropping {%v}",nh)
	//	}
	return keep
}

func checkFdbType(fdbtype int) bool {
	var portType = []int{BRIDGEPORT, VXLAN}
	for _, port := range portType {
		if port == fdbtype {
			return true
		}
	}
	return false
}

// installFilterFDB install fdb filer
func installFilterFDB(fdb FdbEntryStruct) bool {
	// Drop entries w/o VLAN ID or associated LogicalBridge ...
	// ... other than with L2 nexthops of type VXLAN and BridgePort ...
	// ... and VXLAN entries with unresolved underlay nextop.
	keep := !reflect.ValueOf(fdb.VlanID).IsZero() && /*reflect.ValueOf(fdb.lb).IsZero() && */ checkFdbType(fdb.Type) && fdb.Nexthop.Resolved
	if !keep {
		log.Printf("install_filter: dropping {%v}", fdb)
	}
	return keep
}

// installFilterL2N install the l2 filter
func installFilterL2N(l2n L2NexthopStruct) bool {
	keep := !(reflect.ValueOf(l2n.Type).IsZero() && l2n.Resolved && reflect.ValueOf(l2n.FdbRefs).IsZero())
	if !keep {
		log.Printf("install_filter FDB: dropping {%+v}", l2n)
	}
	return keep
}

//nolint
func applyInstallFilters() {
	for K, r := range LatestRoutes {
		if !installFilterRoute(&r) {
			// Remove route from its nexthop(s)
			delete(LatestRoutes, K)
		}
	}

	for k, nexthop := range LatestNexthop {
		if !installFilterNH(nexthop) {
			delete(LatestNexthop, k)
		}
	}

	for k, m := range LatestFDB {
		if !installFilterFDB(m) { // TODO Making "== true" (Original != ture) fix it later once InfraDB is ready
			delete(LatestFDB, k)
		}
	}
	for k, L2 := range LatestL2Nexthop {
		if !installFilterL2N(L2) {
			delete(LatestL2Nexthop, k)
		}
	}
}

// oldgenmap old map
var oldgenmap = make(map[interface{}]interface{})

// latestgenmap latest map
var latestgenmap = make(map[interface{}]interface{})

// notifyDBChanges notify the database changes
func notifyDBChanges() {
	var routeEventStr = make([]string, len(notifyEvents))
	var nexthopEventStr = make([]string, len(notifyEvents))
	var fdbEventStr = make([]string, len(notifyEvents))
	var l2nexthopEventStr = make([]string, len(notifyEvents))

	for _, s := range notifyEvents {
		routeEventStr = append(routeEventStr, "route"+s)
		nexthopEventStr = append(nexthopEventStr, "nexthop"+s)
		fdbEventStr = append(fdbEventStr, "fdb_entry"+s)
		l2nexthopEventStr = append(l2nexthopEventStr, "l2_nexthop"+s)
	}
	type NlDBCopy struct {
		RDB   map[RouteKey]RouteStruct
		NDB   map[NexthopKey]NexthopStruct
		FBDB  map[FDBKey]FdbEntryStruct
		L2NDB map[L2NexthopKey]L2NexthopStruct
	}
	latestdb := NlDBCopy{RDB: LatestRoutes, NDB: LatestNexthop, FBDB: LatestFDB, L2NDB: LatestL2Nexthop}
	olddb := NlDBCopy{RDB: Routes, NDB: Nexthops, FBDB: FDB, L2NDB: L2Nexthops}
	var eventStr []interface{}
	eventStr = append(eventStr, routeEventStr)
	eventStr = append(eventStr, nexthopEventStr)
	eventStr = append(eventStr, fdbEventStr)
	eventStr = append(eventStr, l2nexthopEventStr)
	// Routes
	oldgenmap = make(map[interface{}]interface{})
	latestgenmap = make(map[interface{}]interface{})
	for k, v := range latestdb.RDB { // (map[RouteKey]RouteStruct) {
		latestgenmap[k] = v
	}
	for k, v := range olddb.RDB {
		oldgenmap[k] = v
	}
	notify_changes(latestgenmap, oldgenmap, eventStr[0].([]string))
	// Nexthops
	oldgenmap = make(map[interface{}]interface{})
	latestgenmap = make(map[interface{}]interface{})
	for k, v := range latestdb.NDB { // (map[RouteKey]RouteStruct) {
		latestgenmap[k] = v
	}
	for k, v := range olddb.NDB {
		oldgenmap[k] = v
	}
	notify_changes(latestgenmap, oldgenmap, eventStr[1].([]string))
	// FDB
	oldgenmap = make(map[interface{}]interface{})
	latestgenmap = make(map[interface{}]interface{})
	for k, v := range latestdb.FBDB { // (map[RouteKey]RouteStruct) {
		latestgenmap[k] = v
	}
	for k, v := range olddb.FBDB {
		oldgenmap[k] = v
	}
	notify_changes(latestgenmap, oldgenmap, eventStr[2].([]string))
	// L2Nexthop
	oldgenmap = make(map[interface{}]interface{})
	latestgenmap = make(map[interface{}]interface{})
	for k, v := range latestdb.L2NDB { // (map[RouteKey]RouteStruct) {
		latestgenmap[k] = v
	}
	for k, v := range olddb.L2NDB {
		oldgenmap[k] = v
	}
	notify_changes(latestgenmap, oldgenmap, eventStr[3].([]string))
}

// resyncWithKernel fun resyncs with kernal db
func resyncWithKernel() {
	// Build a new DB snapshot from netlink and other sources
	readLatestNetlinkState()
	// Annotate the latest DB entries
	annotateDBEntries()
	// Filter the latest DB to retain only entries to be installed
	applyInstallFilters()
	// Compute changes between current and latest DB versions and inform subscribers about the changes
	notifyDBChanges()
	Routes = LatestRoutes
	Nexthops = LatestNexthop
	Neighbors = LatestNeighbors
	FDB = LatestFDB
	L2Nexthops = LatestL2Nexthop
	DeleteLatestDB()
}

// DeleteLatestDB deletes the latest db snap
func DeleteLatestDB() {
	LatestRoutes = make(map[RouteKey]RouteStruct)
	LatestNeighbors = make(map[NeighKey]NeighStruct)
	LatestNexthop = make(map[NexthopKey]NexthopStruct)
	LatestFDB = make(map[FDBKey]FdbEntryStruct)
	LatestL2Nexthop = make(map[L2NexthopKey]L2NexthopStruct)
}

// monitorNetlink moniters the netlink
func monitorNetlink(_ bool) {
	for !stopMonitoring {
		log.Printf("netlink: Polling netlink databases.")
		resyncWithKernel()
		log.Printf("netlink: Polling netlink databases completed.")
		time.Sleep(time.Duration(pollInterval) * time.Second)
	}
	log.Println("netlink: Stopped periodic polling. Waiting for Infra DB cleanup to finish")
	time.Sleep(2 * time.Second)
	log.Println("netlink: One final netlink poll to identify what's still left.")
	resyncWithKernel()
	// Inform subscribers to delete configuration for any still remaining Netlink DB objects.
	log.Println("netlink: Delete any residual objects in DB")
	for _, r := range Routes {
		notifyAddDel(r, "route_deleted")
	}
	for _, nexthop := range Nexthops {
		notifyAddDel(nexthop, "nexthop_deleted")
	}
	for _, m := range FDB {
		notifyAddDel(m, "FDB_entry_deleted")
	}
	log.Println("netlink: DB cleanup completed.")
	// wg.Done()
}

// Init function intializes config
func Init() {

	// var config Config_t
	/*yfile, err := ioutil.ReadFile("config.yaml")
	if err != nil {
		log.Println(err)
		// os.Exit(0)
	}
	err2 := yaml.Unmarshal(yfile, &config)
	if err2 != nil {
		log.Fatal(err2)
	}*/
	// wg.Add(1)
	pollInterval = config.GlobalConfig.Netlink.Poll_interval
	log.Println(pollInterval)
	//TODO:fix this

	// brTenant = config.Linux_frr.Br_tenant
	// log.Println(brTenant)
	nlEnabled := config.GlobalConfig.Netlink.Enabled
	if !nlEnabled {
		log.Println("netlink_monitor disabled")
		return
	}
	for i := 0; i < len(config.GlobalConfig.Netlink.Phy_ports); i++ {
		phyPorts[config.GlobalConfig.Netlink.Phy_ports[i].Name] = config.GlobalConfig.Netlink.Phy_ports[i].Vsi
	}
	getlink()
	ctx = context.Background()
	nlink = utils.NewNetlinkWrapper()
	go monitorNetlink(config.GlobalConfig.P4.Enabled) // monitor Thread started
	// log.Println("Started netlink_monitor thread with {pollInterval} s poll interval.")
	//	time.Sleep(1 * time.Second)
	//	stopMonitoring = true
	// wg.Wait()
}
