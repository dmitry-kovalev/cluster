package main

import (
	"github.com/dmitry-kovalev/cluster"
	"net/http"
	"os"
	"strings"

	nested "github.com/antonfisher/nested-logrus-formatter"
	"github.com/sirupsen/logrus"
)

func main() {
	log := logrus.New()
	log.SetFormatter(&nested.Formatter{
		HideKeys:        true,
		FieldsOrder:     []string{"component"},
		NoColors:        false,
		TimestampFormat: "15:04:05",
	})
	logEntry := logrus.NewEntry(log).WithFields(logrus.Fields{"component": "cluster"})

	addr := os.Getenv("ADDR")
	nodesList := os.Getenv("NODES")

	nodes := strings.Split(nodesList, ",")

	onLeader := func() error {
		log.Println("I'm leader")
		return nil
	}
	onFollower := func() error {
		log.Println("Me follower")
		return nil
	}

	cl, err := cluster.New(
		nodes,
		onLeader,
		onFollower,
		logEntry,
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
	log.Fatal(http.ListenAndServe(addr, nil))
}
