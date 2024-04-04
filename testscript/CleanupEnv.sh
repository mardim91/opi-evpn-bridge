#!/bin/bash


# Function to check if a service is running and start it if not
check_and_stop_service() {
    local service_name="$1"

    # Check if the service is running
    if systemctl is-active --quiet "$service_name"; then
        echo "Service $service_name is running. Stopping it now..."
        systemctl stop "$service_name"
        
    fi
}

# Function to check if a Docker container is running and stop it if it is
check_and_stop_docker_container() {
    local container_name="$1"

    # Check if the Docker container is running
    if docker inspect -f '{{.State.Running}}' "$container_name" &> /dev/null; then
        echo "Container '$container_name' is running. Stopping container..."
        docker stop "$container_name"
    else
        echo "Container '$container_name' is not running."
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

# Check for a running container and stop it
echo "Checking if my_jaegar container is running....."
check_and_stop_docker_container "my_jaeger"


# Check for a running service and stop it

echo "Checking if docker is running....."
check_and_stop_service "docker"

# Flush the redis database
redis-cli -c FLUSHALL

# Check if opi-evpn-bridge is running , Close if running

echo "Checking if opi-evpn-bridge is running....."
check_opi_evpn_running

# Check for a running service and stop it

echo "Checking if redis server container is running....."
check_and_stop_service "redis"
