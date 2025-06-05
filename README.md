# Redis Large Key Analysis Tool: Supports TOP N, Batch Analysis, and Slave Node Priority

# Background

Redis large key analysis tools are mainly divided into two categories:

1. Offline Analysis

   Parsing is based on the RDB file, and the commonly used tool is redis-rdb-tools (https://github.com/sripathikrishnan/redis-rdb-tools).
   However, this tool has not been updated for nearly 5 years, does not support Redis 7, and because it is developed using Python, the parsing speed is slow.
   The currently more active alternative tool is https://github.com/HDT3213/rdb. This tool supports Redis 7 and is developed using Go.

2. Online Analysis

   The commonly used tool is redis-cli, which provides two analysis methods:

   - \- -bigkeys: Introduced in Redis 3.0.0, it counts the number of elements in the key.
   - \- -memkeys: Introduced in Redis 6.0.0, it uses the `MEMORY USAGE` command to count the memory occupation of the key.

The advantages and disadvantages of these two methods are as follows:

- Offline analysis: Parsing is based on the RDB file and will not affect the performance of online instance. The disadvantage is that the operation is relatively complex. Especially for many Redis cloud services, since the SYNC command is disabled, the RDB file cannot be directly downloaded through `redis-cli --rdb <filename>`, and it can only be manually downloaded from the console.
- Online analysis: The operation is simple. As long as there is access permission to the instance, the analysis can be directly carried out. The disadvantage is that the analysis process may have a certain impact on the performance of the online instance.

The tool introduced in this article (`redis-find-big-key`) is also an online analysis tool. Its implementation idea is similar to `redis-cli --memkeys`, but it is more powerful and practical. It is mainly reflected in:

1. Supports the TOP N function
   This tool can output the top N keys with the most memory occupation, while redis-cli can only output the single key with the most occupation in each type.
2. Supports batch analysis
   This tool can analyze multiple Redis nodes at the same time. Especially for Redis Cluster, after enabling the cluster mode (`-cluster-mode`), it will automatically analyze each shard. While redis-cli can only analyze a single node.
3. Automatically selects the slave node for analysis
   In order to reduce the impact on the instance performance, the tool will automatically select the slave node for analysis. Only when there is no slave node will the master node be selected for analysis. While redis-cli can only analyze the master node.

# Test Time Comparison

Test environment: Redis 6.2.17, single instance, used_memory_human is 9.75G, the number of keys is 1 millon, and the RDB file size is 3GB.
The following is the time-consuming situation of the above four tools when obtaining the 100 keys with the most memory occupation:

# Tool Effect

```c++
# ./redis-find-big-key -addr 10.0.1.76:6379 -cluster-mode
Log file not specified, using default: /tmp/10.0.1.76:6379_20250222_043832.txt
Scanning keys from node: 10.0.1.76:6380 (slave)
Node: 10.0.1.76:6380
-------- Summary --------
Sampled 8 keys in the keyspace!
Total key length in bytes is 2.96 MB (avg len 379.43 KB)
Top biggest keys:
+------------------------------+--------+-----------+---------------------+
|             Key              |  Type  |   Size    | Number of elements  |
+------------------------------+--------+-----------+---------------------+
| mysortedset_20250222043729:1 |  zset  | 739.6 KB  |    8027 members     |
|   myhash_20250222043741:2    |  hash  | 648.12 KB |     9490 fields     |
| mysortedset_20250222043741:1 |  zset  | 536.44 KB |    5608 members     |
|    myset_20250222043729:1    |  set   | 399.66 KB |    8027 members     |
|    myset_20250222043741:1    |  set   | 328.36 KB |    5608 members     |
|   myhash_20250222043729:2    |  hash  | 222.65 KB |     3917 fields     |
|   mylist_20250222043729:1    |  list  | 160.54 KB |     8027 items      |
|    mykey_20250222043729:2    | string | 73 bytes  | 7 bytes (value len) |
+------------------------------+--------+-----------+---------------------+
Scanning keys from node: 10.0.1.202:6380 (slave)
Node: 10.0.1.202:6380
-------- Summary --------
Sampled 8 keys in the keyspace!
Total key length in bytes is 3.11 MB (avg len 398.23 KB)
Top biggest keys:
+------------------------------+--------+------------+---------------------+
|             Key              |  Type  |    Size    | Number of elements  |
+------------------------------+--------+------------+---------------------+
| mysortedset_20250222043741:2 |  zset  | 1020.13 KB |    9490 members     |
|    myset_20250222043741:2    |  set   | 588.81 KB  |    9490 members     |
|   myhash_20250222043729:1    |  hash  |  456.1 KB  |     8027 fields     |
| mysortedset_20250222043729:2 |  zset  |  404.5 KB  |    3917 members     |
|   myhash_20250222043741:1    |  hash  | 335.79 KB  |     5608 fields     |
|    myset_20250222043729:2    |  set   | 195.87 KB  |    3917 members     |
|   mylist_20250222043741:2    |  list  | 184.55 KB  |     9490 items      |
|    mykey_20250222043741:1    | string |  73 bytes  | 7 bytes (value len) |
+------------------------------+--------+------------+---------------------+
Scanning keys from node: 10.0.1.147:6380 (slave)
Node: 10.0.1.147:6380
-------- Summary --------
Sampled 4 keys in the keyspace!
Total key length in bytes is 192.9 KB (avg len 48.22 KB)
Top biggest keys:
+-------------------------+--------+-----------+---------------------+
|           Key           |  Type  |   Size    | Number of elements  |
+-------------------------+--------+-----------+---------------------+
| mylist_20250222043741:1 |  list  | 112.45 KB |     5608 items      |
| mylist_20250222043729:2 |  list  | 80.31 KB  |     3917 items      |
| mykey_20250222043729:1  | string | 73 bytes  | 7 bytes (value len) |
| mykey_20250222043741:2  | string | 73 bytes  | 7 bytes (value len) |
+-------------------------+--------+-----------+---------------------+
```

# Tool Address

Project address: https://github.com/slowtech/redis-find-big-key
You can directly download the binary package or compile the source code.

## Directly Download the Binary Package

```bash
# wget https://github.com/slowtech/redis-find-big-key/releases/download/v1.0.0/redis-find-big-key-linux-amd64.tar.gz
# tar xvf redis-find-big-key-linux-amd64.tar.gz
```

After decompression, an executable file named `redis-find-big-key` will be generated in the current directory.

## Build from Source Code

```bash
# wget https://github.com/slowtech/redis-find-big-key/archive/refs/tags/v1.0.0.tar.gz
# tar xvf v1.0.0.tar.gz 
# cd redis-find-big-key-1.0.0
# go build
```

After compilation, an executable file named `redis-find-big-key` will be generated in the current directory.

# Parameter Parsing

```bash
# ./redis-find-big-key --help
Usage of ./redis-find-big-key:
  -addr string
        Redis server address in the format <hostname>:<port>
  -cluster-mode
        Enable cluster mode to get keys from all shards in the Redis cluster
  -concurrency int
        Maximum number of nodes to process concurrently (default 1)
  -direct
        Perform operation on the specified node. If not specified, the operation will default to executing on the slave node
  -log-file string
        Log file for saving progress and intermediate result
  -master-yes
        Execute even if the Redis role is master
  -password string
        Redis password
  -samples uint
        Samples for memory usage (default 5)
  -skip-lazyfree-check
        Skip check lazyfree-lazy-expire
  -sleep float
        Sleep duration (in seconds) after processing each batch
  -tls
        Enable TLS for Redis connection
  -top int
        Maximum number of biggest keys to display (default 100)
```

The specific meanings of each parameter are as follows:

- -addr: Specify the address of the Redis instance in the format of `<hostname>:<port>`, for example, 10.0.0.108:6379. Note, If the cluster mode (`-cluster-mode`) is not enabled, multiple addresses can be specified, separated by commas, for example, 10.0.0.108:6379,10.0.0.108:6380. If the cluster mode is enabled, only one address can be specified, and the tool will automatically discover other nodes in the cluster.

- -cluster-mode: Enable the cluster mode. The tool will automatically analyze each shard in the Redis Cluster and preferentially select the slave node. Only when there is no slave node in the corresponding shard will the master node be selected for analysis.

- -concurrency: Set the concurrency, with a default value of 1, that is, analyze the nodes one by one. If there are many nodes to be analyzed, increasing the concurrency can improve the analysis speed.

- -direct: Directly perform the analysis on the node specified by -addr, which will skip the default logic of automatically selecting the slave node.

- -log-file: Specify the path of the log file, which is used to record the progress information and intermediate process information during the analysis process. If not specified, the default is `/tmp/<firstNode>_<timestamp>.txt`, for example, /tmp/10.0.0.108:6379_20250218_125955.txt.

- -master-yes: If there is a master node among the nodes to be analyzed (common reasons: the slave node does not exist; specify to analyze on the master node through the -direct parameter), the tool will prompt the following error: 

  ```
  Error: nodes 10.0.1.76:6379 are master. To execute, you must specify -master-yes
  ```

  If it is determined that the analysis can be carried out on the master node, you can specify -master-yes to skip the detection.

- -password: Specify the password of the Redis instance.

- -samples: Set the sampling number in the `MEMORY USAGE key [SAMPLES count]` command. For data structures with multiple elements (such as LIST, SET, ZSET, HASH, STREAM, etc.), a too low sampling number may lead to inaccurate estimation of memory occupation, while a too high number will increase the calculation time and resource consumption. If SAMPLES is not specified, the default value is 5.

- -skip-lazyfree-check: If the analysis is carried out on the master node, special attention should be paid to the large expired keys. Because the scanning operation will trigger the deletion of expired keys. If lazy deletion (`lazyfree-lazy-expire`) is not enabled, the deletion operation will be executed in the main thread. At this time, deleting large keys may cause blocking and affect normal business requests.
  Therefore, when the tool analyzes on the master node, it will automatically check whether the node has enabled lazy deletion. If it is not enabled, the tool will prompt the following error and terminate the operation to avoid affecting the online business: 

  ```bash
  Error: nodes 10.0.1.76:6379 are master and lazyfree-lazy-expire is set to ‘no’. Scanning might trigger large key expiration, which could block the main thread. Please set lazyfree-lazy-expire to ‘yes’ for better performance. To skip this check, you must specify --skip-lazyfree-check
  ```

  In this case, it is recommended to enable lazy deletion by the command `CONFIG SET lazyfree-lazy-expire yes`*.*
  If it is confirmed that there are no large expired keys, you can specify -skip-lazyfree-check to skip the detection.

- -sleep: Set the sleep time after scanning each batch of data.

- -tls: Enable the TLS connection.

- -top: Display the top N keys with the most memory occupation. The default is 100.

# Common Usage

## Analyze a Single Node

```bash
./redis-find-big-key -addr 10.0.1.76:6379
Scanning keys from node: 10.0.1.202:6380 (slave)
```

Note that in the above example, the specified node is not the same as the actually scanned node. This is because 10.0.1.76:6379 is the master node, and the tool will default to selecting the slave library for analysis. Only when there is no slave library for the specified master node will the tool directly scan the master node.

## Analyze a Single Redis Cluster

```bash
./redis-find-big-key -addr 10.0.1.76:6379 -cluster-mode
```

Just provide the address of any node in the cluster, and the tool will automatically obtain the addresses of other nodes in the cluster. At the same time, the tool will preferentially select the slave node for analysis. Only when there is no slave node in a certain shard will the master node of that shard be selected for analysis.

## Analyze Multiple Nodes

```bash
./redis-find-big-key -addr 10.0.1.76:6379,10.0.1.202:6379,10.0.1.147:6379
```

The nodes are independent of each other and can come from the same cluster or different clusters. Note that if multiple node addresses are specified in the -addr parameter, the -cluster-mode parameter cannot be used.

## Analyze the Master Node

If you need to analyze the master node, you can specify the master node and use the `-direct` parameter.

```bash
./redis-find-big-key -addr 10.0.1.76:6379 -direct -master-yes
```

# Notes

1. This tool is only applicable to Redis version 4.0 and above, because `MEMORY USAGE` and `lazyfree-lazy-expire` are supported starting from Redis 4.0.

2. The size of the same key displayed by `redis-find-big-key` and `redis-cli` may differ. This is normal because `redis-find-big-key` defaults to analyzing slave nodes, showing the key size in the slave, while `redis-cli` can only analyze the master node, displaying the key size in the master. Consider the following example:

   ```bash
   # ./redis-find-big-key -addr 10.0.1.76:6379 -top 1
   Scanning keys from node: 10.0.1.202:6380 (slave)
   ...
   Top biggest keys:
   +------------------------------+------+------------+--------------------+
   |             Key              | Type |    Size    | Number of elements |
   +------------------------------+------+------------+--------------------+
   | mysortedset_20250222043741:2 | zset | 1020.13 KB |    9490 members    |
   +------------------------------+------+------------+--------------------+
   
   # redis-cli -h 10.0.1.76 -p 6379 -c MEMORY USAGE mysortedset_20250222043741:2
   (integer) 1014242
   # echo "scale=2; 1014242 / 1024" | bc
   990.47
   ```

   One shows 1020.13 KB, and the other 990.47 KB.

   If you directly check the key size in the master using `redis-find-big-key`, the result will match `redis-cli`:

   ```bash
   # ./redis-find-big-key -addr 10.0.1.76:6379 -direct --master-yes -top 1 --skip-lazyfree-check
   Scanning keys from node: 10.0.1.76:6379 (master)
   ...
   Top biggest keys:
   +------------------------------+------+-----------+--------------------+
   |             Key              | Type |   Size    | Number of elements |
   +------------------------------+------+-----------+--------------------+
   | mysortedset_20250222043741:2 | zset | 990.47 KB |    9490 members    |
   +------------------------------+------+-----------+--------------------+
   ```

   

# Implementation Principle

This tool is implemented by referring to `redis-cli --memkeys`.

In fact, both `redis-cli --bigkeys` and `redis-cli --memkeys` call the `findBigKeys` function with different parameters:

```c++
/* Find big keys */
if (config.bigkeys) {
    if (cliConnect(0) == REDIS_ERR) exit(1);
    findBigKeys(0, 0);
}

/* Find large keys */
if (config.memkeys) {
    if (cliConnect(0) == REDIS_ERR) exit(1);
    findBigKeys(1, config.memkeys_samples);
}
```

Next, let’s look at the specific implementation logic of this function:

```c++
static void findBigKeys(int memkeys, unsigned memkeys_samples) {
    ...
    // Get the total number of keys via the DBSIZE command
    total_keys = getDbSize();

    /* Status message */
    printf("\n# Scanning the entire keyspace to find biggest keys as well as\n");
    printf("# average sizes per key type.  You can use -i 0.1 to sleep 0.1 sec\n");
    printf("# per 100 SCAN commands (not usually needed).\n\n");

    /* SCAN loop */
    do {
        /* Calculate approximate percentage completion */
        pct = 100 * (double)sampled/total_keys;
      
        // Scan keys via the SCAN command
        reply = sendScan(&it);
        scan_loops++;
        // Get the key names of the current batch
        keys  = reply->element[1];
        ...
        // Use pipeline to batch send TYPE commands to get the type of each key
        getKeyTypes(types_dict, keys, types);
        // Use pipeline to batch send corresponding commands to get the size of each key
        getKeySizes(keys, types, sizes, memkeys, memkeys_samples);

        // Process each key and update statistics
        for(i=0;i<keys->elements;i++) {
            typeinfo *type = types[i];
            /* Skip keys that disappeared between SCAN and TYPE */
            if(!type)
                continue;

            type->totalsize += sizes[i]; // Accumulate the total size of keys of this type
            type->count++; // Count the number of keys of this type
            totlen += keys->element[i]->len; // Accumulate the key length
            sampled++; // Count the number of scanned keys
            // If the current key size exceeds the maximum of this type, update the maximum key size and print statistics
            if(type->biggest<sizes[i]) {
                if (type->biggest_key)
                    sdsfree(type->biggest_key);
                type->biggest_key = sdscatrepr(sdsempty(), keys->element[i]->str, keys->element[i]->len);
                ...
                printf(
                   "[%05.2f%%] Biggest %-6s found so far '%s' with %llu %s\n",
                   pct, type->name, type->biggest_key, sizes[i],
                   !memkeys? type->sizeunit: "bytes");

                type->biggest = sizes[i];
            }

            // Every 1 million keys scanned, output the current progress and the number of scanned keys
            if(sampled % 1000000 == 0) {
                printf("[%05.2f%%] Sampled %llu keys so far\n", pct, sampled);
            }
        }

        // If interval is set, sleep for a while every 100 SCAN commands
        if (config.interval && (scan_loops % 100) == 0) {
            usleep(config.interval);
        }

        freeReplyObject(reply);
    } while(force_cancel_loop == 0 && it != 0);
    .. 
    // Output overall statistics
    printf("\n-------- summary -------\n\n");
    if (force_cancel_loop) printf("[%05.2f%%] ", pct); // Show progress percentage if the loop was cancelled
    printf("Sampled %llu keys in the keyspace!\n", sampled); // Print the number of scanned keys
    printf("Total key length in bytes is %llu (avg len %.2f)\n\n",
       totlen, totlen ? (double)totlen/sampled : 0); // Print the total and average key name length

    // Output information about the largest key of each type
    di = dictGetIterator(types_dict);
    while ((de = dictNext(di))) {
        typeinfo *type = dictGetVal(de);
        if(type->biggest_key) {
            printf("Biggest %6s found '%s' has %llu %s\n", type->name, type->biggest_key,
               type->biggest, !memkeys? type->sizeunit: "bytes");
        } // type->name is the key type, type->biggest_key is the largest key name
    } // type->biggest is the size of the largest key, !memkeys? type->sizeunit: "bytes" is the size unit

    ..
    // Output statistics for each type
    di = dictGetIterator(types_dict);
    while ((de = dictNext(di))) {
        typeinfo *type = dictGetVal(de);
        printf("%llu %ss with %llu %s (%05.2f%% of keys, avg size %.2f)\n",
           type->count, type->name, type->totalsize, !memkeys? type->sizeunit: "bytes",
           sampled ? 100 * (double)type->count/sampled : 0,
           type->count ? (double)type->totalsize/type->count : 0);
    } // sampled ? 100 * (double)type->count/sampled : 0 is the percentage of keys of this type among all scanned keys
    ..
    exit(0);
}
```

The implementation logic of this function is as follows:

1. Use the DBSIZE command to get the total number of keys in the Redis database.
2. Use the SCAN command to batch scan keys and get the key names of the current batch.
3. Use pipeline to batch send TYPE commands to get the type of each key.
4. Use pipeline to batch send corresponding commands to get the size of each key: If `--bigkeys` is specified, use corresponding commands based on key type: STRLEN (string), LLEN (list), SCARD (set), HLEN (hash), ZCARD (zset), XLEN (stream). If `--memkeys` is specified, use the MEMORY USAGE command to get the memory usage of the key.
5. Process each key and update statistics: If a key’s size exceeds the maximum of its type, update the maximum and print relevant statistics.
6. Output summary information showing the largest key of each type and related statistics.
