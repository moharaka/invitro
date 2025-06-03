/*
 * MIT License
 *
 * Copyright (c) 2023 EASL and the vHive community
 *
 * Permission is hereby granted, free of charge, to any person obtaining a copy
 * of this software and associated documentation files (the "Software"), to deal
 * in the Software without restriction, including without limitation the rights
 * to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
 * copies of the Software, and to permit persons to whom the Software is
 * furnished to do so, subject to the following conditions:
 *
 * The above copyright notice and this permission notice shall be included in all
 * copies or substantial portions of the Software.
 *
 * THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
 * IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
 * FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
 * AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
 * LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
 * OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
 * SOFTWARE.
 */

package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/vhive-serverless/loader/pkg/generator"

	"golang.org/x/exp/slices"

	"github.com/vhive-serverless/loader/pkg/common"
	"github.com/vhive-serverless/loader/pkg/config"
	"github.com/vhive-serverless/loader/pkg/driver"
	"github.com/vhive-serverless/loader/pkg/trace"

	log "github.com/sirupsen/logrus"
	tracer "github.com/vhive-serverless/vSwarm/utils/tracing/go"
)

const (
	zipkinAddr = "http://localhost:9411/api/v2/spans"
)

var (
	configPath    = flag.String("config", "cmd/config_knative_trace.json", "Path to loader configuration file")
	failurePath   = flag.String("failureConfig", "cmd/failure.json", "Path to the failure configuration file")
	verbosity     = flag.String("verbosity", "info", "Logging verbosity - choose from [info, debug, trace]")
	iatGeneration = flag.Bool("iatGeneration", false, "Generate IATs only or run invocations as well")
	iatFromFile   = flag.Bool("generated", false, "True if iats were already generated")
	dryRun        = flag.Bool("dryRun", false, "Dry run mode - do not deploy functions or generate invocations")
)

func init() {
	flag.Parse()

	log.SetFormatter(&log.TextFormatter{
		TimestampFormat: time.StampMilli,
		FullTimestamp:   true,
	})
	log.SetOutput(os.Stdout)

	switch *verbosity {
	case "debug":
		log.SetLevel(log.DebugLevel)
	case "trace":
		log.SetLevel(log.TraceLevel)
	default:
		log.SetLevel(log.InfoLevel)
	}
}

func main() {
	cfg := config.ReadConfigurationFile(*configPath)
	if cfg.EnableZipkinTracing {
		// TODO: how not to exclude Zipkin spans here? - file a feature request
		log.Warnf("Zipkin tracing has been enabled. This will exclude Istio spans from the Zipkin traces.")
		shutdown, err := tracer.InitBasicTracer(zipkinAddr, "loader")
		if err != nil {
			log.Print(err)
		}
		defer shutdown()
	}
	if cfg.ExperimentDuration < 1 {
		log.Fatal("Runtime duration should be longer, at least a minute.")
	}

	supportedPlatforms := []string{
		common.PlatformKnative,
		common.PlatformOpenWhisk,
		common.PlatformAWSLambda,
		common.PlatformDirigent,
	}
	if !slices.Contains(supportedPlatforms, cfg.Platform) {
		log.Fatal("Unsupported platform!")
	}

	if cfg.Platform == common.PlatformKnative {
		common.CheckCPULimit(cfg.CPULimit)
	}

	if cfg.TracePath == "RPS" {
		runRPSMode(&cfg, *iatFromFile, *iatGeneration)
	} else {
		runTraceMode(&cfg, *iatFromFile, *iatGeneration)
	}
}

func determineDurationToParse(runtimeDuration int, warmupDuration int) int {
	result := 0

	if warmupDuration > 0 {
		result += warmupDuration // warmup
	}

	result += runtimeDuration // actual experiment

	return result
}

func parseIATDistribution(cfg *config.LoaderConfiguration) (common.IatDistribution, bool) {
	switch cfg.IATDistribution {
	case "exponential":
		return common.Exponential, false
	case "exponential_shift":
		return common.Exponential, true
	case "uniform":
		return common.Uniform, false
	case "uniform_shift":
		return common.Uniform, true
	case "equidistant":
		return common.Equidistant, false
	default:
		log.Fatal("Unsupported IAT distribution.")
	}

	return common.Exponential, false
}

func parseYAMLSpecification(cfg *config.LoaderConfiguration) string {
	switch cfg.YAMLSelector {
	case "container":
		return "workloads/container/trace_func_go.yaml"
	case "firecracker":
		return "workloads/firecracker/trace_func_go.yaml"
	default:
		if cfg.Platform != common.PlatformDirigent {
			log.Fatal("Invalid 'YAMLSelector' parameter.")
		}
	}

	return ""
}

func parseTraceGranularity(cfg *config.LoaderConfiguration) common.TraceGranularity {
	switch cfg.Granularity {
	case "minute":
		return common.MinuteGranularity
	case "second":
		return common.SecondGranularity
	default:
		log.Fatal("Invalid trace granularity parameter.")
	}

	return common.MinuteGranularity
}

func runTraceMode(cfg *config.LoaderConfiguration, readIATFromFile bool, writeIATsToFile bool) {
	durationToParse := determineDurationToParse(cfg.ExperimentDuration, cfg.WarmupDuration)
	yamlPath := parseYAMLSpecification(cfg)
	var functions []*common.Function
	var traceParser trace.Parser

	// Azure trace parsing
	if !cfg.VSwarm {
		traceParser = trace.NewAzureParser(cfg.TracePath, durationToParse, yamlPath)
	} else {
		traceParser = trace.NewMapperParser(cfg.TracePath, durationToParse)
	}

	functions = traceParser.Parse()
	// Dirigent metadata parsing
	dirigentMetadataParser := trace.NewDirigentMetadataParser(cfg.TracePath, functions, yamlPath, cfg.Platform)
	dirigentMetadataParser.Parse()

	log.Infof("Traces contain the following %d functions:\n", len(functions))
	for _, function := range functions {
		fmt.Printf("\t%s\n", function.Name)
	}

	iatType, shiftIAT := parseIATDistribution(cfg)

	experimentDriver := driver.NewDriver(&config.Configuration{
		LoaderConfiguration:  cfg,
		FailureConfiguration: config.ReadFailureConfiguration(*failurePath),

		// loads dirigent config only if the platform is 'dirigent'
		DirigentConfiguration: config.ReadDirigentConfig(cfg),

		IATDistribution:  iatType,
		ShiftIAT:         shiftIAT,
		TraceGranularity: parseTraceGranularity(cfg),
		TraceDuration:    durationToParse,

		TestMode: false,

		Functions: functions,
	})

	// Skip experiments execution during dry run mode
	if *dryRun {
		return
	}

	log.Infof("Using %s as a service YAML specification file.\n", yamlPath)

	experimentDriver.GenerateSpecification()
	experimentDriver.ReadOrWriteFileSpecification(writeIATsToFile, readIATFromFile)
	experimentDriver.RunExperiment()
}

func runRPSMode(cfg *config.LoaderConfiguration, readIATFromFile bool, writeIATsToFile bool) {
	experimentDuration := determineDurationToParse(cfg.ExperimentDuration, cfg.WarmupDuration)
	yamlPath := parseYAMLSpecification(cfg)

	rpsTarget := cfg.RpsTarget
	coldStartPercentage := cfg.RpsColdStartRatioPercentage

	warmStartRPS := rpsTarget * (100 - coldStartPercentage) / 100
	coldStartRPS := rpsTarget * coldStartPercentage / 100

	iatType, shiftIAT := parseIATDistribution(cfg)

	fmt.Printf("\nExperiment Configuration:\n")
	fmt.Printf("------------------------\n")
	fmt.Printf("Duration: %d %s\n", experimentDuration, cfg.Granularity)
	fmt.Printf("Total Target RPS: %.2f\n", rpsTarget)
	fmt.Printf("Warm Start RPS: %.2f (%.1f%%)\n", warmStartRPS, 100-coldStartPercentage)
	fmt.Printf("Cold Start RPS: %.2f (%.1f%%)\n", coldStartRPS, coldStartPercentage)
	fmt.Printf("IAT Distribution: %s (shift=%v)\n", cfg.IATDistribution, shiftIAT)
	fmt.Printf("Seed: %d\n\n", cfg.Seed)

	fmt.Printf("Generating warm start function with RPS: %.2f\n", warmStartRPS)
	warmFunction, warmStartCount := generator.GenerateWarmStartFunction(0, experimentDuration, parseTraceGranularity(cfg), warmStartRPS, iatType, shiftIAT, cfg.Seed)
	
	if len(warmFunction) > 0 {
		fmt.Printf("\nWarm Function IAT Statistics:\n")
		fmt.Printf("-------------------------\n")
		fmt.Printf("Number of requests: %d\n", len(warmFunction))
		fmt.Printf("First 5 IATs (μs): %v\n", warmFunction[:min(5, len(warmFunction))])
		fmt.Printf("Expected avg IAT (μs): %.2f\n", 1000000.0/warmStartRPS)
		
		// Calculate actual average IAT
		sum := 0.0
		for _, iat := range warmFunction {
			sum += iat
		}
		fmt.Printf("Actual avg IAT (μs): %.2f\n", sum/float64(len(warmFunction)))
		
		fmt.Printf("\nInvocations per %s: %v\n", cfg.Granularity, warmStartCount)
		
		// Calculate actual RPS
		totalTimeUs := sum
		actualRPS := float64(len(warmFunction)) / (totalTimeUs / 1000000.0)
		fmt.Printf("Actual RPS: %.2f\n\n", actualRPS)
	}
	
	fmt.Printf("Generating cold start functions with RPS: %.2f\n", coldStartRPS)
	coldFunctions, coldStartCount := generator.GenerateColdStartFunctions(experimentDuration, parseTraceGranularity(cfg), coldStartRPS, cfg.RpsCooldownSeconds, iatType, shiftIAT, cfg.Seed)

	if len(coldFunctions) > 0 {
		fmt.Printf("\nCold Functions:\n")
		fmt.Printf("---------------\n")
		fmt.Printf("Number of cold functions: %d\n", len(coldFunctions))
		for i, cf := range coldFunctions {
			fmt.Printf("Cold function %d: %d requests, First IAT=%.2f μs\n", 
				i, len(cf), cf[0])
		}
	}

	// loads dirigent config only if the platform is 'dirigent'
	dirigentConfig := config.ReadDirigentConfig(cfg)

	var warmFunctions []common.IATArray
	var warmStartCounts [][]int
	if warmFunction != nil {
		warmFunctions = []common.IATArray{warmFunction}
		warmStartCounts = [][]int{warmStartCount}
	}

	experimentDriver := driver.NewDriver(&config.Configuration{
		LoaderConfiguration: cfg,
		TraceDuration:       experimentDuration,

		DirigentConfiguration: dirigentConfig,

		Functions: generator.CreateRPSFunctions(cfg, dirigentConfig, warmFunctions, warmStartCounts, coldFunctions, coldStartCount, yamlPath),
	})

	// Skip experiments execution during dry run mode
	if *dryRun {
		return
	}

	experimentDriver.ReadOrWriteFileSpecification(writeIATsToFile, readIATFromFile)
	experimentDriver.RunExperiment()
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
