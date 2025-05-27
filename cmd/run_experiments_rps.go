package main

import (
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"strings"
)

const configTemplate = `{
  "Seed": 42,

  "Platform": "Knative",
  "InvokeProtocol" : "grpc",
  "YAMLSelector": "container",
  "EndpointPort": 80,

  "BusyLoopOnSandboxStartup": false,

  "RpsTarget": $rps,
  "RpsColdStartRatioPercentage": 0,
  "RpsCooldownSeconds": 10,
  "RpsImage": "ghcr.io/vhive-serverless/invitro_empty_function:latest",
  "RpsRuntimeMs": 10,
  "RpsMemoryMB": 2048,
  "RpsIterationMultiplier": 80,

  "TracePath": "RPS",
  "Granularity": "minute",
  "OutputPathPrefix": "$outputDir/experiment_$rps",
  "IATDistribution": "equidistant",
  "CPULimit": "$cpu",
  "ExperimentDuration": 3,
  "WarmupDuration": 5,

  "IsPartiallyPanic": false,
  "EnableZipkinTracing": false,
  "EnableMetricsScrapping": false,
  "MetricScrapingPeriodSeconds": 15,
  "AutoscalingMetric": "concurrency",

  "GRPCConnectionTimeoutSeconds": 15,
  "GRPCFunctionTimeoutSeconds": 900
}`

// Helper function to get experiment directory path
func getExperimentDir(cpu string) string {
	return fmt.Sprintf("data/out/exp_rps_fx_cs_%s", cpu)
}

func runExperiment(rps int, cpu string) {
	experimentDir := getExperimentDir(cpu)
	
	// Create experiment directory
	err := os.MkdirAll(experimentDir, 0755)
	if err != nil {
		fmt.Printf("Failed to create directory %s: %v\n", experimentDir, err)
		os.Exit(1)
	}

	// Set environment variable
	yamlPath := fmt.Sprintf("%s/config.yaml", experimentDir)
	if err := os.Setenv("OUTPUT_CONFIG_PATH", yamlPath); err != nil {
		log.Fatalf("Failed to set OUTPUT_CONFIG_PATH: %v", err)
	}

	// Generate JSON config
	rpsStr := fmt.Sprintf("%d", rps)
	configContent := strings.ReplaceAll(configTemplate, "$rps", rpsStr)
	configContent = strings.ReplaceAll(configContent, "$cpu", cpu)
	configContent = strings.ReplaceAll(configContent, "$outputDir", experimentDir)
	fileName := fmt.Sprintf("%s/config_knative_trace_%s_%d.json", experimentDir, cpu, rps)

	// Write JSON config
	if err := ioutil.WriteFile(fileName, []byte(configContent), 0644); err != nil {
		fmt.Printf("Failed to write file %s: %v\n", fileName, err)
		os.Exit(1)
	}
	fmt.Printf("Generated %s\n", fileName)

	// Run loader command
	cmd := exec.Command("go", "run", "cmd/loader.go", "--config", fileName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("Running: go run cmd/loader.go --config %s\n", fileName)
	if err := cmd.Run(); err != nil {
		fmt.Printf("Command failed for %s: %v\n", fileName, err)
		os.Exit(1)
	}

	// Cleanup
	cmd = exec.Command("make", "clean")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	fmt.Printf("Running: make clean\n")
	if err := cmd.Run(); err != nil {
		fmt.Printf("Command failed for %s: %v\n", fileName, err)
		os.Exit(1)
	}
}

func main() {
	cpuPtr := flag.String("cpu", "1vCPU", "CPU limit (options: 0.1vCPU, 1vCPU, 2vCPU, 4vCPU)")
	startRPtr := flag.Int("start", 10, "Starting RPS value")
	endRPtr := flag.Int("end", 200, "Ending RPS value")
	stepRPtr := flag.Int("step", 10, "RPS increment step")
	maxScalePtr := flag.Int("max_scale", 1, "Maximum scale factor (constant for all runs)")
	flag.Parse()

	// Set MAX_SCALE environment variable
	if err := os.Setenv("MAX_SCALE", fmt.Sprintf("%d", *maxScalePtr)); err != nil {
		log.Fatalf("Failed to set MAX_SCALE: %v", err)
	}

	fmt.Printf("CPU: %s | RPS Range: %d-%d (step %d) | Max Scale: %d\n",
		*cpuPtr, *startRPtr, *endRPtr, *stepRPtr, *maxScalePtr)

	runExperiment(1, *cpuPtr)
	for rps := *startRPtr; rps <= *endRPtr; rps += *stepRPtr {
		runExperiment(rps, *cpuPtr)
	}
}

