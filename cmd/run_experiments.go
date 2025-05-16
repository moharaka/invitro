package main

import (
    "flag"
    "fmt"
    "io/ioutil"
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
  "TracePath": "data/traces/sampled_150/$exp/",
  "Granularity": "minute",
  "OutputPathPrefix": "data/exp_$cpu/experiment_$exp",
  "IATDistribution": "exponential",
  "CPULimit": "$cpu",
  "ExperimentDuration": 20,
  "WarmupDuration": 0,
  "IsPartiallyPanic": false,
  "EnableZipkinTracing": false,
  "EnableMetricsScrapping": false,
  "MetricScrapingPeriodSeconds": 15,
  "AutoscalingMetric": "concurrency",
  "GRPCConnectionTimeoutSeconds": 15,
  "GRPCFunctionTimeoutSeconds": 900,
  "DAGMode": false,
  "EnableDAGDataset": true,
  "Width": 2, 
  "Depth": 2
}
`

var allowedCPUs = map[string]bool{
    "0.1vCPU": true,
    "1vCPU":   true,
    "2vCPU":   true,
    "4vCPU":   true,
}

func runExperiment(exp int, cpu string) {
    expStr := fmt.Sprintf("%d", exp)
    configContent := strings.ReplaceAll(configTemplate, "$exp", expStr)
    configContent = strings.ReplaceAll(configContent, "$cpu", cpu)
    fileName := fmt.Sprintf("cmd/config_knative_trace_%d.json", exp)

    // Write the config file
    err := ioutil.WriteFile(fileName, []byte(configContent), 0644)
    if err != nil {
        fmt.Printf("Failed to write file %s: %v\n", fileName, err)
        os.Exit(1)
    }
    fmt.Printf("Generated %s\n", fileName)

    // Run the command
    cmd := exec.Command("go", "run", "cmd/loader.go", "--config", fileName)
    cmd.Stdout = os.Stdout
    cmd.Stderr = os.Stderr
    fmt.Printf("Running: go run cmd/loader.go --config %s\n", fileName)
    if err := cmd.Run(); err != nil {
        fmt.Printf("Command failed for %s: %v\n", fileName, err)
        os.Exit(1)
    }

    // Run the cleanup command
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
    flag.Parse()

    if !allowedCPUs[*cpuPtr] {
        fmt.Printf("Invalid CPU value: %s\nAllowed values: 0.1vCPU, 1vCPU, 2vCPU, 4vCPU\n", *cpuPtr)
        os.Exit(1)
    }

    fmt.Printf("Runnig experiement with %s\n", *cpuPtr)
    runExperiment(10, *cpuPtr)
    for exp := 100; exp <= 900; exp += 100 {
        runExperiment(exp, *cpuPtr)
    }
}
