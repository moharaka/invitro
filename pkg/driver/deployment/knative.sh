CONFIG_FILE=$1
export FUNC_NAME=$2
export CPU_REQUEST=$3
export CPU_LIMITS=$4
export MEMORY_REQUESTS=$5
INIT_SCALE=$6
export PANIC_WINDOW=$7
export PANIC_THRESHOLD=$8
export AUTOSCALING_METRIC=$9
export AUTOSCALING_TARGET=${10}
export COLD_START_BUSY_LOOP_MS=${11}

# Handle MAX_SCALE (now using $12 instead of ${12} for better compatibility)
if [ -n "${12}" ]; then
    export MAX_SCALE="\"${12}\""
elif [ -n "${MAX_SCALE}" ]; then
    export MAX_SCALE="\"${MAX_SCALE}\""
else    
    export MAX_SCALE="\"200\""
fi

# Create directory for output if specified
if [ -n "$OUTPUT_CONFIG_PATH" ]; then
    mkdir -p "$(dirname "$OUTPUT_CONFIG_PATH")"
fi

# Process config and conditionally write to file
processed_config=$(cat "$CONFIG_FILE" | envsubst)

# Write to file if OUTPUT_CONFIG_PATH is set
if [ -n "$OUTPUT_CONFIG_PATH" ]; then
    echo "$processed_config" > "$OUTPUT_CONFIG_PATH"
    echo "Saved processed config to: $OUTPUT_CONFIG_PATH"
fi

# Apply configuration to knative
echo "$processed_config" | kn service apply "$FUNC_NAME" \
    --scale-init "$INIT_SCALE" \
    --concurrency-target 1 \
    --wait-timeout 2000000 \
    -f /dev/stdin

