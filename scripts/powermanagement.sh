#!/bin/bash
# HDMI Power & Powertop Management Script
# Usage: ./script.sh [performance | powersave | auto]
# Default is 'auto' if no argument is provided.

# --- 1. CONFIGURATION & PATHS ---
export PATH=/usr/local/sbin:/usr/local/bin:/usr/bin:/usr/sbin:/bin:/sbin

# Find the HDMI status file (Framework Laptop usually card0 or card1)
HDMI_DIR=$(find /sys/class/drm/ -name "card*-HDMI-A-1" | head -n 1)
HDMI_STATUS_FILE="${HDMI_DIR}/status"

# --- 2. HELPER FUNCTIONS ---

# Function: Check Connectivity
# Returns 0 (True) if connected, 1 (False) if not.
is_hdmi_active() {
    if [ ! -f "$HDMI_STATUS_FILE" ]; then
        return 1 # File not found -> Assume disconnected
    fi

    STATUS=$(cat "$HDMI_STATUS_FILE")

    # STRICT CHECK: Only return 0 (Bash True) if it is EXACTLY "connected"
    if [[ "$STATUS" == "connected" ]]; then
        return 0 # Success/True
    else
        return 1 # Failure/False
    fi
}

# Function: Enable Max Power Savings (When Disconnected)
enable_powertop_tune() {
    echo "  -> Applying Powertop Auto-Tune..."
    if command -v powertop &> /dev/null; then
        powertop --auto-tune &> /dev/null
    fi
}

# Function: Disable Latency-Inducing Savings (When Connected)
disable_latency() {
    echo "  -> Reverting USB & Audio Latency..."
    
    # 1. Disable USB Autosuspend (Fixes mouse/keyboard lag)
    for i in /sys/bus/usb/devices/*/power/control; do
        if [ -w "$i" ]; then echo on > "$i"; fi
    done

    # 2. Disable Audio Power Save (Fixes audio popping)
    if [ -w /sys/module/snd_hda_intel/parameters/power_save ]; then
        echo 0 > /sys/module/snd_hda_intel/parameters/power_save
    fi
}

# --- 3. CORE LOGIC BLOCKS ---

set_mode_performance() {
    echo "[MODE SET] Switching to Performance Mode ($1)" | systemd-cat -t fw-power
    
    # 1. Power Profile -> Balanced
    powerprofilesctl set balanced

    # 2. CPU Preference -> Speed
    if [ -d /sys/devices/system/cpu/cpu0/cpufreq ]; then
        echo balance_performance | tee /sys/devices/system/cpu/cpu*/cpufreq/energy_performance_preference > /dev/null
    fi

    # 3. Scheduler -> Gaming
    if command -v scxctl &> /dev/null; then
        scxctl switch --sched scx_lavd --mode gaming
    fi

    # 4. POWERTOP -> DISABLE LATENCY
    disable_latency
}

set_mode_powersave() {
    echo "[MODE SET] Switching to Power Saver ($1)" | systemd-cat -t fw-power

    # 1. Power Profile -> Power Saver
    powerprofilesctl set power-saver

    # 2. CPU Preference -> Power Saving
    if [ -d /sys/devices/system/cpu/cpu0/cpufreq ]; then
        echo power | tee /sys/devices/system/cpu/cpu*/cpufreq/energy_performance_preference > /dev/null
    fi

    # 3. Scheduler -> Powersave
    if command -v scxctl &> /dev/null; then
        scxctl switch --sched scx_lavd --mode powersave
    fi

    # 4. PCIe ASPM -> Aggressive
    if [ -f /sys/module/pcie_aspm/parameters/policy ]; then
        echo powersupersave > /sys/module/pcie_aspm/parameters/policy
    fi

    # 5. POWERTOP -> MAX SAVINGS
    enable_powertop_tune
}

# --- 4. EXECUTION FLOW ---

# Read the first argument passed to the script, default to "auto"
MODE="${1:-auto}"

case "$MODE" in
    performance|perf)
        set_mode_performance "Manual Override"
        ;;
    
    powersave|save)
        set_mode_powersave "Manual Override"
        ;;
        
    auto)
        if is_hdmi_active; then
            set_mode_performance "HDMI Detected"
        else
            set_mode_powersave "HDMI Disconnected"
        fi
        ;;
        
    *)
        echo "Usage: $0 [performance | powersave | auto]"
        exit 1
        ;;
esac
