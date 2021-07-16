package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
)

const (
	// RouteAppendEntries for append entries requests.
	RouteAppendEntries = "/appendentries"
	// RouteID for id requests.
	RouteID = "/id"
	// RouteRequestVote for request vote requests.
	RouteRequestVote = "/requestvote"
	// RouteStatus will render the current nodes full state.
	RouteStatus = "/status"
	// RouteStepDown to force a node to step down.
	RouteStepDown = "/stepdown"
)

var (
	emptyAppendEntriesResponse bytes.Buffer
	emptyRequestVoteResponse   bytes.Buffer
)

func init() {
	if err := json.NewEncoder(&emptyAppendEntriesResponse).Encode(appendEntriesResponse{}); err != nil {
		log.Fatalln("error encoding json:", err)
	}
	if err := json.NewEncoder(&emptyRequestVoteResponse).Encode(requestVoteResponse{}); err != nil {
		log.Fatalln("error encoding json:", err)
	}
}

// Route holds path and handler information.
type Route struct {
	Path    string
	Handler func(http.ResponseWriter, *http.Request)
}

// Routes create the routes required for leader election.
func (c *Cluster) Routes() []*Route {
	routes := []*Route{
		&Route{c.routePrefix + RouteID, c.IDHandler()},
		&Route{c.routePrefix + RouteRequestVote, c.RequestVoteHandler()},
		&Route{c.routePrefix + RouteAppendEntries, c.AppendEntriesHandler()},
		&Route{c.routePrefix + RouteStatus, c.StatusHandler()},
		&Route{c.routePrefix + RouteStepDown, c.StepDownHandler()},
	}
	return routes
}

// StepDownHandler (POST) will force the node to step down to a follower state.
func (c *Cluster) StepDownHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PUT" {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		c.State.State(StateFollower)
		fmt.Fprintln(w, c.State)
	}
}

// StatusHandler (GET) returns the nodes full state.
func (c *Cluster) StatusHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprintln(w, c.State)
	}
}

// IDHandler (GET) returns the nodes id.
func (c *Cluster) IDHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		fmt.Fprintln(w, c.State.ID())
	}
}

// RequestVoteHandler (POST) handles requests for votes
func (c *Cluster) RequestVoteHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}
		if r.Method != "POST" {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		var rv requestVoteRequest
		if err := json.NewDecoder(r.Body).Decode(&rv); err != nil {
			c.logger.Errorf("%s", err)
			http.Error(w, emptyRequestVoteResponse.String(), http.StatusBadRequest)
			return
		}

		switch {
		case rv.Term < c.State.Term():
			// The request is from an old term so reject
			c.logger.Infof("got RequestVote request from older term, rejecting %+v %s", rv, c.State)
			rvResp := requestVoteResponse{
				Term:        c.State.Term(),
				VoteGranted: false,
				Reason:      fmt.Sprintf("term %d < %d", rv.Term, c.State.Term()),
				NodeID:      c.State.ID(),
			}
			if c.State.State() == StateLeader {
				rvResp.Reason = "already leader"
			}
			if err := json.NewEncoder(w).Encode(rvResp); err != nil {
				http.Error(w, emptyRequestVoteResponse.String(), http.StatusInternalServerError)
			}
			return
		case (c.State.VotedFor() != NoVote) && (c.State.VotedFor() != rv.CandidateID):
			// don't double vote if already voted for a term
			c.logger.Infof("got RequestVote request but already voted, rejecting %+v %s", rv, c.State)
			rvResp := requestVoteResponse{
				Term:        c.State.Term(),
				VoteGranted: false,
				Reason:      fmt.Sprintf("already cast vote for %d", c.State.VotedFor()),
				NodeID:      c.State.ID(),
			}
			if err := json.NewEncoder(w).Encode(rvResp); err != nil {
				http.Error(w, emptyRequestVoteResponse.String(), http.StatusInternalServerError)
			}
			return
		case rv.Term > c.State.Term():
			// Step down and reset state on newer term.
			c.logger.Infof("got RequestVote request from newer term, stepping down %+v %s", rv, c.State)
			c.State.StepDown(rv.Term)
			c.State.LeaderID(UnknownLeaderID)
		}

		// ok and vote for candidate
		// reset election timeout
		c.logger.Infof("Got RequestVote request, voting for candidate id %d %s", rv.CandidateID, c.State)
		c.State.VotedFor(rv.CandidateID)
		// defer d.State.HeartbeatReset(true)
		rvResp := requestVoteResponse{
			Term:        c.State.Term(),
			VoteGranted: true,
			NodeID:      c.State.ID(),
		}
		if err := json.NewEncoder(w).Encode(rvResp); err != nil {
			http.Error(w, emptyRequestVoteResponse.String(), http.StatusInternalServerError)
		}
		return
	}
}

// AppendEntriesHandler (POST) handles append entry requests
func (c *Cluster) AppendEntriesHandler() func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Body != nil {
			defer r.Body.Close()
		}
		if r.Method != "POST" {
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}

		var ae AppendEntriesRequest
		if err := json.NewDecoder(r.Body).Decode(&ae); err != nil {
			c.logger.Errorf("%s", err)
			http.Error(w, emptyRequestVoteResponse.String(), http.StatusBadRequest)
			return
		}

		// There are a few cases here:
		// 1. The request is from an old term so reject
		// 2. The request is from a newer term so step down
		// 3. If we are a follower and get an append entries continue on with success
		// 4. If we are not a follower and get an append entries, it means there is
		//    another leader already and we should step down.
		switch {
		case ae.Term < c.State.Term():
			c.logger.Infof("Got AppendEntries request from older term, rejecting %+v %s", ae, c.State)
			aeResp := appendEntriesResponse{
				Term:    c.State.Term(),
				Success: false,
				Reason:  fmt.Sprintf("term %d < %d", ae.Term, c.State.Term()),
			}
			if err := json.NewEncoder(w).Encode(aeResp); err != nil {
				http.Error(w, emptyRequestVoteResponse.String(), http.StatusInternalServerError)
			}
			return
		case ae.Term > c.State.Term():
			c.logger.Infof("Got AppendEntries request from newer term, stepping down %+v %s", ae, c.State)
			c.State.StepDown(ae.Term)
		case ae.Term == c.State.Term():
			// ignore request to self and only step down if not in follower state.
			if ae.LeaderID != c.State.ID() && c.State.State() != StateFollower {
				c.logger.Infof("Got AppendEntries request from another leader with the same term, stepping down %+v %s", ae, c.State)
				c.State.StepDown(ae.Term)
			}
		}

		// ok
		c.State.LeaderID(ae.LeaderID)
		c.State.AppendEntriesEvent(&ae)
		aeResp := appendEntriesResponse{
			Term:    c.State.Term(),
			Success: true,
		}
		if err := json.NewEncoder(w).Encode(aeResp); err != nil {
			http.Error(w, emptyRequestVoteResponse.String(), http.StatusInternalServerError)
		}
		return
	}
}
