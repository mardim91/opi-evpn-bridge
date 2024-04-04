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

# Function to check if a docker is running and start it if not
check_and_start_docker_container() {
    local container_name="$1"

    # Check if the Docker container is running
    if docker ps --format '{{.Names}}' | grep -q "^$container_name$"; then
        echo "Container $container_name is already running."
    else
        # If the container is not running, start it
        echo "Container $container_name is not running. Starting it now..."

	docker run --rm -d --network host --name my_jaeger eb4900ef8268
        # Check if the container started successfully
        if docker ps --format '{{.Names}}' | grep -q "^$container_name$"; then
            echo "Container $container_name started successfully."
        else
            echo "Failed to start container $container_name."
        fi
    fi
}
close_screen() {
# Check if there are any "my_session" screen sessions running
if screen -ls | grep -q "my_session"; then
    # Get list of all "my_session" sessions and their IDs
    session_list=$(screen -ls | grep "my_session" | awk '{print $1}')
    
    # Iterate over each session and send the quit command
    while IFS= read -r session; do
        screen -S "$session" -X quit
        echo "Screen session '$session' terminated."
    done <<< "$session_list"
else
    echo "No existing screen sessions named 'my_session' found."
fi
}

check_opi_evpn_running() {
# Check if the process is running
if pgrep "opi-evpn-bridge" > /dev/null; then
    # If the process is running, kill it
    echo "Process opi-evpn-bridge is running. Killing it..."
    pkill opi-evpn-bridge
    sleep 1 # Optional: Wait for the process to be killed
fi
}

check_and_start_service "redis"
check_and_start_service "docker"

redis-cli -c FLUSHALL

check_and_start_docker_container "my_jaeger"

close_screen

netstat -tulpn | grep :4317

check_opi_evpn_running
export OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4317

(./opi-evpn-bridge --config config-ipu.yaml) &




