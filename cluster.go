package cluster

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

var (
	// DefaultOnLeader is a no op function to execute when a node becomes a leader.
	DefaultOnLeader = func() error { return nil }
	// DefaultOnFollower is a no op function to execute when a node becomes a follower.
	DefaultOnFollower = func() error { return nil }
	// DefaultRoutePrefix is what is prefixed for the cluster routes. (/cluster)
	DefaultRoutePrefix = "/cluster"
	// ErrTooFewVotes happens on a RequestVote when the candidate receives less than the
	// majority of votes.
	ErrTooFewVotes = errors.New("too few votes")
	// ErrNewElectionTerm if during RequestVote there is a higher term found.
	ErrNewElectionTerm = errors.New("newer election term")
	// ErrLeader is returned when an operation can't be completed on a
	// leader node.
	ErrLeader = errors.New("node is the leader")
	// ErrNotLeader is returned when an operation can't be completed on a
	// follower or candidate node.
	ErrNotLeader = errors.New("node is not the leader")
)

// Cluster manages the raft FSM and executes OnLeader and OnFollower events.
type Cluster struct {
	client *http.Client
	logger Logger
	mu     *sync.Mutex
	// routePrefix will be prefixed to all handler routes. This should start with /route.
	routePrefix string
	stopChan    chan struct{}
	// Nodes is a list of all nodes for consensus.
	Nodes []string
	// OnLeader is an optional function to execute when becoming a leader.
	OnLeader func() error
	// OnFollower is an optional function to execute when becoming a follower.
	OnFollower func() error
	// State for holding the raft state.
	State *State
}

// ApplyFunc is for on leader and on follower events.
type ApplyFunc func() error

// New initializes a new cluster. Start is required to be run to
// begin leader election.
func New(nodes []string, onLeader, onFollower ApplyFunc, l Logger, eMS, hMS int) (*Cluster, error) {
	l.Infof("Creating cluster node with nodes: %v", nodes)

	if onLeader == nil {
		onLeader = DefaultOnLeader
	}
	if onFollower == nil {
		onFollower = DefaultOnFollower
	}
	client := &http.Client{
		Timeout: time.Duration(hMS) * time.Millisecond,
	}

	id := rand.Uint32()
	mu := &sync.Mutex{}
	mu.Lock()
	c := &Cluster{
		client:      client,
		logger:      l,
		mu:          mu,
		routePrefix: DefaultRoutePrefix,
		stopChan:    make(chan struct{}),
		Nodes:       nodes,
		OnLeader:    onLeader,
		OnFollower:  onFollower,
		State:       NewState(id, eMS, hMS),
	}
	mu.Unlock()
	l.Debugf("Cluster: %+v", c)
	return c, nil
}

// Stop will stop any running event loop.
func (c *Cluster) Stop() error {
	c.logger.Infof("Stopping cluster node")
	// exit any state running and the main event fsm.
	for i := 0; i < 2; i++ {
		c.stopChan <- struct{}{}
	}
	return nil
}

// Start begins the leader election process.
func (c *Cluster) Start() error {
	c.logger.Infof("Starting cluster node")
	for {
		c.logger.Infof("Current state is %s", c.State)
		select {
		case <-c.stopChan:
			c.logger.Infof("Stopping cluster node")
			return nil
		default:
		}
		switch c.State.State() {
		case StateFollower:
			if err := c.follower(); err != nil {
				return err
			}
		case StateCandidate:
			c.candidate()
		case StateLeader:
			if err := c.leader(); err != nil {
				return err
			}
		default:
			c.logger.Errorf("Unknown state %d", c.State.State())
			return fmt.Errorf("unknown state %d", c.State.State())
		}
	}
}

// follower will wait for an AppendEntries from the leader and on expiration will begin
// the process of leader election with a RequestVote.
func (c *Cluster) follower() error {
	c.logger.Infof("Entering follower state, leader id %d", c.State.LeaderID())
	c.mu.Lock()
	if err := c.OnFollower(); err != nil {
		c.logger.Errorf("Can't execute OnFollower: %v", err)
		c.mu.Unlock()
		return err
	}
	c.mu.Unlock()
LOOP:
	for {
		select {
		case <-c.stopChan:
			return nil
		case newState := <-c.State.StateChanged():
			if newState == StateFollower {
				continue
			}
			c.logger.Infof("Follower state changed to %v", c.State.StateString(newState))
			return nil
		case <-c.State.HeartbeatReset():
			c.logger.Infof("Heartbeat reset")
			continue LOOP
		case aer := <-c.State.AppendEntriesEvent():
			c.logger.Debugf(" %v got AppendEntries from leader %v", c.State.StateString(c.State.State()), aer)
			continue LOOP
		case <-c.State.HeartbeatTickRandom():
			// https://raft.github.io/raft.pdf
			// If a follower receives no communication over a period of time
			// called the election timeout, then it assumes there is no viable
			// leader and begins an election to choose a new leader.
			// To begin an election, a follower increments its current
			// term and transitions to candidate state.
			c.logger.Infof("Follower heartbeat timeout, transitioning to candidate")
			c.State.VotedFor(NoVote)
			c.State.LeaderID(UnknownLeaderID)
			c.State.Term(c.State.Term() + 1)
			c.State.State(StateCandidate)
			return nil
		}
	}
}

// candidate is for when in StateCandidate. The loop will
// attempt an election repeatedly until it receives events.
// https://raft.github.io/raft.pdf
// A candidate continues in
// this state until one of three things happens: (a) it wins the
// election, (b) another server establishes itself as leader, or
// (c) a period of time goes by with no winner.
func (c *Cluster) candidate() {
	c.logger.Infof("Entering candidate state")
	go func() {
		c.logger.Infof("Requesting vote")
		currentTerm, err := c.RequestVoteRequest()
		if err != nil {
			c.logger.Errorf("Executing RequestVoteRequest: %v", err)
			switch err {
			case ErrNewElectionTerm:
				c.State.StepDown(currentTerm)
			case ErrTooFewVotes:
				c.State.State(StateFollower)
			}
			return
		}
		// it wins the election
		c.State.LeaderID(c.State.ID())
		c.State.State(StateLeader)
	}()
	for {
		select {
		case <-c.stopChan:
			return
		case aer := <-c.State.AppendEntriesEvent():
			// https://raft.github.io/raft.pdf
			// While waiting for votes, a candidate may receive an
			// AppendEntries RPC from another server claiming to be
			// leader. If the leader’s term (included in its RPC) is at least
			// as large as the candidate’s current term, then the candidate
			// recognizes the leader as legitimate and returns to follower
			// state. If the term in the RPC is smaller than the candidate’s
			// current term, then the candidate rejects the RPC and continues
			// in candidate state.
			c.logger.Infof("Candidate got an AppendEntries from a leader %v", aer)
			if aer.Term >= c.State.Term() {
				c.State.StepDown(aer.Term)
				return
			}
		case newState := <-c.State.StateChanged():
			if newState == StateCandidate {
				continue
			}
			c.logger.Infof("Candidate state changed to %s", c.State.StateString(newState))
			return
		case e := <-c.State.ElectionTick():
			c.logger.Infof("Election timeout, restarting election %v", e)
			return
		}
	}
}

// leader is for when in StateLeader. The loop will continually send
// a heartbeat of AppendEntries to all peers at a rate of HeartbeatTimeoutMS.
func (c *Cluster) leader() error {
	c.logger.Infof("Entering leader state")
	go c.AppendEntriesRequest()
	errChan := make(chan error)
	go func() {
		// Run the OnLeader event in a goroutine in case
		// it has a long delay. Any errors returned will exit the
		// leader state.
		c.mu.Lock()
		defer c.mu.Unlock()
		if err := c.OnLeader(); err != nil {
			c.logger.Errorf("Can't execute OnLeader: %v", err)
			errChan <- err
		}
	}()
	for {
		select {
		case err := <-errChan:
			c.State.State(StateFollower)
			go func() {
				// Removing the state change event here
				// before returning error.
				<-c.State.StateChanged()
			}()
			return err
		case <-c.stopChan:
			return nil
		case <-c.State.AppendEntriesEvent():
			// ignore any append entries to self.
			continue
		case newState := <-c.State.StateChanged():
			if newState == StateLeader {
				continue
			}
			c.logger.Infof("Leader state changed to %s", c.State.StateString(newState))
			return nil
		case h := <-c.State.HeartbeatTick():
			c.logger.Debugf("Sending to peers AppendEntriesRequest %v %v", c.Nodes, h)
			currentTerm, err := c.AppendEntriesRequest()
			if err != nil {
				c.logger.Errorf("Executing AppendEntriesRequest: %v", err)
				switch err {
				case ErrNewElectionTerm:
					c.State.StepDown(currentTerm)
					return nil
				}
			}
		}
	}
}
