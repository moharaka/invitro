sudo modprobe msr

sudo apt install -y linux-tools-$(uname -r)

# Disable SMT
echo off | sudo tee /sys/devices/system/cpu/smt/control

# Disable Intel P-State
echo passive | sudo tee /sys/devices/system/cpu/intel_pstate/status
sudo cpupower frequency-set -g userspace

# Set CPU frequency to 2.2GHz
sudo cpupower frequency-set -f 2200000
sudo cpupower frequency-set -u 2200000
sudo cpupower frequency-set -d 2200000

# Set energy performance bias to "balance"
echo 0 | sudo tee /sys/devices/system/cpu/cpu*/power/energy_perf_bias
