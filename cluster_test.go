package cluster

import (
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"
)

// assert fails the test if the condition is false.
func assert(tb testing.TB, condition bool, msg string, v ...interface{}) {
	if !condition {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d: "+msg+"\033[39m\n\n", append([]interface{}{filepath.Base(file), line}, v...)...)
		tb.FailNow()
	}
}

// ok fails the test if an err is not nil.
func ok(tb testing.TB, err error) {
	if err != nil {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d: unexpected error: %s\033[39m\n\n", filepath.Base(file), line, err.Error())
		tb.FailNow()
	}
}

// equals fails the test if exp is not equal to act.
func equals(tb testing.TB, exp, act interface{}) {
	if !reflect.DeepEqual(exp, act) {
		_, file, line, _ := runtime.Caller(1)
		fmt.Printf("\033[31m%s:%d:\n\n\texp: %#v\n\n\tgot: %#v\033[39m\n\n", filepath.Base(file), line, exp, act)
		tb.FailNow()
	}
}

func newCluster(t testing.TB) *Cluster {
	l := log.New(ioutil.Discard, "", log.Ldate|log.Ltime|log.Lshortfile)
	ll := &LogLogger{l}
	cl, err := New(
		[]string{"localhost:9999", "localhost:9998"},
		DefaultOnLeader,
		DefaultOnFollower,
		ll,
		10,
		10,
	)
	ok(t, err)
	return cl
}

func TestNew(t *testing.T) {
	type args struct {
		addr       string
		nodes      []string
		onLeader   func() error
		onFollower func() error
	}
	tests := []struct {
		name            string
		args            args
		want            *Cluster
		wantErr         bool
		wantLeaderErr   bool
		wantFollowerErr bool
	}{
		{
			"00 init defaults",
			args{
				addr:  "localhost:9999",
				nodes: []string{"localhost:9999", "localhost:9998"},
			},
			&Cluster{
				Nodes:      []string{"localhost:9999", "localhost:9998"},
				OnLeader:   DefaultOnLeader,
				OnFollower: DefaultOnFollower,
			},
			false,
			false,
			false,
		},
		{
			"01 event methods",
			args{
				addr:  "localhost:9999",
				nodes: []string{"localhost:9999", "localhost:9998"},
			},
			&Cluster{
				Nodes:      []string{"localhost:9999", "localhost:9998"},
				OnLeader:   func() error { return fmt.Errorf("") },
				OnFollower: func() error { return fmt.Errorf("") },
			},
			false,
			false,
			false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := log.New(ioutil.Discard, "", 0)
			ll := &LogLogger{l}
			got, err := New(tt.args.nodes, tt.args.onLeader, tt.args.onFollower, ll, 10, 10)
			if (err != nil) != tt.wantErr {
				t.Errorf("New() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			equals(t, got.Nodes, tt.want.Nodes)
			got.mu.Lock()
			defer got.mu.Unlock()
			err = got.OnLeader()
			if (err != nil) != tt.wantLeaderErr {
				t.Errorf("OnLeader() error = %v, wantErr %v", err, tt.wantLeaderErr)
				return
			}
			err = got.OnFollower()
			if (err != nil) != tt.wantFollowerErr {
				t.Errorf("OnFollower() error = %v, wantErr %v", err, tt.wantFollowerErr)
				return
			}
		})
	}
}

func TestCluster_StartStop(t *testing.T) {
	din := newCluster(t)
	go func() {
		err := din.Start()
		if err != nil {
			t.Fatal(err)
		}
	}()
	time.Sleep(time.Millisecond * 10)
	din.Stop()

	// Start with an invalid state
	din.State.State(99)
	err := din.Start()
	equals(t, "unknown state 99", err.Error())
}

func TestCluster_follower(t *testing.T) {
	// coverage only
	cl := newCluster(t)
	go cl.Start()
	go cl.follower()
	time.Sleep(time.Millisecond * 10)
	cl.Stop()

	// state change
	go cl.Start()
	go cl.follower()
	cl.State.State(StateCandidate)
	time.Sleep(time.Millisecond * 20)
	cl.Stop()

	// append entries
	cl.State.State(StateFollower)
	go cl.Start()
	go cl.follower()
	cl.State.AppendEntriesEvent(&AppendEntriesRequest{5, cl.State.ID(), cl.State.ID()})
	time.Sleep(time.Millisecond * 20)
	cl.Stop()

	// heartbeat timeout
	cl.State.State(StateFollower)
	go cl.Start()
	go cl.leader()
	time.Sleep(time.Millisecond * 10)
	cl.Stop()

	wantErr := fmt.Errorf("test")
	cl.mu.Lock()
	cl.OnFollower = func() error { return wantErr }
	cl.mu.Unlock()
	err := cl.Start()
	equals(t, wantErr, err)
}

func TestCluster_candidate(t *testing.T) {
	// coverage only
	cl := newCluster(t)
	go cl.Start()
	go cl.candidate()
	time.Sleep(time.Millisecond * 10)
	cl.Stop()

	// state change
	go cl.Start()
	go cl.leader()
	cl.State.State(StateFollower)
	time.Sleep(time.Millisecond * 20)
	cl.Stop()

	// election timeout
	cl.State.State(StateCandidate)
	go cl.Start()
	go cl.leader()
	time.Sleep(time.Millisecond * 10)
	cl.Stop()
}

func TestCluster_leader(t *testing.T) {
	// coverage only
	cl := newCluster(t)
	go cl.Start()
	go cl.leader()
	time.Sleep(time.Millisecond * 10)
	cl.Stop()

	// state change
	go cl.Start()
	go cl.leader()
	cl.State.State(StateFollower)
	time.Sleep(time.Millisecond * 20)
	cl.Stop()

	// heartbeat tick
	cl.State.State(StateLeader)
	go cl.Start()
	go cl.leader()
	time.Sleep(time.Millisecond * 10)
	cl.Stop()

	wantErr := fmt.Errorf("test")
	cl.mu.Lock()
	cl.OnLeader = func() error { return wantErr }
	cl.mu.Unlock()
	err := cl.leader()
	equals(t, wantErr, err)
	time.Sleep(time.Millisecond * 10)
	equals(t, StateFollower, cl.State.State())
}
