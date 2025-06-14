package main

import (
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/olekukonko/tablewriter"
	"github.com/redis/go-redis/v9"
	"golang.org/x/net/context"
)

var (
	ctx        = context.Background()
	printMutex sync.Mutex
	logWriter  *os.File
)

var (
	// Redis connection configuration
	addr     = flag.String("addr", "", "Redis server address in the format <hostname>:<port>")
	password = flag.String("password", "", "Redis password")
	tlsFlag  = flag.Bool("tls", false, "Enable TLS for Redis connection")

	// Key scanning and memory usage configuration
	topN          = flag.Int("top", 100, "Maximum number of biggest keys to display")
	samples       = flag.Uint("samples", 5, "Samples for memory usage")
	sleepDuration = flag.Float64("sleep", 0, "Sleep duration (in seconds) after processing each batch")

	// Additional flags
	masterYes         = flag.Bool("master-yes", false, "Execute even if the Redis role is master")
	concurrency       = flag.Int("concurrency", 1, "Maximum number of nodes to process concurrently")
	clusterFlag       = flag.Bool("cluster-mode", false, "Enable cluster mode to get keys from all shards in the Redis cluster")
	directFlag        = flag.Bool("direct", false, "Perform operation on the specified node. If not specified, the operation will default to executing on the slave node")
	skipLazyfreeCheck = flag.Bool("skip-lazyfree-check", false, "Skip check lazyfree-lazy-expire")
	logFile           = flag.String("log-file", "", "Log file for saving progress and intermediate result")
)

// TypeInfo holds the command for checking key size and the unit of measurement
type TypeInfo struct {
	SizeCmd  string
	SizeUnit string
}

// KeyTypeSize holds the type of a key and the number of elements in it
type KeyTypeSize struct {
	Type       string
	ElementNum uint64
}

// KeySize holds the name of the key and its size
type KeySize struct {
	Key  string
	Size uint64
}

// BySize implements sort.Interface for []KeySize based on the Size field
type BySize []KeySize

func (a BySize) Len() int           { return len(a) }
func (a BySize) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a BySize) Less(i, j int) bool { return a[i].Size > a[j].Size } // Descending order

type NodeInfo struct {
	Address  string // Address of the Redis node
	Role     string // Role of the node (master or slave)
	MasterID string // For slaves, the ID of the master node
	ID       string // Unique ID for the node (used to link slave to master)
}

// TypeInfo map for Redis key types
var typeInfoMap = map[string]TypeInfo{
	"string": {"STRLEN", "bytes (value len)"},
	"list":   {"LLEN", "items"},
	"set":    {"SCARD", "members"},
	"hash":   {"HLEN", "fields"},
	"zset":   {"ZCARD", "members"},
	"stream": {"XLEN", "entries"},
	"none":   {"", ""},
}

func InitLogFile() (*os.File, error) {
	if *logFile == "" {
		nodes := strings.Split(*addr, ",")
		firstNode := nodes[0]

		timestamp := time.Now().Format("20060102_150405")
		*logFile = fmt.Sprintf("/tmp/%s_%s.txt", firstNode, timestamp)
		fmt.Printf("Log file not specified, using default: %s\n", *logFile)
	}

	var err error
	logWriter, err = os.OpenFile(*logFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("%v", err)
	}

	return logWriter, nil
}

func writeToLogFile(logWriter *os.File, logMessage string) {
	if logWriter != nil {
		printMutex.Lock()
		defer printMutex.Unlock()
		timestamp := time.Now().Format("2006/01/02 15:04:05")
		logMessage = fmt.Sprintf("%s %s", timestamp, logMessage)
		_, err := logWriter.WriteString(logMessage)
		if err != nil {
			log.Printf("Failed to write to log file: %v", err)
		}
	}
}

// bytesToHuman converts bytes to a human-readable string
func bytesToHuman(n uint64) string {
	units := []string{"bytes", "KB", "MB", "GB"}
	unit := units[0]
	nFloat := float64(n)

	for _, u := range units[1:] {
		if nFloat < 1024 {
			break
		}
		nFloat /= 1024
		unit = u
	}

	result := fmt.Sprintf("%.2f", nFloat)
	// Remove trailing zeros and decimal point
	if strings.Contains(result, ".") {
		result = strings.TrimRight(result, "0") // Remove trailing zeros
		result = strings.TrimRight(result, ".") // Remove decimal point if it's the last character
	}
	return result + " " + unit
}

func newRedisClient(addr string) (*redis.Client, error) {
	options := &redis.Options{
		Addr:        addr,
		Password:    *password,
		DialTimeout: 2 * time.Second,
	}

	if *tlsFlag {
		options.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	}

	rdb := redis.NewClient(options)

	// Test the connection by sending a Ping
	_, err := rdb.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("Error connecting to Redis: %v", err)
	}
	_, err = rdb.Do(ctx, "READONLY").Result()
	if err != nil {
		if err.Error() == "ERR This instance has cluster support disabled" {
			// Non-cluster mode, skip READONLY command
			return rdb, nil
		}
		return nil, fmt.Errorf("failed to send READONLY command: %v", err)
	}
	return rdb, nil
}

// getKeyInfoMap gathers key type and element counts for the biggest keys
func getKeyInfoMap(rdb *redis.Client, biggestKeys []KeySize) (map[string]KeyTypeSize, error) {
	keyNames := make([]string, len(biggestKeys))
	for i, keySize := range biggestKeys {
		keyNames[i] = keySize.Key
	}

	types, err := getKeyTypes(rdb, keyNames)
	if err != nil {
		return nil, err
	}
	elementNums, err := getKeySizes(rdb, keyNames, types, false, 0)
	if err != nil {
		return nil, err
	}

	keyInfoMap := make(map[string]KeyTypeSize, len(biggestKeys))

	for i, key := range keyNames {
		keyInfoMap[key] = KeyTypeSize{
			Type:       types[i],
			ElementNum: elementNums[i],
		}
	}

	return keyInfoMap, nil
}

// scanKeys scans Redis keys with the SCAN command
func scanKeys(rdb *redis.Client, cursor uint64) ([]string, uint64, error) {
	keys, newCursor, err := rdb.Scan(ctx, cursor, "*", 10).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("error scanning keys: %v", err)
	}
	return keys, newCursor, nil
}

// getRedisVersion retrieves the Redis version
func getRedisVersion(rdb *redis.Client) (string, int, error) {
	info, err := rdb.Info(ctx, "server").Result()
	if err != nil {
		return "", 0, fmt.Errorf("%v", err)
	}

	for _, line := range strings.Split(info, "\n") {
		if strings.HasPrefix(line, "redis_version:") {
			version := strings.TrimSpace(strings.Split(line, ":")[1])
			majorVersion, _ := strconv.Atoi(strings.Split(version, ".")[0])
			return version, majorVersion, nil
		}
	}
	return "", 0, fmt.Errorf("redis_version not found in server info")
}

// getKeyTypes retrieves the types of the specified keys
func getKeyTypes(rdb *redis.Client, keys []string) ([]string, error) {
	pipe := rdb.Pipeline()
	typeCmds := make([]*redis.StatusCmd, len(keys))
	for i, key := range keys {
		typeCmds[i] = pipe.Type(ctx, key)
	}
	// Execute the pipeline
	_, err := pipe.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("pipeline execution failed: %v", err)
	}
	types := make([]string, len(keys))

	for i, cmd := range typeCmds {
		if cmd.Err() != nil {
			return nil, fmt.Errorf("error getting type for key '%s': %v", keys[i], cmd.Err())
		}
		types[i] = cmd.Val()
	}

	return types, nil
}

// getKeySizes retrieves the sizes of the specified keys
func getKeySizes(rdb *redis.Client, keys []string, types []string, memkeys bool, samples uint) ([]uint64, error) {
	sizes := make([]uint64, len(keys))
	pipeline := rdb.Pipeline()
	commands := make([]*redis.Cmd, len(keys))

	for i, key := range keys {
		if !memkeys && typeInfoMap[types[i]].SizeCmd == "" {
			continue
		}

		if memkeys {
			commands[i] = pipeline.Do(ctx, "MEMORY", "USAGE", key, "SAMPLES", samples)
		} else {
			commands[i] = pipeline.Do(ctx, typeInfoMap[types[i]].SizeCmd, key)
		}
	}

	// Execute the pipeline
	_, err := pipeline.Exec(ctx)
	if err != nil {
		return nil, fmt.Errorf("error executing pipeline: %v", err)
	}

	// Collect the results
	for i, cmd := range commands {
		if !memkeys && typeInfoMap[types[i]].SizeCmd == "" {
			sizes[i] = 0
			continue
		}
		size, err := cmd.Uint64()
		if err != nil {
			return nil, fmt.Errorf("error getting size for key '%s': %v", keys[i], err)
		}
		sizes[i] = size
	}

	return sizes, nil
}

// printSummary prints the summary of the largest keys
func printSummary(nodeAddr string, sampled, totalKeyLen, totalKeys uint64, biggestKeys []KeySize, keyInfoMap map[string]KeyTypeSize) {
	fmt.Printf("\nNode: %s\n-------- Summary --------\n", nodeAddr)
	fmt.Printf("Sampled %d keys in the keyspace!\n", sampled)
	var avgLen uint64 = 0
	if sampled != 0 {
		avgLen = uint64(float64(totalKeyLen) / float64(sampled))
	}
	fmt.Printf("Total key length in bytes is %s (avg len %s)\n\n", bytesToHuman(totalKeyLen), bytesToHuman(avgLen))

	// Print biggest keys
	if sampled > 0 {
		table := tablewriter.NewWriter(os.Stdout)
		table.SetAutoFormatHeaders(false)
		table.SetHeader([]string{"Key", "Type", "Size", "Number of elements"})
		table.SetAlignment(tablewriter.ALIGN_CENTER)
		fmt.Println("Top biggest keys:")
		for _, ks := range biggestKeys {
			if ks.Size > 0 {
				keyName := ks.Key
				keyType := keyInfoMap[keyName].Type
				keyElementNum := keyInfoMap[keyName].ElementNum
				table.Append([]string{keyName, keyType, bytesToHuman(ks.Size), fmt.Sprintf("%d %s", keyElementNum, typeInfoMap[keyType].SizeUnit)})
			}
		}
		table.Render()
	}
}

/*
func compareKeySizes(rdb *redis.Client, keys []string, sizes []uint64, samples uint) error {
	for i, key := range keys {
		cmd := rdb.Do(ctx, "MEMORY", "USAGE", key, "SAMPLES", samples)
		memoryUsage, err := cmd.Uint64()
		if err != nil {
			return fmt.Errorf("error getting memory usage for key '%s': %v", key, err)
		}
		if memoryUsage != sizes[i] {
			fmt.Printf("Key '%s' - Size mismatch! MEMORY USAGE: %d, Expected Size: %d\n", key, memoryUsage, sizes[i])
		}
	}

	return nil
}
*/
// scanKeysFromNode scans keys from a specific node in the cluster
func scanKeysFromNode(nodeAddr, role string) error {
	parts := strings.Split(nodeAddr, "@")
	nodeAddr = parts[0]
	fmt.Printf("Scanning keys from node: %s (%s)\n", nodeAddr, role)
	rdb, err := newRedisClient(nodeAddr)
	if err != nil {
		return fmt.Errorf("%s", err)
	}
	var sampled, totalKeys, totalKeyLen uint64
	var keys []string
	var cursor uint64 = 0
	var biggestKey uint64
	biggestKeys := make([]KeySize, 0, *topN)
	totalKeys, err = rdb.DBSize(ctx).Uint64()
	if err != nil {
		return fmt.Errorf("Error getting DB size: %v", err)
	}
	// Repeat scan process for each node
	for {
		pct := 100 * float64(sampled) / float64(totalKeys)
		keys, cursor, err = scanKeys(rdb, cursor)
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			break
		}

		sizes, err := getKeySizes(rdb, keys, []string{}, true, *samples)

		if err != nil {
			return err
		}
		// compareKeySizes(rdb, keys, sizes, 5)
		for i, key := range keys {
			sampled++
			totalKeyLen += sizes[i]
			if sizes[i] > biggestKey {
				biggestKey = sizes[i]
				types, err := getKeyTypes(rdb, []string{key})
				if err != nil {
					return err
				}
				elementNum, err := getKeySizes(rdb, []string{key}, types, false, 0)
				if err != nil {
					return err
				}
				logMessage := fmt.Sprintf("%s [%05.2f%%] Biggest key found so far '%s' with type: %s, size: %s, %d %s\n",
					nodeAddr, pct, key, types[0], bytesToHuman(sizes[i]), elementNum[0], typeInfoMap[types[0]].SizeUnit)
				writeToLogFile(logWriter, logMessage)
			}
			if sampled%100000 == 0 {
				logMessage := fmt.Sprintf("%s [%05.2f%%] Sampled %d keys so far\n", nodeAddr, pct, sampled)
				writeToLogFile(logWriter, logMessage)
			}
			biggestKeys = append(biggestKeys, KeySize{Key: key,
				Size: sizes[i]})
		}

		if len(biggestKeys) > *topN {
			sort.Sort(BySize(biggestKeys))
			biggestKeys = biggestKeys[:*topN]
		}

		if *sleepDuration > 0 {
			time.Sleep(time.Duration(*sleepDuration * float64(time.Second)))
		}

		if cursor == 0 {
			logMessage := fmt.Sprintf("%s [%05.2f%%] Sampled a total of %d keys\n", nodeAddr, 100.00, sampled)
			writeToLogFile(logWriter, logMessage)
			break
		}
	}
	// Final sort and print summary
	sort.Sort(BySize(biggestKeys)) // Final sorting
	keyInfoMap, err := getKeyInfoMap(rdb, biggestKeys)
	if err != nil {
		return err
	}
	printMutex.Lock()
	defer printMutex.Unlock()
	printSummary(nodeAddr, sampled, totalKeyLen, totalKeys, biggestKeys, keyInfoMap)

	return nil
}

// getNonClusterNodes retrieves nodes in non-cluster mode
func getNonClusterNodes(addrs []string) (map[string]string, error) {
	nodes := make(map[string]string)

	for _, addr := range addrs {
		rdb, err := newRedisClient(addr)
		if err != nil {
			return nil, fmt.Errorf("%v", err)
		}

		_, majorVersion, err := getRedisVersion(rdb)
		if err != nil {
			return nil, fmt.Errorf("failed to get Redis version for node %s: %v", addr, err)
		}
		if majorVersion < 4 {
			return nil, fmt.Errorf("node %s has Redis version < 4.0, which is not supported", addr)
		}

		nodeInfo, err := rdb.Info(ctx, "replication").Result()
		if err != nil {
			return nil, fmt.Errorf("failed to fetch replication info for node %s: %v", addr, err)
		}

		var masterNode, slaveNode string
		slaveInfoRegexp := regexp.MustCompile(`slave\d+:ip=.*`)
		for _, line := range strings.Split(nodeInfo, "\n") {
			if strings.HasPrefix(line, "role:master") {
				masterNode = addr
				if *directFlag {
					nodes[masterNode] = "master"
					break
				}
			} else if strings.HasPrefix(line, "role:slave") {
				slaveNode = addr
				if *directFlag {
					nodes[slaveNode] = "slave"
					break
				}
			} else if slaveInfoRegexp.MatchString(line) && strings.Contains(line, "online") {
				slaveInfo := strings.Split(line, ":")
				s1 := slaveInfo[1]
				slaveInfo = strings.Split(s1, ",")
				var host, port string
				for _, item := range slaveInfo {
					if strings.HasPrefix(item, "ip=") {
						host = strings.Split(item, "=")[1]
					}
					if strings.HasPrefix(item, "port=") {
						port = strings.Split(item, "=")[1]
					}
				}
				slaveNode = host + ":" + port
				break
			}
		}
		if !*directFlag {
			if slaveNode != "" {
				nodes[slaveNode] = "slave"
			} else if masterNode != "" {
				nodes[masterNode] = "master"
			}
		}
	}

	return nodes, nil
}

// getClusterNodes retrieves nodes in cluster mode
func getClusterNodes(addr string) (map[string]string, error) {
	rdb, err := newRedisClient(addr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to cluster node %s: %v", addr, err)
	}

	_, majorVersion, err := getRedisVersion(rdb)
	if err != nil {
		return nil, fmt.Errorf("failed to get Redis version for cluster node %s: %v", addr, err)
	}
	if majorVersion < 4 {
		return nil, fmt.Errorf("cluster node %s has Redis version < 4.0, which is not supported", addr)
	}

	nodes := make(map[string]string)
	nodesInfo, err := rdb.ClusterNodes(ctx).Result()
	if err != nil {
		if err.Error() == "ERR This instance has cluster support disabled" {
			addrs := strings.Split(addr, ",")
			nodes, err = getNonClusterNodes(addrs)
			return nodes, err

		}
		return nil, fmt.Errorf("failed to fetch cluster nodes: %v", err)
	}

	var slaveNodes []NodeInfo
	var masterNodes []NodeInfo

	for _, line := range strings.Split(nodesInfo, "\n") {
		parts := strings.Fields(line)
		if len(parts) >= 8 {
			nodeID := parts[0]
			address := parts[1]
			role := parts[2]
			masterID := parts[3] // master ID of the slave node
			if strings.Contains(role, "slave") {
				slaveNodes = append(slaveNodes, NodeInfo{Address: address, MasterID: masterID})
			} else if strings.Contains(role, "master") {
				masterNodes = append(masterNodes, NodeInfo{Address: address, ID: nodeID})
			}
		}
	}

	for _, master := range masterNodes {
		foundSlave := false
		for _, slave := range slaveNodes {
			if slave.MasterID == master.ID {
				foundSlave = true
				nodes[slave.Address] = "slave"
				break
			}
		}
		if !foundSlave {
			nodes[master.Address] = "master"
		}
	}

	return nodes, nil
}

// findBigKeys scans the cluster nodes in parallel for the biggest keys
func findBigKeys(addr string) error {
	var nodes map[string]string
	var err error
	if *clusterFlag {
		nodes, err = getClusterNodes(addr)
	} else {
		addrs := strings.Split(addr, ",")
		nodes, err = getNonClusterNodes(addrs)
	}

	if err != nil {
		return fmt.Errorf("%v", err)
	}

	var masterNodes []string
	for nodeAddr, role := range nodes {
		if role == "master" {
			masterNodes = append(masterNodes, nodeAddr)
		}
	}

	if len(masterNodes) > 0 {
		masterNodesStr := strings.Join(masterNodes, ", ")

		if !*masterYes {
			return fmt.Errorf("Error: nodes %s are master. To execute, you must specify --master-yes", masterNodesStr)
		}

		var lazyfreeNoNodes []string
		for _, nodeAddr := range masterNodes {
			rdb, err := newRedisClient(nodeAddr)
			if err != nil {
				return fmt.Errorf("failed to connect to master node %s: %v", nodeAddr, err)
			}
			
			if *skipLazyfreeCheck {
		                log.Printf("Skipping lazyfree-lazy-expire check for node %s", nodeAddr)
		                continue
	                }
			
			lazyfreeConfig, err := rdb.ConfigGet(ctx, "lazyfree-lazy-expire").Result()
			if err != nil {
				return fmt.Errorf("failed to get lazyfree-lazy-expire config for master node %s: %v", nodeAddr, err)
			}

			lazyfreeValue := lazyfreeConfig["lazyfree-lazy-expire"]
			if lazyfreeValue == "no" {
				lazyfreeNoNodes = append(lazyfreeNoNodes, nodeAddr)
			}
		}

		if len(lazyfreeNoNodes) > 0 && !*skipLazyfreeCheck {
			lazyfreeNoNodesStr := strings.Join(lazyfreeNoNodes, ", ")
			return fmt.Errorf("Error: nodes %s are master and lazyfree-lazy-expire is set to 'no'. "+
				"Scanning might trigger large key expiration, which could block the main thread. "+
				"Please set lazyfree-lazy-expire to 'yes' for better performance. "+
				"To skip this check, you must specify --skip-lazyfree-check", lazyfreeNoNodesStr)
		}
	}

	logWriter, err := InitLogFile()
	if err != nil {
		return fmt.Errorf("Error initializing log file: %v\n", err)
	}
	defer logWriter.Close()

	var wg sync.WaitGroup
	sem := make(chan struct{}, *concurrency) // Semaphore for controlling concurrency
	errs := make(chan error, len(nodes))

	// Parallel scan across nodes
	for nodeAddr, role := range nodes {
		sem <- struct{}{} // Acquire a slot
		wg.Add(1)
		go func(nodeAddr string, role string) {
			defer func() {
				<-sem
				wg.Done()
			}()
			if err := scanKeysFromNode(nodeAddr, role); err != nil {
				errs <- err
			}
		}(nodeAddr, role)
	}

	// Wait for all goroutines to finish
	go func() {
		wg.Wait()
		close(errs)
	}()

	var combinedErr error
	for err := range errs {
		if combinedErr == nil {
			combinedErr = err
		} else {
			combinedErr = fmt.Errorf("%v; %w", combinedErr, err)
		}
	}

	return combinedErr
}

func main() {
	flag.Parse()

	// Check if addr is provided
	if *addr == "" {
		log.Fatalf("Error: Redis server address must be provided.")
	}

	if *clusterFlag && *directFlag {
		log.Fatalf("-cluster-mode and -direct cannot be specified at the same time")
	}

	if *clusterFlag {
		if strings.Contains(*addr, ",") {
			log.Fatalf("when -cluster-mode is specified, addr must be a single address")
		}
	}

	addresses := strings.Split(*addr, ",")
	for _, address := range addresses {
		parts := strings.Split(address, ":")
		if len(parts) != 2 || parts[1] == "" {
			log.Fatal("Error: Redis server address must be in the format <hostname>:<port>")
		}
	}

	if err := findBigKeys(*addr); err != nil {
		// log.Fatalf("%v\n", err)
		for _, e := range strings.Split(err.Error(), ";") {
			log.Println(" -", strings.TrimSpace(e))
		}
	}
}
