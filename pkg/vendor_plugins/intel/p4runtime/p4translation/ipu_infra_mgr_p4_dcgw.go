package p4translation

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net"
	"reflect"
	"strconv"
	"strings"

	"github.com/opiproject/opi-evpn-bridge/pkg/infradb"
	netlink_polling "github.com/opiproject/opi-evpn-bridge/pkg/netlink"
	p4client "github.com/opiproject/opi-evpn-bridge/pkg/vendor_plugins/intel/p4runtime/p4driverAPI"
	binarypack "github.com/roman-kachanovsky/go-binary-pack/binary-pack"
)

var TcamPrefix = struct {
	GRD, VRF uint32
}{
	GRD: 1,
	VRF: 2, // taking const for now as not imported VRF

}

var Direction = struct {
	Rx, Tx int
}{
	Rx: 0,
	Tx: 1,
}

var Vlan = struct {
	GRD, PHY0, PHY1, PHY2, PHY3 uint16
}{
	GRD:  4089,
	PHY0: 4090,
	PHY1: 4091,
	PHY2: 4092,
	PHY3: 4093,
}

var PortId = struct {
	PHY0, PHY1, PHY2, PHY3 int
}{
	PHY0: 0,
	PHY1: 1,
	PHY2: 2,
	PHY3: 3,
}

var EntryType = struct {
	BP, L3_NH, L2_NH, TRIE_I uint32
}{
	BP:     0,
	L3_NH:  1,
	L2_NH:  2,
	TRIE_I: 3,
}

var ModPointer = struct {
	IGNORE_PTR, L2_FLOODING_PTR, PTR_MIN_RANGE, PTR_MAX_RANGE uint32
}{
	IGNORE_PTR:      0,
	L2_FLOODING_PTR: 1,
	PTR_MIN_RANGE:   2,
	PTR_MAX_RANGE:   uint32(math.Pow(2, 16)) - 1,
}

var TrieIndex = struct {
	TRIEIDX_MIN_RANGE, TRIEIDX_MAX_RANGE uint32
}{
	TRIEIDX_MIN_RANGE: 1,
	TRIEIDX_MAX_RANGE: uint32(math.Pow(2, 16)) - 1,
}

var RefCountOp = struct {
	RESET, INCREMENT, DECREMENT int
}{
	RESET:     0,
	INCREMENT: 1,
	DECREMENT: 2,
}
var ipu_db = struct {
	TRUNK, ACCESS int
}{
	TRUNK:  0,
	ACCESS: 1,
}

type IdPool struct {
	_in_use_ids    map[interface{}]uint32
	_ref_count     map[interface{}]uint32
	_available_ids []uint32
}

func (i IdPool) IdPoolInit(min uint32, max uint32) IdPool {
	for j := min; j <= (max + 1); j++ {
		i._available_ids = append(i._available_ids, j)
	}
	return i
}

var Ptr_Pool IdPool
var ptr_pool = Ptr_Pool.IdPoolInit(ModPointer.PTR_MIN_RANGE, ModPointer.PTR_MAX_RANGE)
var trie_index_pool = Ptr_Pool.IdPoolInit(TrieIndex.TRIEIDX_MIN_RANGE, TrieIndex.TRIEIDX_MAX_RANGE)

func (i IdPool) get_id(key_type uint32, key []interface{}) uint32 {
	var full_key interface{}
	full_key = fmt.Sprintf("%d%d", key_type, key)
	// (full_key, key_type, key)
	var ptr_id uint32 = ptr_pool._in_use_ids[full_key]
	if ptr_id == 0 {
		ptr_id = ptr_pool._available_ids[0]
		ptr_pool._available_ids = ptr_pool._available_ids[1:]
		if ptr_pool._in_use_ids == nil {
			ptr_pool._in_use_ids = make(map[interface{}]uint32)
		}
		ptr_pool._in_use_ids[full_key] = ptr_id
	}
	return ptr_id
}

func (i IdPool) get_used_id(key_type uint32, key []interface{}) uint32 {
	var full_key interface{}
	full_key = fmt.Sprintf("%d%d", key_type, key)
	var ptr_id uint32 = ptr_pool._in_use_ids[full_key]
	return ptr_id
}

func (i IdPool) put_id(key_type uint32, key []interface{}, ptr_id uint32) error {
	var full_key interface{}
	full_key = fmt.Sprintf("%d%d", key_type, key)
	ptr_id = ptr_pool._in_use_ids[full_key]
	if ptr_id == 0 {
		return fmt.Errorf("TODO") // or log
	}
	delete(ptr_pool._in_use_ids, full_key)
	ptr_pool._available_ids = append(ptr_pool._available_ids, ptr_id)
	return nil
}

func (i IdPool) ref_count(key_type uint32, key []interface{}, op int) uint32 {
	var full_key interface{}
	var ref_count uint32
	full_key = fmt.Sprintf("%d%d", key_type, key)
	for key := range i._ref_count {
		if key == full_key {
			ref_count = i._ref_count[full_key]
			if op == RefCountOp.RESET {
				ref_count = 1
			} else if op == RefCountOp.INCREMENT {
				ref_count += 1
			} else if op == RefCountOp.DECREMENT {
				ref_count -= 1
			}
			i._ref_count[full_key] = ref_count
		} else {
			i._ref_count[full_key] = 1
			return uint32(1)
		}
	}
	return ref_count
}

type Table string

const (
	L3_RT = "linux_networking_control.l3_routing_table" // VRFs routing table in LPM
	//                            TableKeys (
	//                                ipv4_table_lpm_root2,  // Exact
	//                                vrf,                   // LPM
	//                                direction,             // LPM
	//                                dst_ip,                // LPM
	//                            )
	//                            Actions (
	//                                set_neighbor(neighbor),
	//                            )
	L3_RT_HOST = "linux_networking_control.l3_lem_table"
	//                            TableKeys (
	//                                vrf,                   // Exact
	//                                direction,             // Exact
	//                                dst_ip,                // Exact
	//                            )
	//                            Actions (
	//                                set_neighbor(neighbor)
	//                            )
	L3_NH = "linux_networking_control.l3_nexthop_table" // VRFs next hop table
	//                            TableKeys (
	//                                neighbor,              // Exact
	//                                bit32_zeros,           // Exact
	//                            )
	//                            Actions (
	//                               push_dmac_vlan(mod_ptr, vport)
	//                               push_vlan(mod_ptr, vport)
	//                               push_mac(mod_ptr, vport)
	//                               push_outermac_vxlan_innermac(mod_ptr, vport)
	//                               push_mac_vlan(mod_ptr, vport)
	//                            )
	PHY_IN_IP = "linux_networking_control.phy_ingress_ip_table" // PHY ingress table - IP traffic
	//                           TableKeys(
	//                               port_id,                // Exact
	//                               bit32_zeros,            // Exact
	//                           )
	//                           Actions(
	//                               set_vrf_id(tcam_prefix, vport, vrf),
	//                           )
	PHY_IN_ARP = "linux_networking_control.phy_ingress_arp_table" // PHY ingress table - ARP traffic
	//                           TableKeys(
	//                               port_id,                // Exact
	//                               bit32_zeros,            // Exact
	//                           )
	//                           Actions(
	//                               fwd_to_port(port)
	//                           )
	PHY_IN_VXLAN = "linux_networking_control.phy_ingress_vxlan_table" // PHY ingress table - VXLAN traffic
	//                           TableKeys(
	//                               dst_ip
	//                               vni,
	//                               da
	//                           )
	//                           Actions(
	//                               pop_vxlan_set_vrf_id(mod_ptr, tcam_prefix, vport, vrf),
	//                           )
	PHY_IN_VXLAN_L2 = "linux_networking_control.phy_ingress_vxlan_vlan_table"
	//                           Keys {
	//                               dst_ip                  // Exact
	//                               vni                     // Exact
	//                           }
	//                           Actions(
	//                               pop_vxlan_set_vlan_id(mod_ptr, vlan_id, vport)
	//                           )
	POD_IN_ARP_ACCESS = "linux_networking_control.vport_arp_ingress_table"
	//                       Keys {
	//                           vsi,                        // Exact
	//                           bit32_zeros                 // Exact
	//                       }
	//                       Actions(
	//                           fwd_to_port(port),
	//                           send_to_port_mux_access(mod_ptr, vport)
	//                       )
	POD_IN_ARP_TRUNK = "linux_networking_control.tagged_vport_arp_ingress_table"
	//                       Key {
	//                           vsi,                        // Exact
	//                           vid                         // Exact
	//                       }
	//                       Actions(
	//                           send_to_port_mux_trunk(mod_ptr, vport),
	//                           fwd_to_port(port),
	//                           pop_vlan(mod_ptr, vport)
	//                       )
	POD_IN_IP_ACCESS = "linux_networking_control.vport_ingress_table"
	//                       Key {
	//                           vsi,                        // Exact
	//                           bit32_zeros                 // Exact
	//                       }
	//                       Actions(
	//                          fwd_to_port(port)
	//                          set_vlan(vlan_id, vport)
	//                       )
	POD_IN_IP_TRUNK = "linux_networking_control.tagged_vport_ingress_table"
	//                       Key {
	//                           vsi,                        // Exact
	//                           vid                         // Exact
	//                       }
	//                       Actions(
	//                           //pop_vlan(mod_ptr, vport)
	//                           //pop_vlan_set_vrfid(mod_ptr, vport, tcam_prefix, vrf)
	//                           set_vlan_and_pop_vlan(mod_ptr, vlan_id, vport)
	//                       )
	POD_IN_SVI_ACCESS = "linux_networking_control.vport_svi_ingress_table"
	//                       Key {
	//                           vsi,                        // Exact
	//                           da                          // Exact
	//                       }
	//                       Actions(
	//                           set_vrf_id_tx(tcam_prefix, vport, vrf)
	//                           fwd_to_port(port)
	//                       )
	POD_IN_SVI_TRUNK = "linux_networking_control.tagged_vport_svi_ingress_table"
	//                       Key {
	//                           vsi,                        // Exact
	//                           vid,                        // Exact
	//                           da                          // Exact
	//                       }
	//                       Actions(
	//                           pop_vlan_set_vrf_id(tcam_prefix, mod_ptr, vport, vrf)
	//                       )
	PORT_MUX_IN = "linux_networking_control.port_mux_ingress_table"
	//                       Key {
	//                           vsi,                        // Exact
	//                           vid                         // Exact
	//                       }
	//                       Actions(
	//                           set_def_vsi_loopback()
	//                           pop_ctag_stag_vlan(mod_ptr, vport),
	//                           pop_stag_vlan(mod_ptr, vport)
	//                       )
	//    PORT_MUX_RX        = "linux_networking_control.port_mux_rx_table"
	//                       Key {
	//                           vid,                        // Exact
	//                           bit32_zeros                 // Exact
	//                       }
	//                       Actions(
	//                           pop_ctag_stag_vlan(mod_ptr, vport),
	//                           pop_stag_vlan(mod_ptr, vport)
	//                       )
	PORT_MUX_FWD = "linux_networking_control.port_mux_fwd_table"
	//                       Key {
	//                           bit32_zeros                 // Exact
	//                       }
	//                       Actions(
	//                           "linux_networking_control.send_to_port_mux(vport)"
	//                       )
	L2_FWD_LOOP = "linux_networking_control.l2_fwd_rx_table"
	//                       Key {
	//                           da                          // Exact (MAC)
	//                       }
	//                       Actions(
	//                           l2_fwd(port)
	//                       )
	L2_FWD = "linux_networking_control.l2_dmac_table"
	//                       Key {
	//                           vlan_id,                    // Exact
	//                           da,                         // Exact
	//                           direction                   // Exact
	//                       }
	//                       Actions(
	//                           set_neighbor(neighbor)
	//                       )
	L2_NH = "linux_networking_control.l2_nexthop_table"
	//                       Key {
	//                           neighbor                    // Exact
	//                           bit32_zeros                 // Exact
	//                       }
	//                       Actions(
	//                           //push_dmac_vlan(mod_ptr, vport)
	//                           push_stag_ctag(mod_ptr, vport)
	//                           push_vlan(mod_ptr, vport)
	//                           fwd_to_port(port)
	//                           push_outermac_vxlan(mod_ptr, vport)
	//                       )
	TCAM_ENTRIES = "linux_networking_control.ecmp_lpm_root_lut1"

//                       Key {
//                           tcam_prefix,                 // Exact
//                           MATCH_PRIORITY,              // Exact
//                       }
//                       Actions(
//                           None(ipv4_table_lpm_root1)
//                       )

)

type ModTable string

const (
	PUSH_VLAN = "linux_networking_control.vlan_push_mod_table"
	//                        src_action="push_vlan"
	//			  Actions(
	// 				vlan_push(pcp, dei, vlan_id),
	//                        )
	PUSH_MAC_VLAN = "linux_networking_control.mac_vlan_push_mod_table"
	//                       src_action=""
	//                       Actions(
	//                          update_smac_dmac_vlan(src_mac_addr, dst_mac_addr, pcp, dei, vlan_id)
	PUSH_DMAC_VLAN = "linux_networking_control.dmac_vlan_push_mod_table"
	//                        src_action="push_dmac_vlan",
	//                       Actions(
	//                           dmac_vlan_push(pcp, dei, vlan_id, dst_mac_addr),
	//                        )
	MAC_MOD = "linux_networking_control.mac_mod_table"
	//                       src_action="push_mac"
	//                        Actions(
	//                            update_smac_dmac(src_mac_addr, dst_mac_addr),
	//                        )
	PUSH_VXLAN_HDR = "linux_networking_control.omac_vxlan_imac_push_mod_table"
	//                       src_action="push_outermac_vxlan_innermac"
	//                       Actions(
	//                           omac_vxlan_imac_push(outer_smac_addr,
	//                                                outer_dmac_addr,
	//                                                src_addr,
	//                                                dst_addr,
	//                                                dst_port,
	//                                                vni,
	//                                                inner_smac_addr,
	//                                                inner_dmac_addr)
	//                       )
	POD_OUT_ACCESS = "linux_networking_control.vlan_encap_ctag_stag_mod_table"
	//                       src_actions="send_to_port_mux_access"
	//                       Actions(
	//                           vlan_push_access(pcp, dei, ctag_id, pcp_s, dei_s, stag_id, dst_mac)
	//                       )
	POD_OUT_TRUNK = "linux_networking_control.vlan_encap_stag_mod_table"
	//                       src_actions="send_to_port_mux_trunk"
	//                       Actions(
	//                           vlan_push_trunk(pcp, dei, stag_id, dst_mac)
	//                       )
	POP_CTAG_STAG = "linux_networking_control.vlan_ctag_stag_pop_mod_table"
	//                       src_actions=""
	//                       Actions(
	//                           vlan_ctag_stag_pop()
	//                       )
	POP_STAG = "linux_networking_control.vlan_stag_pop_mod_table"
	//                       src_actions=""
	//                       Actions(
	//                           vlan_stag_pop()
	//                       )
	PUSH_QNQ_FLOOD = "linux_networking_control.vlan_encap_ctag_stag_flood_mod_table"
	//                       src_action="l2_nexthop_table.push_stag_ctag()"
	//                       Action(
	//                           vlan_push_stag_ctag_flood()
	//                       )
	PUSH_VXLAN_OUT_HDR = "linux_networking_control.omac_vxlan_push_mod_table"

//                      src_action="l2_nexthop_table.push_outermac_vxlan()"
//			Action(
//                           omac_vxlan_push(outer_smac_addr, outer_dmac_addr, src_addr, dst_addr, dst_port, vni)
//                       )

)

/*func set_mux_vsi(representors map[string]string) string{
	var mux_vsi:= representors["vrf_mux"][0]
	return mux_vsi
}*/

func _is_l3vpn_enabled(VRF *infradb.Vrf) uint32 {
	return *VRF.Spec.Vni
}

func bigEndian16(id uint32) interface{} {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, uint16(id))
	unpackedData := binary.BigEndian.Uint16(buf)
	return unpackedData
}

func _big_endian_16(id interface{}) interface{} {
	var bp = new(binarypack.BinaryPack)
	var pack_format = []string{"H"}
	var value = []interface{}{id}
	var packed_data, err = bp.Pack(pack_format, value)
	if err != nil {
		log.Println(err)
	}
	var unpacked_data = binary.BigEndian.Uint16(packed_data)
	return unpacked_data
}

func _big_endian_32(id interface{}) interface{} {
	var bp = new(binarypack.BinaryPack)
	var pack_format = []string{"I"}
	var value = []interface{}{id}
	var packed_data, err = bp.Pack(pack_format, value)
	if err != nil {
		log.Println(err)
	}
	var unpacked_data = binary.BigEndian.Uint32(packed_data)
	return unpacked_data
}

func _to_egress_vsi(vsi_id int) int {
	return vsi_id + 16
}

/*
def _ip_addr(ip_addr):

	net = ipaddress.ip_network(ip_addr)

	# TODO: this is a ovs-p4ctl specific format, move to the driver
	return int(net.network_address)
*/
func _directions_of(entry interface{}) []int {
	var directions []int
	var direction int
	switch entry.(type) {
	case netlink_polling.RouteStruct:
		// entry := netlink_polling.RouteStruct{}
		direction, _ = entry.(netlink_polling.RouteStruct).Metadata["direction"].(int)
	case netlink_polling.FdbEntryStruct:
		// s := netlink_polling.FdbEntryStruct{}
		direction, _ = entry.(netlink_polling.RouteStruct).Metadata["direction"].(int)
	}
	if direction == int(netlink_polling.TX) || direction == int(netlink_polling.RXTX) {
		directions = append(directions, Direction.Tx)
	}
	if direction == int(netlink_polling.RX) || direction == int(netlink_polling.RXTX) {
		directions = append(directions, Direction.Rx)
	}
	return directions
}
func _add_tcam_entry(vrf_id uint32, direction int) (p4client.TableEntry, uint32) {
	tcam_prefix := fmt.Sprintf("%d%d", vrf_id, direction)
	var tblentry p4client.TableEntry
	var tcam, _ = strconv.Atoi(tcam_prefix)
	var tidx = trie_index_pool.get_used_id(EntryType.TRIE_I, []interface{}{tcam})
	if tidx == 0 {
		tidx = trie_index_pool.get_id(EntryType.TRIE_I, []interface{}{tcam})
		trie_index_pool.ref_count(EntryType.TRIE_I, []interface{}{tcam}, RefCountOp.RESET)
		tblentry = p4client.TableEntry{
			Tablename: TCAM_ENTRIES,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"user_meta.cmeta.tcam_prefix": {uint32(tcam), "ternary"},
					// "tcam_prefix_mask": {"0xFFFFFFFF","exact"},
					// "dst_ip_p_length"??????????????????????????????????
				},
				Priority: int32(tidx),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.ecmp_lpm_root_lut1_action", // "linux_networking_control.ecmp_lpm_root_lut1_action",
				Params:      []interface{}{tidx},
			},
		}
	} else {
		trie_index_pool.ref_count(EntryType.TRIE_I, []interface{}{tcam}, RefCountOp.INCREMENT)
	}
	return tblentry, tidx
}
func _get_tcam_prefix(vrf_id uint32, direction int) (int, error) {
	tcam_prefix := fmt.Sprintf("%d%d", vrf_id, direction)
	return strconv.Atoi(tcam_prefix)
}
func _delete_tcam_entry(vrf_id uint32, direction int) ([]interface{}, uint32) {
	tcam_prefix := fmt.Sprintf("%d%d", vrf_id, direction)
	var tblentry []interface{}
	var tcam, _ = strconv.Atoi(tcam_prefix)
	var tidx = trie_index_pool.get_used_id(EntryType.TRIE_I, []interface{}{tcam})
	var ref_count uint32
	if tidx != 0 {
		ref_count = trie_index_pool.ref_count(EntryType.TRIE_I, []interface{}{tcam}, RefCountOp.DECREMENT)
		if ref_count == 0 {
			trie_index_pool.put_id(EntryType.TRIE_I, []interface{}{tcam}, tidx)
			tblentry = append(tblentry, p4client.TableEntry{
				Tablename: TCAM_ENTRIES,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"user_meta.cmeta.tcam_prefix": {uint32(tcam), "ternary"},
						// "tcam_prefix_mask": {"0xFFFFFFFF","exact"},
						// "dst_ip_p_length"??????????????????????????????????
					},
					Priority: int32(1),
				},
			})
		}
	}
	return tblentry, tidx
}

type PhyPort struct {
	id  int
	vsi int
	mac string
}

func (p PhyPort) PhyPort_Init(id int, vsi string, mac string) PhyPort {
	p.id = id
	p.vsi, _ = strconv.Atoi(vsi)
	p.mac = mac

	return p
}

type GrpcPairPort struct {
	vsi  int
	mac  string
	peer map[string]string
}

func (g GrpcPairPort) GrpcPairPort_Init(vsi string, mac string) GrpcPairPort {
	g.vsi, _ = strconv.Atoi(vsi)
	g.mac = mac
	return g
}

func (g GrpcPairPort) set_remote_peer(peer [2]string) GrpcPairPort {
	g.peer = make(map[string]string)
	g.peer["vsi"] = peer[0]
	g.peer["mac"] = peer[1]
	return g
}

type L3Decoder struct {
	_mux_vsi     uint16
	_default_vsi int
	_phy_ports   []PhyPort
	_grpc_ports  []GrpcPairPort
	PhyPort
	GrpcPairPort
}

func (l L3Decoder) L3DecoderInit(representors map[string][2]string) L3Decoder {
	s := L3Decoder{
		_mux_vsi:     l.set_mux_vsi(representors),
		_default_vsi: 0x6,
		_phy_ports:   l._get_phy_info(representors),
		_grpc_ports:  l._get_grpc_info(representors),
	}
	return s
}
func (l L3Decoder) set_mux_vsi(representors map[string][2]string) uint16 {
	var a string = representors["vrf_mux"][0]
	var mux_vsi, _ = strconv.Atoi(a)
	return uint16(mux_vsi)
}
func (l L3Decoder) _get_phy_info(representors map[string][2]string) []PhyPort {
	var enabled_ports []PhyPort
	var vsi string
	var mac string
	var p = reflect.TypeOf(PortId)
	for i := 0; i < p.NumField(); i++ {
		var k = p.Field(i).Name
		var key = strings.ToLower(k) + "_rep"
		for k = range representors {
			if key == k {
				vsi = representors[key][0]
				mac = representors[key][1]
				enabled_ports = append(enabled_ports, l.PhyPort_Init(i, vsi, mac))
			}
		}
	}
	return enabled_ports // should return tuple
}

/*func (l L3Decoder) _set_phy_info(representors map[string]string){
	l._phy_ports = l._get_phy_info(representors map[string]string)
}*/

func (l L3Decoder) _get_grpc_info(representors map[string][2]string) []GrpcPairPort {
	var acc_host GrpcPairPort
	var host_port GrpcPairPort
	var grpc_ports []GrpcPairPort

	var acc_vsi string = representors["grpc_acc"][0]
	var acc_mac string = representors["grpc_acc"][1]
	acc_host = acc_host.GrpcPairPort_Init(acc_vsi, acc_mac) // ??

	var host_vsi string = representors["grpc_host"][0]
	var host_mac string = representors["grpc_host"][1]
	host_port = host_port.GrpcPairPort_Init(host_vsi, host_mac) // ??

	var acc_peer [2]string = representors["grpc_host"]
	var host_peer [2]string = representors["grpc_acc"]
	acc_host = acc_host.set_remote_peer(acc_peer)

	host_port = host_port.set_remote_peer(host_peer)

	grpc_ports = append(grpc_ports, acc_host, host_port)
	return grpc_ports
}
func (l L3Decoder) get_vrf_id(route netlink_polling.RouteStruct) uint32 {
	if route.Vrf.Spec.Vni == nil {
		return 0
	} else {
		return *route.Vrf.Spec.Vni
	}
}
func (l L3Decoder) _l3_host_route(route netlink_polling.RouteStruct, Delete string) []interface{} {
	/*var vrf_id = _big_endian_16(l.get_vrf_id(route.vrf))
	  if reflect.TypeOf(vrf_id) == "int"{
	          vrf_id = uint16(vrf_id[0])
	  }*/
	var vrf_id = l.get_vrf_id(route)
	var directions = _directions_of(route)
	// var host = _ip_addr(route.dst) //?? TODO
	// var host = route.Route0.Dst.IP.String() //?? TODO
	var host = route.Route0.Dst
	var entries []interface{}
	if Delete == "TRUE" {
		for _, dir := range directions {
			entries = append(entries, p4client.TableEntry{
				Tablename: L3_RT_HOST,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"vrf":       {_big_endian_16(vrf_id), "exact"},
						"direction": {uint16(dir), "exact"},
						"dst_ip":    {host, "exact"},
					},
					Priority: int32(0),
				},
			})
		}
	} else {
		for _, dir := range directions {
			entries = append(entries, p4client.TableEntry{
				Tablename: L3_RT_HOST,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"vrf":       {bigEndian16(vrf_id), "exact"},
						"direction": {uint16(dir), "exact"},
						// "dst_ip": {net.ParseIP(host),"exact"},
						"dst_ip": {host, "exact"},
					},
					Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.set_neighbor",
					Params:      []interface{}{uint16(route.Metadata["nh_ids"].(int))},
				},
			})
		}
	}
	return entries
}
func (l L3Decoder) _l3_route(route netlink_polling.RouteStruct, Delete string) []interface{} {
	var vrf_id = l.get_vrf_id(route)
	var directions = _directions_of(route)
	// addr = _ip_addr(route.dst)//TODO
	// prefix = _ip_prefixlen(route.dst)//TODO
	var addr = route.Route0.Dst.IP.String()
	var entries []interface{}

	for _, dir := range directions {
		if Delete == "TRUE" {
			var tbl_entry, t_idx = _delete_tcam_entry(vrf_id, dir)
			if !reflect.ValueOf(tbl_entry).IsZero() {
				entries = append(entries, tbl_entry)
			}
			entries = append(entries, p4client.TableEntry{
				Tablename: L3_RT,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"ipv4_table_lpm_root1": {t_idx, "ternary"},
						"dst_ip":               {net.ParseIP(addr), "lpm"},
						// "dst_ip_p_length"??????????????????????????????????
					},
					Priority: int32(1),
				},
			})
		} else {
			var tbl_entry, t_idx = _add_tcam_entry(vrf_id, dir)
			if !reflect.ValueOf(tbl_entry).IsZero() {
				entries = append(entries, tbl_entry)
			}
			entries = append(entries, p4client.TableEntry{
				Tablename: L3_RT,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"ipv4_table_lpm_root1": {t_idx, "ternary"},
						"dst_ip":               {net.ParseIP(addr), "lpm"},
						// "dst_ip_p_length"??????????????????????????????????
					},
					Priority: int32(1),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.set_neighbor",
					Params:      []interface{}{uint16(route.Metadata["nh_ids"].(int))},
				},
			})
		}
	}
	return entries
}
func (l L3Decoder) translate_added_route(route netlink_polling.RouteStruct) []interface{} {
	var ipv4Net = route.Route0.Dst
	if net.IP(ipv4Net.Mask).String() == "255.255.255.255" {
		return l._l3_host_route(route, "False")
	} else {
		return l._l3_route(route, "False")
	}
}
func (l L3Decoder) translate_changed_route(route netlink_polling.RouteStruct) []interface{} {
	return l.translate_added_route(route)
}
func (l L3Decoder) translate_deleted_route(route netlink_polling.RouteStruct) []interface{} {
	var ipv4Net = route.Route0.Dst
	if net.IP(ipv4Net.Mask).String() == "255.255.255.255" {
		return l._l3_host_route(route, "True")
	} else {
		return l._l3_route(route, "True")
	}
}
func (l L3Decoder) translate_added_nexthop(nexthop netlink_polling.NexthopStruct) []interface{} {
	if nexthop.NhType == netlink_polling.VXLAN {
		var entries []interface{}
		return entries
	}
	var key []interface{}
	key = append(key, nexthop.Key.VrfName, nexthop.Key.Dst, nexthop.Key.Dev, nexthop.Key.Local)
	var mod_ptr = ptr_pool.get_id(EntryType.L3_NH, key)
	var nh_id interface{}

	nh_id = uint16(nexthop.ID)
	var entries []interface{}

	if nexthop.NhType == netlink_polling.PHY {
		var smac, _ = net.ParseMAC(nexthop.Metadata["smac"].(string))
		var dmac, _ = net.ParseMAC(nexthop.Metadata["dmac"].(string))
		var port_id = nexthop.Metadata["egress_vport"] // port_id = int(PortId(nexthop.Metadata['egress_vport'])) TODO

		entries = append(entries, p4client.TableEntry{
			Tablename: MAC_MOD,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.update_smac_dmac",
				Params:      []interface{}{smac, dmac},
			},
		},
			p4client.TableEntry{
				Tablename: L3_NH,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"neighbor":    {nh_id.(uint16), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.push_mac",
					Params:      []interface{}{uint32(mod_ptr), uint16(port_id.(int))},
				},
			})
	} else if nexthop.NhType == netlink_polling.ACC {
		var dmac, _ = net.ParseMAC(nexthop.Metadata["dmac"].(string))
		var vlan_id = nexthop.Metadata["vlan_id"].(uint32)
		var vport = _to_egress_vsi(nexthop.Metadata["egress_vport"].(int))
		entries = append(entries, p4client.TableEntry{
			Tablename: PUSH_DMAC_VLAN,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.dmac_vlan_push",
				Params:      []interface{}{uint16(0), uint16(1), uint16(vlan_id), dmac},
			},
		},
			p4client.TableEntry{
				Tablename: L3_NH,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"neighbor":    {nh_id.(uint16), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.push_dmac_vlan",
					Params:      []interface{}{uint32(mod_ptr), uint32(vport)},
				},
			})
	} else if nexthop.NhType == netlink_polling.SVI {
		var smac, _ = net.ParseMAC(nexthop.Metadata["smac"].(string))
		var dmac, _ = net.ParseMAC(nexthop.Metadata["dmac"].(string))
		var vlan_id = nexthop.Metadata["vlan_id"]
		var vport = _to_egress_vsi(nexthop.Metadata["egress_vport"].(int))
		var Type = nexthop.Metadata["port_type"]

		if Type == ipu_db.TRUNK {
			entries = append(entries, p4client.TableEntry{
				Tablename: PUSH_MAC_VLAN,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
					},
					Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.update_smac_dmac_vlan",
					Params:      []interface{}{smac, dmac, 0, 1, vlan_id.(uint16)},
				},
			},
				p4client.TableEntry{
					Tablename: L3_NH,
					TableField: p4client.TableField{
						FieldValue: map[string][2]interface{}{
							"neighbor":    {nh_id.(uint16), "exact"},
							"bit32_zeros": {uint32(0), "exact"},
						},
						Priority: int32(0),
					},
					Action: p4client.Action{
						Action_name: "linux_networking_control.push_mac_vlan",
						Params:      []interface{}{uint32(mod_ptr), uint32(vport)},
					},
				})
		} else if Type == ipu_db.ACCESS {
			entries = append(entries, p4client.TableEntry{
				Tablename: MAC_MOD,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
					},
					Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.update_smac_dmac",
					Params:      []interface{}{smac, dmac},
				},
			},
				p4client.TableEntry{
					Tablename: L3_NH,
					TableField: p4client.TableField{
						FieldValue: map[string][2]interface{}{
							"neighbor":    {nh_id.(uint16), "exact"},
							"bit32_zeros": {uint32(0), "exact"},
						},
						Priority: int32(0),
					},
					Action: p4client.Action{
						Action_name: "linux_networking_control.push_mac",
						Params:      []interface{}{uint32(mod_ptr), uint32(vport)},
					},
				})
		} else {
			return entries
		}
	} else {
		return entries
	}

	return entries
}
func (l L3Decoder) translate_changed_nexthop(nexthop netlink_polling.NexthopStruct) []interface{} {
	return l.translate_added_nexthop(nexthop)
}
func (l L3Decoder) translate_deleted_nexthop(nexthop netlink_polling.NexthopStruct) []interface{} {
	if nexthop.NhType == netlink_polling.VXLAN {
		var entries []interface{}
		return entries
	}
	var key []interface{}
	key = append(key, nexthop.Key.VrfName, nexthop.Key.Dst, nexthop.Key.Dev, nexthop.Key.Local)
	var mod_ptr = ptr_pool.get_id(EntryType.L3_NH, key)
	var nh_id interface{}
	nh_id = uint16(nexthop.ID)
	var entries []interface{}

	if nexthop.NhType == netlink_polling.PHY {
		// var smac, _ = net.ParseMAC(nexthop.Metadata["smac"].(string))
		// var dmac, _ = net.ParseMAC(nexthop.Metadata["dmac"].(string))
		// var port_id = nexthop.Metadata["egress_vport"]

		entries = append(entries, p4client.TableEntry{
			Tablename: MAC_MOD,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
				},
				Priority: int32(0),
			},
		},
			p4client.TableEntry{
				Tablename: L3_NH,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"neighbor":    {nh_id.(uint16), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
			})
	} else if nexthop.NhType == netlink_polling.ACC {
		// var dmac, _ = net.ParseMAC(nexthop.Metadata["dmac"].(string))
		// var vlan_id = nexthop.Metadata["vlan_id"]
		// var vport = _to_egress_vsi(nexthop.Metadata["egress_vport"].(int))

		entries = append(entries, p4client.TableEntry{
			Tablename: PUSH_DMAC_VLAN,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
				},
				Priority: int32(0),
			},
		},
			p4client.TableEntry{
				Tablename: L3_NH,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"neighbor":    {nh_id.(uint16), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
			})
	} else if nexthop.NhType == netlink_polling.SVI {
		// var smac, _ = net.ParseMAC(nexthop.Metadata["smac"].(string))
		// var dmac, _ = net.ParseMAC(nexthop.Metadata["dmac"].(string))
		// var vlan_id = nexthop.Metadata["vlan_id"]
		// var vport = _to_egress_vsi(nexthop.Metadata["egress_vport"].(int))
		var Type = nexthop.Metadata["port_type"]

		if Type == ipu_db.TRUNK {
			entries = append(entries, p4client.TableEntry{
				Tablename: PUSH_MAC_VLAN,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
					},
					Priority: int32(0),
				},
			},
				p4client.TableEntry{
					Tablename: L3_NH,
					TableField: p4client.TableField{
						FieldValue: map[string][2]interface{}{
							"neighbor":    {nh_id.(uint16), "exact"},
							"bit32_zeros": {uint32(0), "exact"},
						},
						Priority: int32(0),
					},
				})
		} else if Type == ipu_db.ACCESS {
			entries = append(entries, p4client.TableEntry{
				Tablename: MAC_MOD,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
					},
					Priority: int32(0),
				},
			},
				p4client.TableEntry{
					Tablename: L3_NH,
					TableField: p4client.TableField{
						FieldValue: map[string][2]interface{}{
							"neighbor":    {nh_id.(uint16), "exact"},
							"bit32_zeros": {uint32(0), "exact"},
						},
						Priority: int32(0),
					},
				})
		} else {
			return entries
		}
	} else {
		return entries
	}
	ptr_pool.put_id(EntryType.L3_NH, key, mod_ptr)
	return entries
}
func (l L3Decoder) Static_additions() []interface{} {
	// var dummy = _to_egress_vsi(l._default_vsi)
	var tcam_prefix = TcamPrefix.GRD
	var entries []interface{}

	entries = append(entries, p4client.TableEntry{
		Tablename: POD_IN_IP_TRUNK,
		TableField: p4client.TableField{
			FieldValue: map[string][2]interface{}{
				"vsi": {l._mux_vsi, "exact"},
				"vid": {Vlan.GRD, "exact"},
			},
			Priority: int32(0),
		},
		Action: p4client.Action{
			Action_name: "linux_networking_control.pop_vlan_set_vrfid",
			Params:      []interface{}{ModPointer.IGNORE_PTR, uint32(0), tcam_prefix, uint32(0)},
		},
	},
	/*p4client.TableEntry{
	  Tablename: L3_RT,
	  TableField: p4client.TableField{
	  FieldValue: map[string][2]interface{}{
	  "ipv4_table_lpm_root1": {uint32(5), "ternary"},
	  "dst_ip": {net.ParseIPNet("169.254.155.169/0"),"lpm"},
	                          //"dst_ip_p_length"??????????????????????????????????
	   },
	   Priority: int32(1),
	   },
	   Action: p4client.Action{
	   Action_name : "linux_networking_control.set_neighbor",
	   Params : []interface{}{ uint16(10)},
	   },
	   },*/
	)
	for _, port := range l._grpc_ports {
		var peer_vsi, _ = strconv.Atoi(port.peer["vsi"])
		// var port_vsi, _ = strconv.Atoi(port.vsi)
		var peer_da, _ = net.ParseMAC(port.peer["mac"])
		var port_da, _ = net.ParseMAC(port.mac)
		entries = append(entries, p4client.TableEntry{
			Tablename: POD_IN_SVI_ACCESS,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"vsi": {uint16(port.vsi), "exact"},
					"da":  {peer_da, "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.fwd_to_port",
				Params:      []interface{}{uint32(_to_egress_vsi(peer_vsi))},
			},
		},
			p4client.TableEntry{
				Tablename: L2_FWD_LOOP,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"da": {port_da, "exact"},
					},
					Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.l2_fwd",
					Params:      []interface{}{uint32(_to_egress_vsi(port.vsi))},
				},
			})
	}
	for _, port := range l._phy_ports {
		entries = append(entries, p4client.TableEntry{
			Tablename: PHY_IN_IP,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"port_id":     {uint16(port.id), "exact"},
					"bit32_zeros": {uint32(0), "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.set_vrf_id",
				Params:      []interface{}{tcam_prefix, uint32(_to_egress_vsi(l._default_vsi)), uint32(0)},
			},
		},
			p4client.TableEntry{
				Tablename: PHY_IN_ARP,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"port_id":     {uint16(port.id), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.fwd_to_port",
					Params:      []interface{}{uint32(_to_egress_vsi(port.vsi))},
				},
			},
			p4client.TableEntry{
				Tablename: POD_IN_IP_ACCESS,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"vsi":         {uint16(port.vsi), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.fwd_to_port",
					Params:      []interface{}{uint32(port.id)},
				},
			},
			p4client.TableEntry{
				Tablename: POD_IN_ARP_ACCESS,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"vsi":         {uint16(port.vsi), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.fwd_to_port",
					Params:      []interface{}{uint32(port.id)},
				},
			})
	}

	return entries
}

func (l L3Decoder) Static_deletions() []interface{} {
	var entries []interface{}
	for _, port := range l._phy_ports {
		entries = append(entries, p4client.TableEntry{
			Tablename: PHY_IN_IP,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"port_id":     {uint16(port.id), "exact"},
					"bit32_zeros": {uint32(0), "exact"},
				},
				Priority: int32(0),
			},
		},
			p4client.TableEntry{
				Tablename: PHY_IN_ARP,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"port_id":     {uint16(port.id), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
			},
			p4client.TableEntry{
				Tablename: POD_IN_IP_ACCESS,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"vsi":         {uint16(port.vsi), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
			},
			p4client.TableEntry{
				Tablename: POD_IN_ARP_ACCESS,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"vsi":         {uint16(port.vsi), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
			})
	}
	for _, port := range l._grpc_ports {
		var peer_da, _ = net.ParseMAC(port.peer["mac"])
		var port_da, _ = net.ParseMAC(port.mac)
		entries = append(entries, p4client.TableEntry{
			Tablename: POD_IN_SVI_ACCESS,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"vsi": {uint16(port.vsi), "exact"},
					"da":  {peer_da, "exact"},
				},
				Priority: int32(0),
			},
		},
			p4client.TableEntry{
				Tablename: L2_FWD_LOOP,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"da": {port_da, "exact"},
					},
					Priority: int32(0),
				},
			})
	}
	entries = append(entries, p4client.TableEntry{
		Tablename: POD_IN_IP_TRUNK,
		TableField: p4client.TableField{
			FieldValue: map[string][2]interface{}{
				"vsi": {l._mux_vsi, "exact"},
				"vid": {Vlan.GRD, "exact"},
			},
			Priority: int32(0),
		},
	})
	return entries
}

type VxlanDecoder struct {
	VXLAN_UDP_PORT uint32
	_mux_vsi       int
	_default_vsi   int
}

func (v VxlanDecoder) VxlanDecoderInit(representors map[string][2]string) VxlanDecoder {
	var mux_vsi, _ = strconv.Atoi(representors["vrf_mux"][0])
	s := VxlanDecoder{
		VXLAN_UDP_PORT: 4789,
		_default_vsi:   0xb,
		_mux_vsi:       mux_vsi,
	}
	return s
}

/*@staticmethod //todo
  def _is_l2vpn_enabled(lb: LogicalBridge):
      return lb.vni is not None

*/
/*func(v VxlanDecoder) _is_l2vpn_enabled(ln LogicalBridge) interface{}{
	return lb.vni
}*/
func (v VxlanDecoder) translate_added_vrf(VRF *infradb.Vrf) []interface{} {
	var entries []interface{}
	/*if _is_l3vpn_enabled(VRF) != 0{
	                return entries
		}*/
	var tcam_prefix, _ = _get_tcam_prefix(*VRF.Spec.Vni, Direction.Rx)
	G, _ := infradb.GetVrf(VRF.Name)
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
	if reflect.ValueOf(Rmac).IsZero() {
		log.Println("Rmac not found for Vtep :", VRF.Spec.VtepIP.IP)
		return entries
	}
	entries = append(entries, p4client.TableEntry{
		Tablename: PHY_IN_VXLAN,
		TableField: p4client.TableField{
			FieldValue: map[string][2]interface{}{
				"dst_ip": {VRF.Spec.VtepIP.IP, "exact"},
				"vni":    {uint32(*VRF.Spec.Vni), "exact"},
				"da":     {Rmac, "exact"},
			},
			Priority: int32(0),
		},
		Action: p4client.Action{
			Action_name: "linux_networking_control.pop_vxlan_set_vrf_id",
			Params:      []interface{}{ModPointer.IGNORE_PTR, uint32(tcam_prefix), uint32(_to_egress_vsi(v._default_vsi)), *VRF.Spec.Vni},
		},
	})
	return entries
}

func (v VxlanDecoder) translate_deleted_vrf(VRF *infradb.Vrf) []interface{} {
	var entries []interface{}
	/*if _is_l3vpn_enabled(VRF) != 0{
	                return entries
		}*/
	G, _ := infradb.GetVrf(VRF.Name)
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
	if reflect.ValueOf(Rmac).IsZero() {
		log.Println("Rmac not found for Vtep :", VRF.Spec.VtepIP.IP)
		return entries
	}
	entries = append(entries, p4client.TableEntry{
		Tablename: PHY_IN_VXLAN,
		TableField: p4client.TableField{
			FieldValue: map[string][2]interface{}{
				"dst_ip": {VRF.Spec.VtepIP.IP, "exact"},
				"vni":    {uint32(*VRF.Spec.Vni), "exact"},
				"da":     {Rmac, "exact"},
			},
			Priority: int32(0),
		},
	})
	return entries
}

// L2 Ingress
/*func (v VxlanDecoder) translate_added_lb(lb LogicalBridge) []interface{}{
	var entries []interface{}
	if !(V._is_l2vpn_enabled(lb)){
                return entries
        }

	entries = append(entries, p4client.TableEntry{
                        Tablename: PHY_IN_VXLAN_L2,
                        TableField: p4client.TableField{
                                FieldValue: map[string][2]interface{}{
                                        "dst_ip":{uint32(lb.vtep),"exact"},
                                        "vni":{uint32(lb.vni),"exact"},
                                },
                                Priority: int32(0),
                        },
                        Action: p4client.Action{
                                Action_name : "linux_networking_control.pop_vxlan_set_vlan_id",
                                Params: []interface{}{ModPointer.IGNORE_PTR, uint16(lb.vlan_id), uint32(_to_egress_vsi(v._default_vsi))},
                        },
                })
        return entries
}
func (v VxlanDecoder) translate_deleted_lb(lb LogicalBridge) []interface{}{
	var entries []interface{}
	if !(V._is_l2vpn_enabled(lb)){
                return entries
        }
  	entries = append(entries, p4client.TableEntry{
			Tablename: PHY_IN_VXLAN_L2,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
						"dst_ip":{uint32(lb.vtep),"exact"},
						"vni":{uint32(lb.vni),"exact"},
				},
				Priority: int32(0),
			},
		})
	return entries
}*/
// L3 egress
func (v VxlanDecoder) translate_added_nexthop(nexthop netlink_polling.NexthopStruct) []interface{} {
	var entries []interface{}
	if nexthop.NhType != netlink_polling.VXLAN {
		return entries
	}
	var key []interface{}
	key = append(key, nexthop.Key.VrfName, nexthop.Key.Dev, nexthop.Key.Dst, nexthop.Key.Dev, nexthop.Key.Local)

	var mod_ptr = ptr_pool.get_id(EntryType.L3_NH, key)
	var nh_id interface{}
	nh_id = uint16(nexthop.ID)
	var vport = nexthop.Metadata["egress_vport"].(int)
	var smac, _ = net.ParseMAC(nexthop.Metadata["phy_smac"].(string))
	var dmac, _ = net.ParseMAC(nexthop.Metadata["phy_dmac"].(string))
	var src_addr = nexthop.Metadata["local_vtep_ip"]
	var dst_addr = nexthop.Metadata["remote_vtep_ip"]
	var vni = nexthop.Metadata["vni"]
	var inner_smac_addr, _ = net.ParseMAC(nexthop.Metadata["inner_smac"].(string))
	var inner_dmac_addr, _ = net.ParseMAC(nexthop.Metadata["inner_dmac"].(string))
	entries = append(entries, p4client.TableEntry{
		Tablename: PUSH_VXLAN_HDR,
		TableField: p4client.TableField{
			FieldValue: map[string][2]interface{}{
				"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
			},
			Priority: int32(0),
		},
		Action: p4client.Action{
			Action_name: "linux_networking_control.omac_vxlan_imac_push",
			Params:      []interface{}{smac, dmac, net.IP(src_addr.(string)), net.IP(dst_addr.(string)), v.VXLAN_UDP_PORT, vni.(uint32), inner_smac_addr, inner_dmac_addr},
		},
	},
		p4client.TableEntry{
			Tablename: L3_NH,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"neighbor":    {nh_id.(uint16), "exact"},
					"bit32_zeros": {uint32(0), "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.push_outermac_vxlan_innermac",
				Params:      []interface{}{uint32(mod_ptr), uint32(vport)},
			},
		})

	return entries
}
func (v VxlanDecoder) translate_changed_nexthop(nexthop netlink_polling.NexthopStruct) []interface{} {
	return v.translate_added_nexthop(nexthop)
}

func (v VxlanDecoder) translate_deleted_nexthop(nexthop netlink_polling.NexthopStruct) []interface{} {
	var entries []interface{}
	if nexthop.NhType != netlink_polling.VXLAN {
		return entries
	}
	var key []interface{}
	key = append(key, nexthop.Key.VrfName, nexthop.Key.Dev, nexthop.Key.Dst, nexthop.Key.Dev, nexthop.Key.Local)
	var mod_ptr = ptr_pool.get_id(EntryType.L3_NH, key)
	// var nh_id = _big_endian_16(nexthop.ID)
	var nh_id interface{}
	nh_id = uint16(nexthop.ID)
	entries = append(entries, p4client.TableEntry{
		Tablename: PUSH_VXLAN_HDR,
		TableField: p4client.TableField{
			FieldValue: map[string][2]interface{}{
				"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
			},
			Priority: int32(0),
		},
	},
		p4client.TableEntry{
			Tablename: L3_NH,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"neighbor":    {nh_id.(uint16), "exact"},
					"bit32_zeros": {uint32(0), "exact"},
				},
				Priority: int32(0),
			},
		})
	ptr_pool.put_id(EntryType.L3_NH, key, mod_ptr)
	return entries
}

// L2 egress
func (v VxlanDecoder) translate_added_l2_nexthop(nexthop netlink_polling.L2NexthopStruct) []interface{} {
	var entries []interface{}
	if nexthop.Type != netlink_polling.VXLAN {
		return entries
	}
	var key []interface{}
	key = append(key, nexthop.Key.Dev, nexthop.Key.VlanID, nexthop.Key.Dst)

	var mod_ptr = ptr_pool.get_id(EntryType.L2_NH, key)
	var vport = nexthop.Metadata["egress_vport"].(int)
	var src_mac, _ = net.ParseMAC(nexthop.Metadata["phy_smac"].(string))
	var dst_mac, _ = net.ParseMAC(nexthop.Metadata["phy_dmac"].(string))
	var src_ip = nexthop.Metadata["local_vtep_ip"]
	var dst_ip = nexthop.Metadata["remote_vtep_ip"]
	var vni = nexthop.Metadata["vni"]
	var vsi_out = _to_egress_vsi(vport)
	var neighbor = nexthop.ID
	entries = append(entries, p4client.TableEntry{
		Tablename: PUSH_VXLAN_OUT_HDR,
		TableField: p4client.TableField{
			FieldValue: map[string][2]interface{}{
				"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
			},
			Priority: int32(0),
		},
		Action: p4client.Action{
			Action_name: "linux_networking_control.omac_vxlan_push",
			Params:      []interface{}{src_mac, dst_mac, net.IP(src_ip.(string)), net.ParseIP(dst_ip.(string)), v.VXLAN_UDP_PORT, vni.(uint32)},
		},
	},
		p4client.TableEntry{
			Tablename: L2_NH,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"neighbor":    {_big_endian_16(neighbor), "exact"},
					"bit32_zeros": {uint32(0), "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.push_outermac_vxlan",
				Params:      []interface{}{uint32(mod_ptr), uint32(vsi_out)},
			},
		})
	return entries
}
func (v VxlanDecoder) translate_changed_l2_nexthop(nexthop netlink_polling.L2NexthopStruct) []interface{} {
	return v.translate_added_l2_nexthop(nexthop)
}

func (v VxlanDecoder) translate_deleted_l2_nexthop(nexthop netlink_polling.L2NexthopStruct) []interface{} {
	var entries []interface{}
	if nexthop.Type != netlink_polling.VXLAN {
		return entries
	}
	var key []interface{}
	key = append(key, nexthop.Key.Dev, nexthop.Key.VlanID, nexthop.Key.Dst)

	var mod_ptr = ptr_pool.get_id(EntryType.L2_NH, key)
	var neighbor = nexthop.ID
	ptr_pool.put_id(EntryType.L2_NH, key, mod_ptr)
	entries = append(entries, p4client.TableEntry{
		Tablename: PUSH_VXLAN_OUT_HDR,
		TableField: p4client.TableField{
			FieldValue: map[string][2]interface{}{
				"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
			},
			Priority: int32(0),
		},
	},
		p4client.TableEntry{
			Tablename: L2_NH,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"neighbor":    {_big_endian_16(neighbor), "exact"},
					"bit32_zeros": {uint32(0), "exact"},
				},
				Priority: int32(0),
			},
		})
	return entries
}

// L2 egress

func (v VxlanDecoder) translate_added_fdb(fdb netlink_polling.FdbEntryStruct) []interface{} {
	var entries []interface{}
	if fdb.Type != netlink_polling.VXLAN {
		return entries
	}
	var mac, _ = net.ParseMAC(fdb.Mac)
	var directions = _directions_of(fdb)

	for _, dir := range directions {
		entries = append(entries, p4client.TableEntry{
			Tablename: L2_FWD,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"vlan_id":   {_big_endian_16(fdb.VlanID), "exact"},
					"da":        {mac, "exact"},
					"direction": {uint16(dir), "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.set_neighbor",
				Params:      []interface{}{fdb.Metadata["nh_id"].(uint16)},
			},
		})
	}
	return entries
}

func (v VxlanDecoder) translate_changed_fdb(fdb netlink_polling.FdbEntryStruct) []interface{} {
	return v.translate_added_fdb(fdb)
}
func (v VxlanDecoder) translate_deleted_fdb(fdb netlink_polling.FdbEntryStruct) []interface{} {
	var entries []interface{}
	if fdb.Type != netlink_polling.VXLAN {
		return entries
	}
	var mac, _ = net.ParseMAC(fdb.Mac)
	var directions = _directions_of(fdb)

	for _, dir := range directions {
		entries = append(entries, p4client.TableEntry{
			Tablename: L2_FWD,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"vlan_id":   {_big_endian_16(fdb.VlanID), "exact"},
					"da":        {mac, "exact"},
					"direction": {uint16(dir), "exact"},
				},
				Priority: int32(0),
			},
		})
	}
	return entries
}

type PodDecoder struct {
	port_mux_ids  [2]string
	_port_mux_vsi int
	_port_mux_mac string
	vrf_mux_ids   [2]string
	_vrf_mux_vsi  int
	_vrf_mux_mac  string
	FLOOD_MOD_PTR uint32
	FLOOD_NH_ID   uint16
}

func (p PodDecoder) PodDecoderInit(representors map[string][2]string) PodDecoder {
	// var port_mux [2]string =  representors["port_mux"]
	p.port_mux_ids = representors["port_mux"]
	p.vrf_mux_ids = representors["vrf_mux"]

	var port_mux_vsi, _ = strconv.Atoi(p.port_mux_ids[0])
	var vrf_mux_vsi, _ = strconv.Atoi(p.vrf_mux_ids[0])

	p._port_mux_vsi = port_mux_vsi
	p._port_mux_mac = p.port_mux_ids[1]
	p._vrf_mux_vsi = vrf_mux_vsi
	p._vrf_mux_mac = p.vrf_mux_ids[1]
	p.FLOOD_MOD_PTR = ModPointer.L2_FLOODING_PTR
	p.FLOOD_NH_ID = uint16(0)
	return p
}

/*func (p PodDecoder) translate_added_bp(bp BridgePort) []interface{}{
	var port_mux_vsi_out = _to_egress_vsi(p._port_mux_vsi);
        var vsi = bp.vport_id
        var vsi_out = _to_egress_vsi(vsi)
        var mod_ptr = ptr_pool.get_id(EntryType.BP, bp.vport_id)
        var ignore_ptr = ModPointer.IGNORE_PTR
	var mac, _ = net.ParseMAC(bp.mac_address)
        var entries []interface{}

	if bp.Type == bp.Type.TRUNK{
		var mod_ptr_d = ptr_pool.get_id(EntryType.BP, mac)
	    	entries = append(entries, p4client.TableEntry{
			//From MUX
                	Tablename: PORT_MUX_IN,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"vsi":{uint16(p._port_mux_vsi),"exact"},
					"vid": {uint16(vsi),"exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name : "linux_networking_control.pop_stag_vlan",
				Params: []interface{}{uint32(mod_ptr_d), uint32(vsi_out)},
			},
		},
		//From Rx-to-Tx-recirculate (pass 3) entry
		p4client.TableEntry{
			Tablename: POP_STAG,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"mod_blob_ptr": {uint32(mod_ptr_d),"exact"},
				},
                                Priority: int32(0),
                        },
			Action: p4client.Action{
				Action_name : "linux_networking_control.vlan_stag_pop",
				Params: []interface{}{mac},
			},
		},
		p4client.TableEntry{
			Tablename: L2_FWD_LOOP,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"da": {uint32(mac),"exact"},
				},
                                Priority: int32(0),
                        },
			Action: p4client.Action{
				Action_name : "linux_networking_control.l2_fwd",
				Params: []interface{}{uint32(vsi_out)},
			},
		},
		p4client.TableEntry{
			Tablename: POD_OUT_TRUNK,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"meta.common.mod_blob_ptr": {uint32(mod_ptr_d),"exact"},
				},
                                Priority: int32(0),
                        },
			Action: p4client.Action{
				Action_name : "linux_networking_control.vlan_push_trunk",
				Params: []interface{}{0, 0, uint32(vsi)},
			},
		},)
		for vid, _ := range bp.vlan_ids{
			var svi = InfraDB.get_LB(vid).svi
			tcam_prefix = _get_tcam_prefix(svi.vrf.vni, Direction.Tx)
			entries = append(entries, p4client.TableEntry{
                                //To MUX PORT
				Tablename: POD_IN_ARP_TRUNK,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"vsi": {uint16(vsi),"exact"},
						"vid": {uint16(vid),"exact"},
					},
				Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name : "linux_networking_control.send_to_port_mux_trunk",
					Params: []interface{}{uint32(mod_ptr), uint32(port_mux_vsi_out)},
				},
			},
                        //To L2 FWD
			p4client.TableEntry{
				Tablename: POD_IN_IP_TRUNK,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"vsi": {uint16(vsi),"exact"},
						"vid": {uint16(vid),"exact"},
                                	},
                                	Priority: int32(0),
                        	},
				Action: p4client.Action{
					Action_name : "linux_networking_control.set_vlan_and_pop_vlan",
					Params: []interface{}{ignore_ptr,uint16(vid), uint32(0)},
				},
			})

                        if svi{
                                //To VRF SVI
                                var svi_mac, _ = net.ParseMAC(svi.mac_address)
                                entries = append(entries, p4client.TableEntry{
                                //From MUX
                                        Tablename: POD_IN_SVI_TRUNK,
                                        TableField: p4client.TableField{
                                                FieldValue: map[string][2]interface{}{
                                                        "vsi":{uint16(p._port_mux_vsi),"exact"},
                                                        "vid": {uint16(vsi),"exact"},
                                                        "da": {uint32(svi_mac),"exact"},
                                                },
                                                Priority: int32(0),
                                        },
                                        Action: p4client.Action{
                                                Action_name : "linux_networking_control.pop_vlan_set_vrf_id",
                                                Params: []interface{}{ignore_ptr, uint32(tcam_prefix), uint32(0), uint16(svi.vrf.vni)},
                                        },
                                })
                        } else{
                                log.Println("logger TODO")
                                //logger.warn(f"no SVI for VLAN {vid} on BP {vsi}, skipping entry for SVI table")
                        }
                }
        } else if (bp.Type == bp.Type.ACCESS){
                        var vid = bp.tag
                        var svi = InfraDB.get_LB(vid).svi
                        var tcam_prefix, _ = _get_tcam_prefix(svi.vrf.vni, Direction.Tx)
                        var mod_ptr_d = ptr_pool.get_id(EntryType.BP, bp.mac_address)
                        var dst_mac_addr=net.ParseMAC(bp.mac_address)
                        entries = append(entries, p4client.TableEntry{
                                //From MUX
                                Tablename: PORT_MUX_IN,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "vsi":{uint16(p._port_mux_vsi),"exact"},
                                                "vid": {uint16(vsi),"exact"},
                                        },
                                        Priority: int32(0),
                                },
                                Action: p4client.Action{
                                        Action_name : "linux_networking_control.pop_ctag_stag_vlan",
                                        Params: []interface{}{uint32(mod_ptr_d), uint32(vsi_out)},
                                },
                        },
                        p4client.TableEntry{
                                Tablename: POP_CTAG_STAG,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "meta.common.mod_blob_ptr": {uint32(mod_ptr_d),"exact"},
                                        },
                                        Priority: int32(0),
                                },
                                Action: p4client.Action{
                                        Action_name : "linux_networking_control.vlan_ctag_stag_pop",
                                        Params: []interface{}{dst_mac_addr},
                                },
                        },
                        //From Rx-to-Tx-recirculate (pass 3) entry
                        p4client.TableEntry{
                                Tablename: L2_FWD_LOOP,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "da":{dst_mac_addr,"exact"},
                                        },
                                        Priority: int32(0),
                                },
                                Action: p4client.Action{
                                        Action_name : "linux_networking_control.l2_fwd",
                                        Params: []interface{}{uint32(vsi_out)},
                                },
                        },
                        // To MUX PORT
                        p4client.TableEntry{
                                Tablename: POD_OUT_ACCESS,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "meta.common.mod_blob_ptr": {uint32(mod_ptr_d),"exact"},
                                        },
                                        Priority: int32(0),
                                },
                                Action: p4client.Action{
                                        Action_name : "linux_networking_control.vlan_push_access",
                                        Params: []interface{}{uint16(0), uint16(0), uint16(vid), uint16(0), uint16(0), uint16(vsi)},
                                },
                        },
                        p4client.TableEntry{
                                Tablename: POD_IN_ARP_ACCESS,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "vsi": {uint16(vsi),"exact"},
                                                "bit32_zeros": {uint32(0),"exact"},
                                        },
                                        Priority: int32(0),
                                },
                                Action: p4client.Action{
                                        Action_name : "linux_networking_control.send_to_port_mux_access",
                                        Params: []interface{}{uint24(mod_ptr), uint32(port_mux_vsi_out)},
                                },
                        },
                        //To L2 FWD
                        p4client.TableEntry{
                                Tablename: POD_IN_IP_ACCESS,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "vsi": {uint16(vsi),"exact"},
                                                "bit32_zeros": {uint32(0),"exact"},
                                        },
                                        Priority: int32(0),
                                },
                                Action: p4client.Action{
                                        Action_name : "linux_networking_control.set_vlan",
                                        Params: []interface{}{uint16(vid), uint32(0)},
                                },
                        })
                        if svi{
                                var svi_mac, _ = net.ParseMAC(svi.mac_address)
                                entries = append(entries, p4client.TableEntry{
                                        //From MUX
                                        Tablename: POD_IN_SVI_ACCESS,
                                        TableField: p4client.TableField{
                                                FieldValue: map[string][2]interface{}{
                                                        "vsi":{uint16(vsi),"exact"},
                                                        "da": {svi_mac,"exact"},
                                                },
                                                Priority: int32(0),
                                        },
                                        Action: p4client.Action{
                                                Action_name : "linux_networking_control.set_vrf_id_tx",
                                                Params: []interface{}{uint32(tcam_prefix), uint32(vsi_out), uint16(svi.vrf.vni)},
                                        },
                                })
                        } else{
                                //logger.warn(f"no SVI for VLAN {vid} on BP {vsi}, skipping entry for SVI table")
                        }
                }
                return entries
	}

func (p PodDecoder) translate_deleted_bp(bp BridgePort) []interface{}{
	var vsi = bp.vport_id
        var mod_ptr = ptr_pool.get_id(EntryType.BP, bp.vport_id)
        var mac, _ = net.ParseMAC(bp.mac_address)
        var entries []interface{}

	if bp.Type == bp.Type.TRUNK{
		var mod_ptr_d = ptr_pool.get_id(EntryType.BP, mac)
	    	entries = append(entries, p4client.TableEntry{
			//From MUX
                	Tablename: PORT_MUX_IN,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"vsi":{uint16(p._port_mux_vsi),"exact"},
					"vid": {uint16(vsi),"exact"},
				},
				Priority: int32(0),
			},
		},
		//From Rx-to-Tx-recirculate (pass 3) entry
		p4client.TableEntry{
			Tablename: POP_STAG,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"mod_blob_ptr": {uint32(mod_ptr_d),"exact"},
				},
                                Priority: int32(0),
                        },
		},
		p4client.TableEntry{
			Tablename: L2_FWD_LOOP,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"da": {uint32(mac),"exact"},
				},
                                Priority: int32(0),
                        },
		},
		p4client.TableEntry{
			Tablename: POD_OUT_TRUNK,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"meta.common.mod_blob_ptr": {uint32(mod_ptr_d),"exact"},
				},
                                Priority: int32(0),
                        },
		})
		for vid, _ := range bp.vlan_ids{
			var svi = InfraDB.get_LB(vid).svi
			tcam_prefix = _get_tcam_prefix(svi.vrf.vni, Direction.Tx)
			entries = append(entries, p4client.TableEntry{
                                //To MUX PORT
				Tablename: POD_IN_ARP_TRUNK,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"vsi": {uint16(vsi),"exact"},
						"vid": {uint16(vid),"exact"},
					},
				Priority: int32(0),
				},
                        },
			//To L2 FWD
			p4client.TableEntry{
				Tablename: POD_IN_IP_TRUNK,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"vsi": {uint16(vsi),"exact"},
						"vid": {uint16(vid),"exact"},
                                	},
                                	Priority: int32(0),
                        	},
			})

                        if svi{
                                //To VRF SVI
                                var svi_mac, _ = net.ParseMAC(svi.mac_address)
                                entries = append(entries, p4client.TableEntry{
                                //From MUX
                                        Tablename: POD_IN_SVI_TRUNK,
                                        TableField: p4client.TableField{
                                                FieldValue: map[string][2]interface{}{
                                                        "vsi":{uint16(p._port_mux_vsi),"exact"},
                                                        "vid": {uint16(vsi),"exact"},
                                                        "da": {uint32(svi_mac),"exact"},
                                                },
                                                Priority: int32(0),
                                        },
                                })
                        } else{
                                //logger.warn(f"no SVI for VLAN {vid} on BP {vsi}, skipping entry for SVI table")
                        }
                }
        } else if ( bp.Type == bp.Type.ACCESS){
                        var vid = bp.tag
                        var svi = InfraDB.get_LB(vid).svi
                        var dst_mac_addr=net.ParseMAC(bp.mac_address)
                        entries = append(entries, p4client.TableEntry{
                                //From MUX
                                Tablename: PORT_MUX_IN,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "vsi":{uint16(p._port_mux_vsi),"exact"},
                                                "vid": {uint16(vsi),"exact"},
                                        },
                                        Priority: int32(0),
                                },
                        },
                        p4client.TableEntry{
                                Tablename: POP_CTAG_STAG,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "meta.common.mod_blob_ptr": {uint32(mod_ptr_d),"exact"},
                                        },
                                        Priority: int32(0),
                                },
                        },
                        //From Rx-to-Tx-recirculate (pass 3) entry
                        p4client.TableEntry{
                                Tablename: L2_FWD_LOOP,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "da":{dst_mac_addr,"exact"},
                                        },
                                        Priority: int32(0),
                                },
                        },
                        // To MUX PORT
                        p4client.TableEntry{
                                Tablename: POD_OUT_ACCESS,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "meta.common.mod_blob_ptr": {uint32(mod_ptr_d),"exact"},
                                        },
                                        Priority: int32(0),
                                },
                        },
                        p4client.TableEntry{
                                Tablename: POD_IN_ARP_ACCESS,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "vsi": {uint16(vsi),"exact"},
                                                "bit32_zeros": {uint32(0),"exact"},
                                        },
                                        Priority: int32(0),
                                },
                        },
                        //To L2 FWD
                        p4client.TableEntry{
                                Tablename: POD_IN_IP_ACCESS,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "vsi": {uint16(vsi),"exact"},
                                                "bit32_zeros": {uint32(0),"exact"},
                                        },
                                        Priority: int32(0),
                                },
                        })
                        if svi{
                                var svi_mac, _ = net.ParseMAC(svi.mac_address)
                                entries = append(entries, p4client.TableEntry{
                                        //From MUX
                                        Tablename: POD_IN_SVI_ACCESS,
                                        TableField: p4client.TableField{
                                                FieldValue: map[string][2]interface{}{
                                                        "vsi":{uint16(vsi),"exact"},
                                                        "da": {svi_mac,"exact"},
                                                },
                                                Priority: int32(0),
                                        },
                                })
                        } else{
                                //logger.warn(f"no SVI for VLAN {vid} on BP {vsi}, skipping entry for SVI table")
                        }
                }
                ptr_pool.put_id(EntryType.BP, bp.vport_id, mod_ptr)
                return entries
}

func(p PodDecoder) translate_added_svi(svi SwitchedVirtualInterface) []interface{}{
        var tcam_prefix = TcamPrefix.GRD
        var ignore_ptr = ModPointer.IGNORE_PTR
        mac = net.ParseMAC(svi.mac_address)
        var entries []interface{}

        for bp, _ := range svi.bridge.ports{
                if bp.Type == bp.Type.ACCESS{
                        entries = append(entries, p4client.TableEntry{
                                Tablename: POD_IN_SVI_ACCESS,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "vsi": {uint16(bp.vport_id,),"exact"},
                                                "da":{mac,"exact"},
                                        },
                                                Priority: int32(0),
                                        },
                                        Action: p4client.Action{
                                                Action_name : "linux_networking_control.set_vrf_id_tx",
                                                Params: []interface{}{uint32(tcam_prefix), uint32(0), uint16(svi.vrf.vni)},
                                        },
                                })
                } else if (bp.Type == bp.Type.TRUNK){
                        entries = append(entries, p4client.TableEntry{
                                Tablename: POD_IN_SVI_TRUNK,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "vsi": {uint16(bp.vport_id,),"exact"},
                                                "vid": {uint16(svi.vlan_id)},
                                                "da":{mac,"exact"},
                                        },
                                                Priority: int32(0),
                                        },
                                        Action: p4client.Action{
                                                Action_name : "linux_networking_control.pop_vlan_set_vrf_id",
                                                Params: []interface{}{ignore_ptr, uint32(tcam_prefix), uint32(0), uint16(svi.vni)},
                                        },
                                })
                }
        }

}

func(p PodDecoder) translate_deleted_svi(svi SwitchedVirtualInterface) []interface{}{
        var tcam_prefix = TcamPrefix.GRD
        var ignore_ptr = ModPointer.IGNORE_PTR
        mac = net.ParseMAC(svi.mac_address)
        var entries []interface{}

        for bp, _ := range svi.bridge.ports{
                if bp.Type == bp.Type.ACCESS{
                        entries = append(entries, p4client.TableEntry{
                                Tablename: POD_IN_SVI_ACCESS,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "vsi": {uint16(bp.vport_id,),"exact"},
                                                "da":{mac,"exact"},
                                        },
                                                Priority: int32(0),
                                        },
                                })
                } else if (bp.Type == bp.Type.TRUNK){
                        entries = append(entries, p4client.TableEntry{
                                Tablename: POD_IN_SVI_TRUNK,
                                TableField: p4client.TableField{
                                        FieldValue: map[string][2]interface{}{
                                                "vsi": {uint16(bp.vport_id,),"exact"},
                                                "vid": {uint16(svi.vlan_id)},
                                                "da":{mac,"exact"},
                                        },
                                                Priority: int32(0),
                                        },
                                })
                }
        }

}*/

func (p PodDecoder) translate_added_fdb(fdb netlink_polling.FdbEntryStruct) []interface{} {
	var entries []interface{}
	var fdb_mac, _ = net.ParseMAC(fdb.Mac)
	if fdb.Type != netlink_polling.BRIDGEPORT {
		return entries
	}
	for dir := range _directions_of(fdb) {
		entries = append(entries, p4client.TableEntry{
			Tablename: L2_FWD,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"vlan_id":   {_big_endian_16(fdb.VlanID), "exact"},
					"da":        {fdb_mac, "exact"},
					"direction": {uint16(dir), "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.set_neighbor",
				Params:      []interface{}{fdb.Metadata["nh_id"].(uint16)},
			},
		})
	}
	return entries
}

func (p PodDecoder) translate_changed_fdb(fdb netlink_polling.FdbEntryStruct) []interface{} {
	return p.translate_added_fdb(fdb)
}

func (p PodDecoder) translate_deleted_fdb(fdb netlink_polling.FdbEntryStruct) []interface{} {
	var entries []interface{}
	var fdb_mac, _ = net.ParseMAC(fdb.Mac)
	if fdb.Type != netlink_polling.BRIDGEPORT {
		return entries
	}
	for dir := range _directions_of(fdb) {
		entries = append(entries, p4client.TableEntry{
			Tablename: L2_FWD,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"vlan_id":   {_big_endian_16(fdb.VlanID), "exact"},
					"da":        {fdb_mac, "exact"},
					"direction": {uint16(dir), "exact"},
				},
				Priority: int32(0),
			},
		})
	}
	return entries
}

func (p PodDecoder) translate_added_l2_nexthop(nexthop netlink_polling.L2NexthopStruct) []interface{} {
	var entries []interface{}
	if nexthop.Type != netlink_polling.BRIDGEPORT {
		return entries
	}
	var neighbor = nexthop.ID
	var port_type = nexthop.Metadata["port_type"]
	var port_id = nexthop.Metadata["vport_id"]

	if port_type == ipu_db.ACCESS {
		entries = append(entries, p4client.TableEntry{
			Tablename: L2_NH,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"neighbor":    {_big_endian_16(neighbor), "exact"},
					"bit32_zeros": {uint32(0), "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.fwd_to_port",
				Params:      []interface{}{uint32(_to_egress_vsi(port_id.(int)))},
			},
		})
	} else if port_type == ipu_db.TRUNK {
		var key []interface{}
		key = append(key, nexthop.Key.Dev, nexthop.Key.VlanID, nexthop.Key.Dst)

		var mod_ptr = ptr_pool.get_id(EntryType.L2_NH, key)
		entries = append(entries, p4client.TableEntry{
			Tablename: PUSH_VLAN,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.vlan_push",
				Params:      []interface{}{uint16(0), uint16(0), uint16(nexthop.VlanID)},
			},
		},
			p4client.TableEntry{
				Tablename: L2_NH,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"neighbor":    {_big_endian_16(neighbor), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
				Action: p4client.Action{
					Action_name: "linux_networking_control.push_vlan",
					Params:      []interface{}{uint32(mod_ptr), uint32(_to_egress_vsi(port_id.(int)))},
				},
			})
	}
	return entries
}

func (p PodDecoder) translate_changed_l2_nexthop(nexthop netlink_polling.L2NexthopStruct) []interface{} {
	return p.translate_added_l2_nexthop(nexthop)
}

func (p PodDecoder) translate_deleted_l2_nexthop(nexthop netlink_polling.L2NexthopStruct) []interface{} {
	var entries []interface{}
	var mod_ptr uint32
	if nexthop.Type != netlink_polling.BRIDGEPORT {
		return entries
	}
	var neighbor = nexthop.ID
	var port_type = nexthop.Metadata["port_type"]

	if port_type == ipu_db.ACCESS {
		entries = append(entries, p4client.TableEntry{
			Tablename: L2_NH,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"neighbor":    {_big_endian_16(neighbor), "exact"},
					"bit32_zeros": {uint32(0), "exact"},
				},
				Priority: int32(0),
			},
		})
	} else if port_type == ipu_db.TRUNK {
		var key []interface{}
		key = append(key, nexthop.Key.Dev, nexthop.Key.VlanID, nexthop.Key.Dst)

		mod_ptr = ptr_pool.get_id(EntryType.L2_NH, key)
		entries = append(entries, p4client.TableEntry{
			Tablename: PUSH_VLAN,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"meta.common.mod_blob_ptr": {uint32(mod_ptr), "exact"},
				},
				Priority: int32(0),
			},
		},
			p4client.TableEntry{
				Tablename: L2_NH,
				TableField: p4client.TableField{
					FieldValue: map[string][2]interface{}{
						"neighbor":    {_big_endian_16(neighbor), "exact"},
						"bit32_zeros": {uint32(0), "exact"},
					},
					Priority: int32(0),
				},
			})
	}
	var key []interface{}
	key = append(key, nexthop.Key.Dev, nexthop.Key.VlanID, nexthop.Key.Dst)

	ptr_pool.put_id(EntryType.L2_NH, key, mod_ptr)
	return entries
}

func (p PodDecoder) Static_additions() []interface{} {
	var port_mux_da, _ = net.ParseMAC(p._port_mux_mac)
	var vrf_mux_da, _ = net.ParseMAC(p._vrf_mux_mac)
	var entries []interface{}
	entries = append(entries, p4client.TableEntry{
		Tablename: PORT_MUX_FWD,
		TableField: p4client.TableField{
			FieldValue: map[string][2]interface{}{
				"bit32_zeros": {uint32(0), "exact"},
			},
			Priority: int32(0),
		},
		Action: p4client.Action{
			Action_name: "linux_networking_control.send_to_port_mux",
			Params:      []interface{}{uint32(_to_egress_vsi(p._port_mux_vsi))},
		},
	},
		p4client.TableEntry{
			Tablename: PORT_MUX_IN,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"vsi": {uint16(p._port_mux_vsi), "exact"},
					"vid": {Vlan.PHY0, "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.set_def_vsi_loopback",
				Params:      []interface{}{uint32(0)},
			},
		},
		p4client.TableEntry{
			Tablename: L2_FWD_LOOP,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"da": {port_mux_da, "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.l2_fwd",
				Params:      []interface{}{uint32(_to_egress_vsi(p._port_mux_vsi))},
			},
		},
		p4client.TableEntry{
			Tablename: L2_FWD_LOOP,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"da": {vrf_mux_da, "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.l2_fwd",
				Params:      []interface{}{uint32(_to_egress_vsi(p._vrf_mux_vsi))},
			},
		},
		// NH entry for flooding
		p4client.TableEntry{
			Tablename: PUSH_QNQ_FLOOD,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"meta.common.mod_blob_ptr": {p.FLOOD_MOD_PTR, "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.vlan_push_stag_ctag_flood",
				Params:      []interface{}{uint32(0)},
			},
		},
		p4client.TableEntry{
			Tablename: L2_NH,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"neighbor":    {p.FLOOD_NH_ID, "exact"},
					"bit32_zeros": {uint32(0), "exact"},
				},
				Priority: int32(0),
			},
			Action: p4client.Action{
				Action_name: "linux_networking_control.push_stag_ctag",
				Params:      []interface{}{p.FLOOD_MOD_PTR, uint32(_to_egress_vsi(p._vrf_mux_vsi))},
			},
		})
	return entries
}

func (p PodDecoder) Static_deletions() []interface{} {
	var entries []interface{}
	var port_mux_da, _ = net.ParseMAC(p._port_mux_mac)
	var vrf_mux_da, _ = net.ParseMAC(p._vrf_mux_mac)
	entries = append(entries, p4client.TableEntry{
		Tablename: PORT_MUX_FWD,
		TableField: p4client.TableField{
			FieldValue: map[string][2]interface{}{
				"bit32_zeros": {uint32(0), "exact"},
			},
			Priority: int32(0),
		},
	},
		p4client.TableEntry{
			Tablename: PORT_MUX_IN,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"vsi": {uint16(p._port_mux_vsi), "exact"},
					"vid": {Vlan.PHY0, "exact"},
				},
				Priority: int32(0),
			},
		},
		p4client.TableEntry{
			Tablename: L2_FWD_LOOP,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"da": {port_mux_da, "exact"},
				},
				Priority: int32(0),
			},
		},
		p4client.TableEntry{
			Tablename: L2_FWD_LOOP,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"da": {vrf_mux_da, "exact"},
				},
				Priority: int32(0),
			},
		},
		// NH entry for flooding
		p4client.TableEntry{
			Tablename: PUSH_QNQ_FLOOD,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"meta.common.mod_blob_ptr": {p.FLOOD_MOD_PTR, "exact"},
				},
				Priority: int32(0),
			},
		},
		p4client.TableEntry{
			Tablename: L2_NH,
			TableField: p4client.TableField{
				FieldValue: map[string][2]interface{}{
					"neighbor":    {p.FLOOD_NH_ID, "exact"},
					"bit32_zeros": {uint32(0), "exact"},
				},
				Priority: int32(0),
			},
		})
	return entries
}
