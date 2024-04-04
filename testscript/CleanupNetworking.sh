#!/bin/bash


# Function to check if a service is running and start it if not
check_and_start_service() {
    local service_name="$1"

    # Check if the service is running
    if systemctl is-active --quiet "$service_name"; then
        echo "Service $service_name is already running."
    else
        # If the service is not running, start it
        echo "Service $service_name is not running. Starting it now..."
        systemctl start "$service_name"
        
        # Check if the service started successfully
        if systemctl is-active --quiet "$service_name"; then
            echo "Service $service_name started successfully."
        else
            echo "Failed to start service $service_name."
        fi
    fi
}

# Function to retrieve the list of network interfaces
get_interfaces() {
    ip -o link show | awk '{print $2}' | cut -d ':' -f 1
}

# Function to compare current interfaces with the previous list
compare_interfaces() {
    local current_interfaces=$(get_interfaces)

    # Check for new interfaces
    local new_interfaces=$(comm -23 <(printf '%s\n' "${previous_interfaces[@]}" | sort) <(printf '%s\n' "${current_interfaces[@]}" | sort))

    # Print the newly created interfaces
    if [ -n "$new_interfaces" ]; then
        echo "Deleted interfaces:"
        printf '%s\n' "${new_interfaces[@]}"
    else
        echo "No interfaces deleted since last check."
    fi

    # Store the current interfaces for the next check
    previous_interfaces=("${current_interfaces[@]}")
}


echo 'Running Delete Transparent Trunk command'
./godpu evpn delete-bp --name secseltrunk
sleep 1; compare_interfaces

echo 'Running Delete Selective Trunk command'
./godpu evpn delete-bp --name sectranstrunk
sleep 1; compare_interfaces


echo 'Running Delete vrf without vni command'
./godpu evpn delete-vrf --name yellow 
sleep 1; compare_interfaces

echo 'Running Delete logical bridge with vni'
./godpu evpn delete-lb --name br-60
sleep 1; compare_interfaces

echo 'Running delete secondary network interfaces'


./godpu evpn delete-svi --name blue-10-svi
 sleep 1; compare_interfaces
./godpu evpn delete-svi --name green-20-svi
./godpu evpn delete-svi --name green-21-svi
 sleep 1; compare_interfaces
./godpu evpn delete-svi --name green-22-svi
 sleep 1; compare_interfaces

./godpu evpn delete-vrf --name blue
 sleep 1; compare_interfaces
./godpu evpn delete-vrf --name green
 sleep 1; compare_interfaces

./godpu evpn delete-lb --name br-10
 sleep 1; compare_interfaces
./godpu evpn delete-lb --name br-20
 sleep 1; compare_interfaces
./godpu evpn delete-lb --name br-21
 sleep 1; compare_interfaces
./godpu evpn delete-lb --name br-22
 sleep 1; compare_interfaces

echo 'Running delete Primary network interfaces'


./godpu evpn delete-svi --name rred-br30-svi
 sleep 1; compare_interfaces
./godpu evpn delete-vrf --name rred
 sleep 1; compare_interfaces
./godpu evpn delete-bp --name primary
 sleep 1; compare_interfaces
./godpu evpn delete-lb --name br-30

