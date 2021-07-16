package cluster

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"
)

// AppendEntriesRequest represents AppendEntries requests. Replication logging is ignored.
type AppendEntriesRequest struct {
	Term     int    `json:"term"`
	LeaderID uint32 `json:"leader_id"`
	NodeID   uint32 `json:"node_id"`
}

// appendEntriesResponse represents the response to an appendEntries. In
// dinghy this always returns success.
type appendEntriesResponse struct {
	Term    int    `json:"term"`
	Success bool   `json:"success"`
	Reason  string `json:"reason,omitempty"`
	NodeID  uint32 `json:"node_id"`
}

// requestVoteRequest represents a requestVote sent by a candidate after an
// election timeout.
type requestVoteRequest struct {
	Term        int    `json:"term"`
	CandidateID uint32 `json:"candidate_id"`
	NodeID      uint32 `json:"node_id"`
}

// requestVoteResponse represents the response to a requestVote.
type requestVoteResponse struct {
	Term        int    `json:"term"`
	VoteGranted bool   `json:"vote_granted"`
	Reason      string `json:"reason,omitempty"`
	NodeID      uint32 `json:"node_id"`
}

// RequestVoteRequest will broadcast a request for votes in order to update node to
// either a follower or leader. If this candidate becomes leader error
// will return nil. The latest known term is always
// returned (this could be a newer term from another peer).
func (c *Cluster) RequestVoteRequest() (int, error) {
	if c.State.State() == StateLeader {
		// no op
		c.logger.Infof("Already leader")
		return c.State.Term(), nil
	}

	rvr := requestVoteRequest{
		Term:        c.State.Term(),
		CandidateID: c.State.ID(),
		NodeID:      c.State.ID(),
	}
	method := "POST"
	route := c.routePrefix + RouteRequestVote
	body, err := json.Marshal(rvr)
	if err != nil {
		c.logger.Errorf("could not create payload %v %v %v", method, route, string(body))
		return c.State.Term(), err
	}
	responses := c.BroadcastRequest(c.Nodes, method, route, body, 0)
	defer func() {
		for _, resp := range responses {
			if resp == nil {
				continue
			}
			resp.Body.Close()
		}
	}()
	ballots := make(map[uint32]bool)
LOOP:
	for i, resp := range responses {
		if resp == nil {
			// peer failed
			continue LOOP
		}
		var rvr requestVoteResponse
		if err := json.NewDecoder(resp.Body).Decode(&rvr); err != nil {
			c.logger.Errorf("Peer request returned invalid nodes %v because of %v", c.Nodes[i], err)
			continue LOOP
		}
		if rvr.Term > c.State.Term() {
			// step down to a follower with the newer term
			c.logger.Infof("Newer election term found %+v %s", rvr, c.State)
			return rvr.Term, ErrNewElectionTerm
		}
		if _, ok := ballots[rvr.NodeID]; ok {
			c.logger.Infof("Node %v already voted, skipping %+v", rvr.NodeID, rvr)
		} else {
			ballots[rvr.NodeID] = rvr.VoteGranted
		}
	}
	votes := 0
	for _, vote := range ballots {
		if vote {
			votes++
		}
	}

	// if votes < (len(c.Nodes))/2 {
	if votes < (len(ballots))/2 {
		c.logger.Infof("Too few votes %d but need more than %d", votes, (len(ballots))/2) // (len(c.Nodes))/2)
		return c.State.Term(), ErrTooFewVotes
	}

	c.logger.Infof("Election won with %d votes, becoming leader %s", votes, c.State)
	return c.State.Term(), nil
}

// AppendEntriesRequest will broadcast an AppendEntries request to peers.
// In the raft protocol this deals with appending and processing the
// replication log, however for leader election this is unused.
// It returns the current term with any errors.
func (c *Cluster) AppendEntriesRequest() (int, error) {
	aer := AppendEntriesRequest{
		Term:     c.State.Term(),
		LeaderID: c.State.ID(),
		NodeID:   c.State.ID(),
	}
	method := "POST"
	route := c.routePrefix + RouteAppendEntries
	body, err := json.Marshal(aer)
	if err != nil {
		c.logger.Errorf("could not create payload %v %v %v", method, route, string(body))
		return c.State.Term(), err
	}
	responses := c.BroadcastRequest(c.Nodes, method, route, body, c.State.heartbeatTimeoutMS/2)
	defer func() {
		for _, resp := range responses {
			if resp == nil {
				continue
			}
			resp.Body.Close()
		}
	}()
	for i, resp := range responses {
		if resp == nil {
			// peer failed
			continue
		}
		var aer appendEntriesResponse
		if err := json.NewDecoder(resp.Body).Decode(&aer); err != nil {
			c.logger.Errorf("peer request returned invalid nodes %v because of %v", c.Nodes[i], err)
			continue
		}
		if aer.Term > c.State.Term() {
			c.logger.Infof("newer election term found %+v", aer)
			return aer.Term, ErrNewElectionTerm
		}
	}

	return c.State.Term(), nil
}

// BroadcastRequest will send a request to all other nodes in the system.
func (c *Cluster) BroadcastRequest(peers []string, method, route string, body []byte, timeoutMS int) []*http.Response {
	responses := make([]*http.Response, len(peers))
	wg := &sync.WaitGroup{}
	for ind, peer := range peers {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()

			url := p
			if !strings.HasPrefix(url, "http") {
				url = "http://" + url + route
			}
			req, err := http.NewRequest(method, url, bytes.NewBuffer(body))
			if err != nil {
				c.logger.Errorf("could not create request %v %v %v", method, url, string(body))
				return
			}
			if timeoutMS > 0 {
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutMS)*time.Millisecond)
				defer cancel()
				req = req.WithContext(ctx)
			}
			resp, err := c.client.Do(req)
			if err != nil {
				c.logger.Debugf("Failed request %v %v %v (%v)", method, url, err, string(body))
				return
			}
			responses[i] = resp
		}(ind, peer)
	}
	wg.Wait()
	return responses
}
