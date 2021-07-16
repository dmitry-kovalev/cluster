# cluster  

Reworked version of [![Dinghy](https://godoc.org/github.com/upsight/dinghy)](http://godoc.org/github.com/upsight/dinghy)

Cluster implements leader election using part of the raft protocol. It might
be useful if you have several workers but only want one of them at a time
doing things.

```
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/dmitry-kovalev/cluster"
)

func main() {
	addr := flag.String("addr", "localhost:8899", "The address to listen on.")
	nodesList := flag.String("nodes", "localhost:8899,localhost:8898,localhost:8897", "Comma separated list of host:port")
	flag.Parse()

	nodes := strings.Split(*nodesList, ",")

	onLeader := func() error {
		fmt.Println("leader")
		return nil
	}
	onFollower := func() error {
		fmt.Println("me follower")
		return nil
	}

	cl, err := cluster.New(
		nodes,
		onLeader,
		onFollower,
		&cluster.LogLogger{Logger: log.New(os.Stderr, "logger: ", log.Lshortfile)},
		cluster.DefaultElectionTickRange,
		cluster.DefaultHeartbeatTickRange,
	)
	if err != nil {
		log.Fatal(err)
	}
	for _, route := range cl.Routes() {
		http.HandleFunc(route.Path, route.Handler)
	}
	go func() {
		if err := cl.Start(); err != nil {
			log.Fatal(err)
		}
	}()
	log.Fatal(http.ListenAndServe(*addr, nil))
}
```

You can test cluster with several nodes using docker-compose
```shell
$ export LOCAL_IP=192.168.1.138
$ docker compose up
[+] Running 4/4
 ⠿ Network cluster_default    Created                                                                                                                                                              3.8s
 ⠿ Container cluster_node3_1  Created                                                                                                                                                              0.1s
 ⠿ Container cluster_node1_1  Created                                                                                                                                                              0.1s
 ⠿ Container cluster_node2_1  Created                                                                                                                                                              0.1s
Attaching to node1_1, node2_1, node3_1
node2_1  | 14:19:40 [INFO] [cluster] Creating cluster node with nodes: [192.168.1.138:8899 192.168.1.138:8898 192.168.1.138:8897]
node2_1  | 14:19:40 [INFO] [cluster] Starting cluster node
node2_1  | 14:19:40 [INFO] [cluster] Current state is {"id":3626187060,"leader_id":0,"state":"follower","term":1,"voted_for":0}
node2_1  | 14:19:40 [INFO] [cluster] Entering follower state, leader id 0
node2_1  | 14:19:40 [INFO] Me follower
node3_1  | 14:19:41 [INFO] [cluster] Creating cluster node with nodes: [192.168.1.138:8899 192.168.1.138:8898 192.168.1.138:8897]
node3_1  | 14:19:41 [INFO] [cluster] Starting cluster node
node3_1  | 14:19:41 [INFO] [cluster] Current state is {"id":3776595137,"leader_id":0,"state":"follower","term":1,"voted_for":0}
node3_1  | 14:19:41 [INFO] [cluster] Entering follower state, leader id 0
node3_1  | 14:19:41 [INFO] Me follower
node2_1  | 14:19:42 [INFO] [cluster] Follower heartbeat timeout, transitioning to candidate
node2_1  | 14:19:42 [INFO] [cluster] Current state is {"id":3626187060,"leader_id":0,"state":"candidate","term":2,"voted_for":0}
node2_1  | 14:19:42 [INFO] [cluster] Entering candidate state
node2_1  | 14:19:42 [INFO] [cluster] Requesting vote
node2_1  | 14:19:42 [INFO] [cluster] Got RequestVote request, voting for candidate id 3626187060 {"id":3626187060,"leader_id":0,"state":"candidate","term":2,"voted_for":0}
node3_1  | 14:19:42 [INFO] [cluster] got RequestVote request from newer term, stepping down {Term:2 CandidateID:3626187060 NodeID:3626187060} {"id":3776595137,"leader_id":0,"state":"follower","term":1,"voted_for":0}
node3_1  | 14:19:42 [INFO] [cluster] Got RequestVote request, voting for candidate id 3626187060 {"id":3776595137,"leader_id":0,"state":"follower","term":2,"voted_for":0}
node2_1  | 14:19:42 [INFO] [cluster] Election won with 2 votes, becoming leader {"id":3626187060,"leader_id":0,"state":"candidate","term":2,"voted_for":3626187060}
node2_1  | 14:19:42 [INFO] [cluster] Candidate state changed to leader
node2_1  | 14:19:42 [INFO] [cluster] Current state is {"id":3626187060,"leader_id":3626187060,"state":"leader","term":2,"voted_for":3626187060}
node2_1  | 14:19:42 [INFO] [cluster] Entering leader state
node2_1  | 14:19:42 [INFO] I'm leader
node1_1  | 14:19:43 [INFO] [cluster] Creating cluster node with nodes: [192.168.1.138:8899 192.168.1.138:8898 192.168.1.138:8897]
node1_1  | 14:19:43 [INFO] [cluster] Starting cluster node
node1_1  | 14:19:43 [INFO] [cluster] Current state is {"id":3070019158,"leader_id":0,"state":"follower","term":1,"voted_for":0}
node1_1  | 14:19:43 [INFO] [cluster] Entering follower state, leader id 0
node1_1  | 14:19:43 [INFO] Me follower
node1_1  | 14:19:43 [INFO] [cluster] Got AppendEntries request from newer term, stepping down {Term:2 LeaderID:3626187060 NodeID:3626187060} {"id":3070019158,"leader_id":0,"state":"follower","term":1,"voted_for":0}

```
