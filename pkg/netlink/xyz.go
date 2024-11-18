package netlink

/*func (nh *Nexthop) tryResolve1(neighbors map[string]*Neighbor) []*Nexthop {
	if nh.resolved {
		return []*Nexthop{nh}
	}

	if nh.dst != "" {
		neighborKey := nh.vrf.name + nh.dst + nh.dev
		neighbor := neighbors[neighborKey]
		nh.neighbor = neighbor
		if neighbor != nil {
			if nh.prefsrc == "" && neighbor.src != "" {
				nh.prefsrc = neighbor.src
			}
		} else if !nh.flags["onlink"] {
			return nil
		}
	} else {
		nh.neighbor = nil
	}

	if nh.nhType == VXLAN || nh.nhType == TUN {
		var dst string
		if nh.nhType == VXLAN {
			if nh.dst == "" || nh.neighbor == nil {
				return nil
			}
			nh.metadata["remote_vtep_ip"] = nh.dst
			nh.metadata["inner_dmac"] = nh.neighbor.lladdr
			dst = nh.dst
		} else if nh.nhType == TUN {
			T := tunReps[nh.dev]
			if T.SA {
				nh.metadata["tun_dev"] = nh.dev
				nh.metadata["spi"] = T.spi
				nh.metadata["sa_idx"] = T.saIdx
				nh.metadata["local_tep_ip"] = T.src
				dst = T.dst
				nh.metadata["remote_tep_ip"] = T.dst
			} else {
				return nil
			}
		}

		finalNhs := []*Nexthop{}
		GRD := infraDB.GetVRF("GRD")
		R := netlinkDB.LookupRoute(dst, GRD, true)
		if R != nil {
			for _, nh1 := range R.nexthops {
				if nh1.nhType == PHY || nh1.nhType == TUN {
					for _, nh2 := range nh1.tryResolve(neighbors) {
						final := *nh2
						final.resolved = true
						final.vrf = nh.vrf
						final.routeRefs = append([]string{}, nh.routeRefs...)
						final.flags = map[string]bool{}
						for k, v := range nh.flags {
							final.flags[k] = v
						}
						final.metadata = map[string]string{}
						for k, v := range nh.metadata {
							final.metadata[k] = v
						}
						if nh.nhType == VXLAN && nh2.nhType == TUN {
							final.nhType = VXLAN_TUN
							for k, v := range nh2.metadata {
								final.metadata[k] = v
							}
						} else {
							final.nhType = nh.nhType
						}
						final.key = nh.vrf.name + nh.dst + nh.dev + nh.metadata["local"] + string(final.nhType)
						finalNhs = append(finalNhs, &final)
					}
				}
			}
		}

		if len(finalNhs) > 0 {
			output := ""
			for _, nh := range finalNhs {
				output += "          " + nh.String() + "\n"
			}
			logger.Printf("Recursively resolved %v to\n%v", nh, output)
		}
		return finalNhs
	} else {
		if nh.neighbor != nil && nh.neighbor.src != "" {
			nh.resolved = true
			logger.Printf("Directly resolved %v", nh)
			return []*Nexthop{nh}
		} else {
			return nil
		}
	}
}*/
