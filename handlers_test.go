package cluster

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

func TestCluster_Handlers(t *testing.T) {
	cl := newCluster(t)
	r := cl.Routes()
	equals(t, 5, len(r))
}

func TestCluster_StatusHandler(t *testing.T) {
	cl := newCluster(t)
	handler := cl.StatusHandler()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", cl.routePrefix+RouteStatus, nil)
	handler(w, req)
	equals(t, http.StatusMethodNotAllowed, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", cl.routePrefix+RouteStatus, nil)
	handler(w, req)

	equals(t, http.StatusOK, w.Code)
	want := Status{
		ID:       1613,
		LeaderID: 0,
		State:    "follower",
		Term:     1,
		VotedFor: 0,
	}
	var got Status
	json.Unmarshal(w.Body.Bytes(), &got)
	equals(t, true, cmp.Equal(want, got, cmpopts.IgnoreFields(Status{}, "ID")))
}

func TestCluster_StepDownHandler(t *testing.T) {
	cl := newCluster(t)
	cl.State.State(StateLeader)
	handler := cl.StepDownHandler()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", cl.routePrefix+RouteStepDown, nil)
	handler(w, req)
	equals(t, http.StatusMethodNotAllowed, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", cl.routePrefix+RouteStepDown, nil)
	handler(w, req)

	equals(t, http.StatusOK, w.Code)
	want := Status{
		ID:       1613,
		LeaderID: 0,
		State:    "follower",
		Term:     1,
		VotedFor: 0,
	}
	var got Status
	json.Unmarshal(w.Body.Bytes(), &got)
	equals(t, true, cmp.Equal(want, got, cmpopts.IgnoreFields(Status{}, "ID")))
}

func TestCluster_IDHandler(t *testing.T) {
	cl := newCluster(t)
	idHandler := cl.IDHandler()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", cl.routePrefix+RouteID, nil)
	idHandler(w, req)
	equals(t, http.StatusMethodNotAllowed, w.Code)

	w = httptest.NewRecorder()
	req = httptest.NewRequest("GET", cl.routePrefix+RouteID, nil)
	idHandler(w, req)
	equals(t, http.StatusOK, w.Code)
	equals(t, strconv.Itoa(int(cl.State.ID()))+"\n", w.Body.String())
}

func TestCluster_RequestVoteHandler(t *testing.T) {
	cl := newCluster(t)
	rvHandler := cl.RequestVoteHandler()

	tests := []struct {
		name          string
		method        string
		req           *requestVoteRequest
		resp          *requestVoteResponse
		statusCode    int
		startState    int
		endState      int
		startTerm     int
		endTerm       int
		startVotedFor uint32
		endVotedFor   uint32
		startLeaderID uint32
		endLeaderID   uint32
	}{
		{
			name:          "00 method not allowed",
			method:        "GET",
			req:           nil,
			resp:          nil,
			statusCode:    http.StatusMethodNotAllowed,
			startState:    StateFollower,
			endState:      StateFollower,
			startTerm:     1,
			endTerm:       1,
			startVotedFor: NoVote,
			endVotedFor:   NoVote,
			startLeaderID: UnknownLeaderID,
			endLeaderID:   UnknownLeaderID,
		},
		{
			name:   "01 ok",
			method: "POST",
			req: &requestVoteRequest{
				Term:        cl.State.Term(),
				CandidateID: cl.State.ID(),
				NodeID:      cl.State.ID(),
			},
			resp: &requestVoteResponse{
				Term:        cl.State.Term(),
				VoteGranted: true,
				NodeID:      cl.State.ID(),
			},
			statusCode:    http.StatusOK,
			startState:    StateFollower,
			endState:      StateFollower,
			startTerm:     1,
			endTerm:       1,
			startVotedFor: NoVote,
			endVotedFor:   cl.State.ID(),
			startLeaderID: UnknownLeaderID,
			endLeaderID:   UnknownLeaderID,
		},
		{
			name:   "02 request from an old term so rejected",
			method: "POST",
			req: &requestVoteRequest{
				Term:        cl.State.Term() - 1,
				CandidateID: cl.State.ID(),
				NodeID:      cl.State.ID(),
			},
			resp: &requestVoteResponse{
				Term:        cl.State.Term(),
				VoteGranted: false,
				Reason:      "term 0 < 1",
				NodeID:      cl.State.ID(),
			},
			statusCode:    http.StatusOK,
			startState:    StateFollower,
			endState:      StateFollower,
			startTerm:     1,
			endTerm:       1,
			startVotedFor: NoVote,
			endVotedFor:   NoVote,
			startLeaderID: UnknownLeaderID,
			endLeaderID:   UnknownLeaderID,
		},
		{
			name:   "03 request from an old term so rejected already leader",
			method: "POST",
			req: &requestVoteRequest{
				Term:        cl.State.Term() - 1,
				CandidateID: cl.State.ID(),
				NodeID:      cl.State.ID(),
			},
			resp: &requestVoteResponse{
				Term:        cl.State.Term(),
				VoteGranted: false,
				Reason:      "already leader",
				NodeID:      cl.State.ID(),
			},
			statusCode:    http.StatusOK,
			startState:    StateLeader,
			endState:      StateLeader,
			startTerm:     1,
			endTerm:       1,
			startVotedFor: NoVote,
			endVotedFor:   NoVote,
			startLeaderID: cl.State.ID(),
			endLeaderID:   cl.State.ID(),
		},
		{
			name:   "04 double vote",
			method: "POST",
			req: &requestVoteRequest{
				Term:        cl.State.Term(),
				CandidateID: cl.State.ID() + 1,
				NodeID:      cl.State.ID(),
			},
			resp: &requestVoteResponse{
				Term:        cl.State.Term(),
				VoteGranted: false,
				Reason:      fmt.Sprintf("already cast vote for %d", cl.State.ID()),
				NodeID:      cl.State.ID(),
			},
			statusCode:    http.StatusOK,
			startState:    StateFollower,
			endState:      StateFollower,
			startTerm:     1,
			endTerm:       1,
			startVotedFor: cl.State.ID(),
			endVotedFor:   cl.State.ID(),
			startLeaderID: cl.State.ID(),
			endLeaderID:   cl.State.ID(),
		},
		{
			name:   "04 newer term",
			method: "POST",
			req: &requestVoteRequest{
				Term:        cl.State.Term() + 1,
				CandidateID: cl.State.ID(),
				NodeID:      cl.State.ID(),
			},
			resp: &requestVoteResponse{
				Term:        2,
				VoteGranted: true,
				NodeID:      cl.State.ID(),
			},
			statusCode:    http.StatusOK,
			startState:    StateFollower,
			endState:      StateFollower,
			startTerm:     1,
			endTerm:       2,
			startVotedFor: cl.State.ID(),
			endVotedFor:   cl.State.ID(),
			startLeaderID: UnknownLeaderID,
			endLeaderID:   UnknownLeaderID,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl.State.State(tt.startState)
			cl.State.Term(tt.startTerm)
			cl.State.VotedFor(tt.startVotedFor)
			cl.State.LeaderID(tt.startLeaderID)
			w := httptest.NewRecorder()
			var req *http.Request
			var body *bytes.Buffer
			if tt.req != nil {
				b, _ := json.Marshal(tt.req)
				body = bytes.NewBuffer(b)
				req = httptest.NewRequest(tt.method, cl.routePrefix+RouteRequestVote, body)
			} else {
				req = httptest.NewRequest(tt.method, cl.routePrefix+RouteRequestVote, nil)
			}
			rvHandler(w, req)
			equals(t, tt.statusCode, w.Code)
			if tt.resp != nil {
				var got requestVoteResponse
				json.Unmarshal(w.Body.Bytes(), &got)
				equals(t, *tt.resp, got)
			}
			equals(t, cl.State.StateString(tt.endState), cl.State.StateString(cl.State.State()))
			equals(t, tt.endTerm, cl.State.Term())
			equals(t, tt.endVotedFor, cl.State.VotedFor())
			equals(t, tt.endLeaderID, cl.State.LeaderID())
		})
	}
}

func TestCluster_AppendEntriesHandler(t *testing.T) {
	cl := newCluster(t)
	aeHandler := cl.AppendEntriesHandler()

	tests := []struct {
		name          string
		method        string
		req           *AppendEntriesRequest
		wantResp      *appendEntriesResponse
		statusCode    int
		startState    int
		endState      int
		startTerm     int
		endTerm       int
		startVotedFor uint32
		endVotedFor   uint32
		startLeaderID uint32
		endLeaderID   uint32
	}{
		{
			name:          "00 method not allowed",
			method:        "GET",
			req:           nil,
			wantResp:      nil,
			statusCode:    http.StatusMethodNotAllowed,
			startState:    StateFollower,
			endState:      StateFollower,
			startTerm:     1,
			endTerm:       1,
			startVotedFor: NoVote,
			endVotedFor:   NoVote,
			startLeaderID: UnknownLeaderID,
			endLeaderID:   UnknownLeaderID,
		},
		{
			name:   "01 request from an old term so rejected",
			method: "POST",
			req: &AppendEntriesRequest{
				Term:     cl.State.Term() - 1,
				LeaderID: cl.State.ID(),
			},
			wantResp: &appendEntriesResponse{
				Term:    cl.State.Term(),
				Success: false,
				Reason:  "term 0 < 1",
			},
			statusCode:    http.StatusOK,
			startState:    StateFollower,
			endState:      StateFollower,
			startTerm:     1,
			endTerm:       1,
			startVotedFor: NoVote,
			endVotedFor:   NoVote,
			startLeaderID: UnknownLeaderID,
			endLeaderID:   UnknownLeaderID,
		},
		{
			name:   "02 request from a newer term so step down",
			method: "POST",
			req: &AppendEntriesRequest{
				Term:     cl.State.Term() + 1,
				LeaderID: 999,
			},
			wantResp: &appendEntriesResponse{
				Term:    2,
				Success: true,
			},
			statusCode:    http.StatusOK,
			startState:    StateCandidate,
			endState:      StateFollower,
			startTerm:     1,
			endTerm:       2,
			startVotedFor: NoVote,
			endVotedFor:   NoVote,
			startLeaderID: UnknownLeaderID,
			endLeaderID:   999,
		},
		{
			name:   "03 request from a equal term so step down",
			method: "POST",
			req: &AppendEntriesRequest{
				Term:     2,
				LeaderID: 999,
			},
			wantResp: &appendEntriesResponse{
				Term:    2,
				Success: true,
			},
			statusCode:    http.StatusOK,
			startState:    StateLeader,
			endState:      StateFollower,
			startTerm:     2,
			endTerm:       2,
			startVotedFor: NoVote,
			endVotedFor:   NoVote,
			startLeaderID: UnknownLeaderID,
			endLeaderID:   999,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cl.State.State(tt.startState)
			cl.State.Term(tt.startTerm)
			cl.State.VotedFor(tt.startVotedFor)
			cl.State.LeaderID(tt.startLeaderID)
			w := httptest.NewRecorder()
			var req *http.Request
			var body *bytes.Buffer
			if tt.req != nil {
				b, _ := json.Marshal(tt.req)
				body = bytes.NewBuffer(b)
				req = httptest.NewRequest(tt.method, cl.routePrefix+RouteAppendEntries, body)
			} else {
				req = httptest.NewRequest(tt.method, cl.routePrefix+RouteAppendEntries, nil)
			}
			aeHandler(w, req)
			equals(t, tt.statusCode, w.Code)
			if tt.wantResp != nil {
				want, _ := json.Marshal(tt.wantResp)
				equals(t, string(want)+"\n", w.Body.String())
			}
			equals(t, cl.State.StateString(tt.endState), cl.State.StateString(cl.State.State()))
			equals(t, tt.endTerm, cl.State.Term())
			equals(t, tt.endVotedFor, cl.State.VotedFor())
			equals(t, tt.endLeaderID, cl.State.LeaderID())
		})
	}
}
