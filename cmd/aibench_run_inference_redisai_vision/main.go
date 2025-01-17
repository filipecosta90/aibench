package main

import (
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/RedisAI/aibench/inference"
	_ "github.com/lib/pq"
	"github.com/mediocregopher/radix/v3"
	"sync"
)

// Global vars:
var (
	runner                  *inference.BenchmarkRunner
	host                    string
	port                    string
	model                   string
	persistOutputs          bool
	showExplain             bool
	clusterMode             bool
	useDag                  bool
	continueOnError         bool
	PoolPipelineConcurrency int
	dialReadTimeout         time.Duration
	PoolPipelineWindow      time.Duration
	inferenceType           = "RedisAI Query - mobilenet_v1_100_224 "
	tensorBenchmarkBytes    = 4 * 1 * 224 * 224 * 3 // number of bytes per float * N x H x W x C
	batchSize               int
	batchSizeStr            string
	metricsCollector        inference.MetricCollector
	metricsPools            []*radix.Pool
	metricsHosts            []string
)

// Vars only for git sha and diff handling
var GitSHA1 = ""
var GitDirty = "0"

func AibenchGitSHA1() string {
	return GitSHA1
}

func AibenchGitDirty() (dirty bool) {
	dirty = false
	dirtyLines, err := strconv.Atoi(GitDirty)
	if err == nil {
		dirty = dirtyLines != 0
	}
	return
}

// Parse args:
func init() {
	runner = inference.NewBenchmarkRunner()
	flag.StringVar(&host, "host", "localhost", "Redis host address, if more than one is passed will round robin requests")
	flag.StringVar(&port, "port", "6379", "Redis host port, if more than one is passed will round robin requests")
	flag.StringVar(&model, "model", "mobilenet_v1_100_224_cpu", "model name")
	flag.BoolVar(&persistOutputs, "persist-results", false, "persist the classification tensors")
	flag.BoolVar(&useDag, "use-dag", false, "use DAGRUN")
	flag.BoolVar(&continueOnError, "continue-on-error", true, "If an error reply is received continue and only log the error message")
	flag.BoolVar(&clusterMode, "cluster-mode", false, "read cluster slots and distribute inferences among shards.")
	flag.DurationVar(&PoolPipelineWindow, "pool-pipeline-window", 500*time.Microsecond, "If window is zero then implicit pipelining will be disabled")
	flag.DurationVar(&dialReadTimeout, "dial-read-timeout", 90*time.Second, "Redis connection dial timeout")
	flag.IntVar(&PoolPipelineConcurrency, "pool-pipeline-concurrency", 0, "If limit is zero then no limit will be used and pipelines will only be limited by the specified time window")
	flag.IntVar(&batchSize, "batch-size", 1, "Input tensor batch size")
	version := flag.Bool("v", false, "Output version and exit")
	flag.Parse()
	if *version {
		git_sha := AibenchGitSHA1()
		git_dirty_str := ""
		if AibenchGitDirty() {
			git_dirty_str = "-dirty"
		}
		fmt.Fprintf(os.Stdout, "aibench_run_inference_redisai_vision (git_sha1:%s%s)\n", git_sha, git_dirty_str)
		os.Exit(0)
	}
	inferenceType += fmt.Sprintf("(input tensor batch size=%d):", batchSize)
	if useDag {
		if persistOutputs {
			inferenceType += "AI.DAGRUN with persistency ON"
		} else {
			inferenceType += "AI.DAGRUN with persistency OFF"
		}
	} else {
		inferenceType += "AI.MODELRUN"
	}
	batchSizeStr = fmt.Sprintf("%d", batchSize)
}

func main() {
	rowBenchmarkBytes := batchSize * tensorBenchmarkBytes
	metricsCollector = &Processor{}
	hosts := strings.Split(host, ",")
	ports := strings.Split(port, ",")
	metricsPools = make([]*radix.Pool, len(hosts))
	metricsHosts = make([]string, len(hosts))
	var err error = nil
	for idx, h := range hosts {
		addr := fmt.Sprintf("%s:%s", h, ports[idx])
		metricsHosts[idx] = addr
		connFunc := func(network, addr string) (radix.Conn, error) {
			return radix.Dial(network, addr, radix.DialReadTimeout(dialReadTimeout))
		}
		metricsPools[idx], err = radix.NewPool("tcp", addr, 1, radix.PoolConnFunc(connFunc))
		if err != nil {
			log.Fatalf("Error preparing for metrics collectors, while creating new pool. error = %v", err)
		}
	}

	runner.Run(&inference.RedisAIPool, newProcessor, rowBenchmarkBytes, int64(batchSize), newCollector)
}

type queryExecutorOptions struct {
	showExplain   bool
	debug         bool
	printResponse bool
}

type Processor struct {
	opts    *queryExecutorOptions
	Metrics chan uint64
	Wg      *sync.WaitGroup
	pclient []*radix.Pool
}

func (p *Processor) Close() {
	if p.pclient != nil {
		for _, client := range p.pclient {
			client.Close()
		}
	}
}

func (p *Processor) CollectRunTimeMetrics() (ts int64, stats interface{}, err error) {
	var hosts_metrics_map = make(map[string]interface{})
	for pos, h := range metricsPools {
		var aicpu_rcv string
		var aiinfo_rcv []string
		var commandstats_rcv string
		var infomemory_rcv string
		var kvmap = make(map[string]interface{})
		pipeCmds := radix.Pipeline(
			radix.FlatCmd(&aicpu_rcv, "INFO", "MODULES"),
			radix.FlatCmd(&aiinfo_rcv, "AI.INFO", model),
			radix.FlatCmd(nil, "AI.INFO", model, "RESETSTAT"),
			radix.FlatCmd(&commandstats_rcv, "INFO", "COMMANDSTATS"),
			radix.FlatCmd(&infomemory_rcv, "INFO", "MEMORY"),
			radix.FlatCmd(nil, "CONFIG", "RESETSTAT"),
		)
		err = h.Do(pipeCmds)
		if err != nil {
			return
		}
		process_ainfo_reply(aiinfo_rcv, kvmap)
		process_commandstats_reply(commandstats_rcv, kvmap)
		process_memorystats_reply(infomemory_rcv, kvmap)
		process_info_modules_ai_cpu(aicpu_rcv, kvmap)
		hosts_metrics_map[metricsHosts[pos]] = kvmap
	}
	stats = hosts_metrics_map
	return
}

func stringInSlice(a string, list []string) bool {
	for _, b := range list {
		if b == a {
			return true
		}
	}
	return false
}

func process_ainfo_reply(aiinfo_rcv []string, kvmap map[string]interface{}) {
	interested_keys := []string{"duration", "samples", "calls", "errors"}
	for i := 0; i < len(aiinfo_rcv); i += 2 {
		k := aiinfo_rcv[i]
		if stringInSlice(k, interested_keys) {
			sk := fmt.Sprintf("ai_info.%s", k)
			sv := aiinfo_rcv[i+1]
			kvmap[sk] = sv
		}
	}
}

func process_commandstats_reply(commandstats_rcv string, kvmap map[string]interface{}) {
	ai_cpu_idx := strings.Index(commandstats_rcv, "Commandstats")
	if ai_cpu_idx > -1 {
		ai_cpu_str := commandstats_rcv[ai_cpu_idx:]
		ai_cpu_metrics_str_arr := strings.Split(ai_cpu_str, "\r\n")[1:]
		for _, kv_str := range ai_cpu_metrics_str_arr {
			kv := strings.Split(kv_str, ":")
			if len(kv) == 2 {
				k := kv[0]
				v := kv[1]
				kvmap[k] = v
			}

		}
	}
}

func process_memorystats_reply(commandstats_rcv string, kvmap map[string]interface{}) {
	ai_cpu_idx := strings.Index(commandstats_rcv, "Memory")
	if ai_cpu_idx > -1 {
		ai_cpu_str := commandstats_rcv[ai_cpu_idx:]
		ai_cpu_metrics_str_arr := strings.Split(ai_cpu_str, "\r\n")[1:]
		for _, kv_str := range ai_cpu_metrics_str_arr {
			kv := strings.Split(kv_str, ":")
			if len(kv) == 2 {
				k := kv[0]
				v := kv[1]
				kvmap[k] = v
			}

		}
	}
}

func process_info_modules_ai_cpu(rcv string, kvmap map[string]interface{}) {
	ai_cpu_idx := strings.Index(rcv, "ai_cpu")
	if ai_cpu_idx > -1 {
		ai_cpu_str := rcv[ai_cpu_idx:]
		ai_cpu_end_idx := strings.Index(ai_cpu_str, "# ")
		if ai_cpu_end_idx > -1 {
			ai_cpu_str = ai_cpu_str[:ai_cpu_end_idx]
		}
		ai_cpu_metrics_str_arr := strings.Split(ai_cpu_str, "\r\n")[1:]
		for _, kv_str := range ai_cpu_metrics_str_arr {
			kv := strings.Split(kv_str, ":")
			if len(kv) == 2 {
				k := kv[0]
				v := kv[1]
				kvmap[k] = v
			}

		}
	}
}

func newCollector() inference.MetricCollector { return metricsCollector }

func newProcessor() inference.Processor { return &Processor{} }

func (p *Processor) Init(numWorker int, totalWorkers int, wg *sync.WaitGroup, m chan uint64, rs chan uint64) {
	p.opts = &queryExecutorOptions{
		showExplain:   showExplain,
		debug:         runner.DebugLevel() > 0,
		printResponse: runner.DoPrintResponses(),
	}
	p.Wg = wg
	p.Metrics = m
	var err error

	hosts := strings.Split(host, ",")
	ports := strings.Split(port, ",")

	// if we have more hosts than workers lets connect to them all
	if len(hosts) > totalWorkers {
		err = p.connectAllHosts(hosts, ports, err)
	} else {
		pos := (numWorker + 1) % len(hosts)
		p.pclient = make([]*radix.Pool, 1)
		addr := fmt.Sprintf("%s:%s", hosts[pos], ports[pos])
		connFunc := func(network, addr string) (radix.Conn, error) {
			return radix.Dial(network, addr, radix.DialReadTimeout(dialReadTimeout))
		}
		p.pclient[0], err = radix.NewPool("tcp", addr, 1, radix.PoolConnFunc(connFunc))
		if err != nil {
			log.Fatalf("Error preparing for DAGRUN(), while creating new pool. error = %v", err)
		}
	}
}

func (p *Processor) connectAllHosts(hosts []string, ports []string, err error) error {
	p.pclient = make([]*radix.Pool, len(hosts))
	for idx, h := range hosts {
		addr := fmt.Sprintf("%s:%s", h, ports[idx])
		connFunc := func(network, addr string) (radix.Conn, error) {
			return radix.Dial(network, addr, radix.DialReadTimeout(dialReadTimeout))
		}
		p.pclient[idx], err = radix.NewPool("tcp", addr, 1, radix.PoolConnFunc(connFunc))
		if err != nil {
			log.Fatalf("Error preparing for DAGRUN(), while creating new pool. error = %v", err)
		}
	}
	return err
}

func (p *Processor) ProcessInferenceQuery(q []byte, isWarm bool, workerNum int, useReferenceDataRedis bool, useReferenceDataMysql bool, queryNumber int64) ([]*inference.Stat, error) {

	// No need to run again for EXPLAIN
	if isWarm && p.opts.showExplain {
		return nil, nil
	}
	tensorName := fmt.Sprintf("imageTensor:{w%d}", workerNum)
	outputTensorName := fmt.Sprintf("classificationTensor:{w%d}", workerNum)
	tensorValues := q
	pos := rand.Int31n(int32(len(p.pclient)))
	var err error
	start := time.Now()
	if useDag {
		var args []string
		if persistOutputs {
			args = []string{"PERSIST", "1", outputTensorName, "|>"}

		} else {
			args = []string{"|>"}
		}
		args = append(args,
			"AI.TENSORSET", tensorName, "FLOAT", batchSizeStr, "224", "224", "3", "BLOB", string(tensorValues), "|>",
			"AI.MODELRUN", model, "INPUTS", tensorName, "OUTPUTS", outputTensorName, "|>",
			"AI.TENSORGET", outputTensorName, "BLOB")
		err = p.pclient[pos].Do(radix.Cmd(nil, "AI.DAGRUN", args...))
	} else {
		pipeCmds := radix.Pipeline(
			radix.FlatCmd(nil, "AI.TENSORSET", tensorName, "FLOAT", batchSizeStr, "224", "224", "3", "BLOB", string(tensorValues)),
			radix.FlatCmd(nil, "AI.MODELRUN", model, "INPUTS", tensorName, "OUTPUTS", outputTensorName),
			radix.FlatCmd(nil, "AI.TENSORGET", outputTensorName, "BLOB"),
		)
		err = p.pclient[pos].Do(pipeCmds)
	}
	took := time.Since(start).Microseconds()
	if err != nil {
		extendedError := fmt.Errorf("Prediction Receive() failed:%v\n", err)
		if !continueOnError {
			log.Fatal(extendedError)
		} else {
			fmt.Fprint(os.Stderr, extendedError)
		}
	}

	stat := inference.GetStat()
	stat.Init([]byte(inferenceType), took, uint64(batchSize), false, "")

	return []*inference.Stat{stat}, nil
}
