#!/bin/bash


# Function to retrieve the list of network interfaces
get_interfaces() {
    ip -o link show | awk '{print $2}' | cut -d ':' -f 1
}

# Function to compare current interfaces with the previous list
compare_interfaces() {
    local current_interfaces=$(get_interfaces)

    # Check for new interfaces
    local new_interfaces=$(comm -13 <(printf '%s\n' "${previous_interfaces[@]}" | sort) <(printf '%s\n' "${current_interfaces[@]}" | sort))

    # Print the newly created interfaces
    if [ -n "$new_interfaces" ]; then
        echo "Newly created interfaces:"
        printf '%s\n' "${new_interfaces[@]}"
    else
        echo "No new interfaces created since last check."
    fi

    # Store the current interfaces for the next check
    previous_interfaces=("${current_interfaces[@]}")
}

# Initialize the previous interfaces variable
previous_interfaces=($(get_interfaces))


 sleep 1; compare_interfaces
echo 'Running Primary Networking command'

./godpu evpn create-lb --name br-30 --vlan-id 30 --addr localhost:50151
 sleep 1; compare_interfaces
./godpu evpn create-vrf --name rred --vni 3000 --loopback 10.120.1.1/32  --vtep 10.1.1.1/32 --addr localhost:50151
 sleep 1; compare_interfaces
./godpu evpn create-svi --name rred-br30-svi --vrf //network.opiproject.org/vrfs/rred --mac 00:30:30:30:30:30 --gw-ips 10.30.1.1/24 --logicalBridge //network.opiproject.org/bridges/br-30 --addr localhost:50151
 sleep 1; compare_interfaces
./godpu evpn create-bp --name primary --type access --mac 00:21:00:00:14:48 --logicalBridges //network.opiproject.org/bridges/br-30 --addr localhost:50151



echo 'Running Secondary Networking command'
./godpu evpn create-lb --name br-10 --vlan-id 10 --addr localhost:50151
 sleep 1; compare_interfaces
./godpu evpn create-lb --name br-20 --vlan-id 20 --addr localhost:50151 
 sleep 1; compare_interfaces
./godpu evpn create-lb --name br-21 --vlan-id 21 --addr localhost:50151 
 sleep 1; compare_interfaces
./godpu evpn create-lb --name br-22 --vlan-id 22 --addr localhost:50151
 sleep 1; compare_interfaces

./godpu evpn create-vrf --name blue --vni 1002 --loopback 10.100.1.1/32 --addr localhost:50151
 sleep 1; compare_interfaces
./godpu evpn create-vrf --name green --vni 2000 --loopback 10.110.1.1/32 --addr localhost:50151
 sleep 1; compare_interfaces

./godpu evpn create-svi --name blue-10-svi --vrf //network.opiproject.org/vrfs/blue  --logicalBridge //network.opiproject.org/bridges/br-10 --mac 00:10:10:10:10:10 --gw-ips 10.10.1.1/24 --ebgp --remote-as 65100 --addr localhost:50151
 sleep 1; compare_interfaces
./godpu evpn create-svi --name green-20-svi --vrf //network.opiproject.org/vrfs/green --logicalBridge //network.opiproject.org/bridges/br-20 --mac 00:20:20:20:20:20 --gw-ips 10.20.1.1/24 --addr localhost:50151 
 sleep 1; compare_interfaces
./godpu evpn create-svi --name green-21-svi --vrf //network.opiproject.org/vrfs/green --logicalBridge //network.opiproject.org/bridges/br-21 --mac 00:21:21:21:21:21 --gw-ips 10.21.1.1/24 --addr localhost:50151 
 sleep 1; compare_interfaces
./godpu evpn create-svi --name green-22-svi --vrf //network.opiproject.org/vrfs/green --logicalBridge //network.opiproject.org/bridges/br-22 --mac 00:22:22:22:22:22 --gw-ips 10.22.1.1/24 --addr localhost:50151
 sleep 1; compare_interfaces


echo 'Running vrf without vni command'
./godpu evpn create-vrf --name yellow --loopback 10.150.1.1/32 --addr localhost:50151
 sleep 1; compare_interfaces

echo 'Running logical bridge with vni'
./godpu evpn create-lb --name br-60 --vlan-id 60 --vni 6000 --vtep "10.1.1.1/32" --addr localhost:50151
 sleep 1; compare_interfaces


echo 'Running Selective Trunk'
./godpu evpn create-bp --name secseltrunk --type trunk --mac 00:25:00:00:15:28 --logicalBridges "//network.opiproject.org/bridges/br-20,//network.opiproject.org/bridges/br-21,//network.opiproject.org/bridges/br-22" --addr localhost:50151
sleep 1; compare_interfaces

echo 'Running Transparent Trunk'
./godpu evpn create-bp --name sectranstrunk --type trunk --mac 00:27:00:00:15:28 --addr localhost:50151

