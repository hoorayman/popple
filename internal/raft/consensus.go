// raft consensus module
package raft

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"strconv"
	"sync"
	"time"

	"github.com/tidwall/buntdb"

	"github.com/hoorayman/popple/internal/conf"
	raftv1 "github.com/hoorayman/popple/internal/proto/raft/v1"
	"github.com/hoorayman/popple/internal/statemachine"
)

type LogEntry struct {
	Command []byte
	Term    int64
}

type CMState int

const (
	Follower CMState = iota
	Candidate
	Leader
	Dead
)

func (s CMState) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	case Dead:
		return "Dead"
	default:
		panic("undefined consensus module state")
	}
}

type ConsensusModule struct {
	mu      sync.Mutex
	id      int64
	peerIds []int64
	server  *Server

	newCommitReadyChan chan struct{}

	storage      *RaftPersistent
	stateMachine statemachine.IKVDB

	currentTerm int64
	votedFor    int64
	log         []LogEntry

	commitIndex        int
	lastApplied        int
	state              CMState
	electionResetEvent time.Time
	nextIndex          map[int64]int
	matchIndex         map[int64]int
}

func NewConsensusModule(id int64, peerIds []int64, server *Server, ready <-chan struct{}) *ConsensusModule {
	cm := new(ConsensusModule)
	cm.id = id
	cm.peerIds = peerIds
	cm.server = server
	cm.state = Follower
	cm.votedFor = -1
	cm.newCommitReadyChan = make(chan struct{}, 32)
	cm.commitIndex = -1
	cm.lastApplied = -1
	cm.nextIndex = make(map[int64]int)
	cm.matchIndex = make(map[int64]int)
	cm.storage = &RaftPersistent{}
	fsm, err := statemachine.NewKVDB()
	if err != nil {
		log.Fatalf("server[%v] failed to make fsm: %s", id, err)
	}
	cm.stateMachine = fsm

	if !conf.GetBool("dev") {
		cm.restoreFromStorage()
		cm.restoreLastApplied()
	}

	go func() {
		<-ready
		cm.mu.Lock()
		cm.electionResetEvent = time.Now()
		cm.mu.Unlock()
		cm.runElectionTimer()
	}()

	go cm.commitChanSender()
	return cm
}

func (cm *ConsensusModule) restoreFromStorage() {
	err := cm.storage.InitAndLoadLog(&cm.log)
	if err != nil {
		log.Fatal("InitAndLoadLog fail: ", err)
	}
	cm.currentTerm = cm.storage.GetTerm()
	cm.votedFor = cm.storage.GetVotedFor()
}

func (cm *ConsensusModule) persistToStorage(rollback, entries []LogEntry) {
	err := cm.storage.SetTerm(cm.currentTerm)
	if err != nil {
		log.Fatal("SetTerm fail: ", err)
	}
	err = cm.storage.SetVotedFor(cm.votedFor)
	if err != nil {
		log.Fatal("SetVotedFor fail: ", err)
	}
	err = cm.storage.AppendLog(rollback, entries)
	if err != nil {
		log.Fatal("AppendLog fail: ", err)
	}
}

func (cm *ConsensusModule) restoreLastApplied() {
	val, err := cm.stateMachine.Get(statemachine.LastAppliedKey)
	if err != nil && err != buntdb.ErrNotFound {
		log.Fatal("restoreLastApplied fail: ", err)
	}
	if err == buntdb.ErrNotFound {
		cm.lastApplied = -1
		return
	}
	cm.lastApplied, err = strconv.Atoi(val)
	if err != nil {
		log.Fatal("restoreLastApplied parse fail: ", err)
	}
}

func (cm *ConsensusModule) commitChanSender() {
	for range cm.newCommitReadyChan {
		// Find which entries we have to apply.
		cm.mu.Lock()
		savedLastApplied := cm.lastApplied
		var entries []LogEntry
		if cm.commitIndex > cm.lastApplied {
			entries = cm.log[cm.lastApplied+1 : cm.commitIndex+1]
			for _, e := range entries {
				err := cm.stateMachine.Call(e.Command)
				if err != nil && err != buntdb.ErrNotFound {
					log.Fatal("fsm run command fail: ", err)
				}
			}
			err := cm.stateMachine.Set(statemachine.LastAppliedKey, strconv.Itoa(cm.commitIndex))
			if err != nil {
				log.Fatal("fsm save lastApplied fail: ", err)
			}
			cm.lastApplied = cm.commitIndex
		}
		cm.mu.Unlock()
		cm.dlog("commitChanSender entries=%v, savedLastApplied=%d", entries, savedLastApplied)
	}

	cm.dlog("commitChanSender done")
}

func (cm *ConsensusModule) Report() (id int64, term int64, isLeader bool) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	return cm.id, cm.currentTerm, cm.state == Leader
}

func (cm *ConsensusModule) Stop() {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.state = Dead
	cm.dlog("becomes Dead")
	close(cm.newCommitReadyChan)
}

func (cm *ConsensusModule) Submit(command []byte) bool {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	cm.dlog("Submit received by %v: %v", cm.state, command)
	if cm.state == Leader && cm.stateMachine.CommandCheck(command) {
		cm.log = append(cm.log, LogEntry{Command: command, Term: cm.currentTerm})
		if !conf.GetBool("dev") {
			cm.persistToStorage(nil, []LogEntry{{Command: command, Term: cm.currentTerm}})
		}
		cm.dlog("... log=%v", cm.log)
		return true
	}

	return false
}

// dlog display debug message.
func (cm *ConsensusModule) dlog(format string, args ...interface{}) {
	if conf.GetBool("dev") {
		format = fmt.Sprintf("server[%d] ", cm.id) + format
		log.Printf(format, args...)
	}
}

func (cm *ConsensusModule) becomeFollower(term int64) {
	cm.dlog("becomes Follower with term=%d; log=%v", term, cm.log)
	cm.state = Follower
	cm.currentTerm = term
	cm.votedFor = -1
	cm.electionResetEvent = time.Now()

	go cm.runElectionTimer()
}

func (cm *ConsensusModule) RequestVote(ctx context.Context, request *raftv1.RequestVoteRequest) (*raftv1.RequestVoteResponse, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.state == Dead {
		return nil, nil
	}

	lastLogIndex, lastLogTerm := cm.lastLogIndexAndTerm()
	cm.dlog("RequestVote: %+v [currentTerm=%d, votedFor=%d, log index/term=(%d, %d)]", request, cm.currentTerm, cm.votedFor, lastLogIndex, lastLogTerm)
	if request.Term > cm.currentTerm {
		cm.dlog("... term out of date in RequestVote")
		cm.becomeFollower(request.Term)
	}

	reply := &raftv1.RequestVoteResponse{}
	if request.Term == cm.currentTerm &&
		(cm.votedFor == -1 || cm.votedFor == request.CandidateId) &&
		(request.LastLogTerm > lastLogTerm ||
			(request.LastLogTerm == lastLogTerm && int(request.LastLogIndex) >= lastLogIndex)) {
		reply.VoteGranted = true
		cm.votedFor = request.CandidateId
		cm.electionResetEvent = time.Now()
	} else {
		reply.VoteGranted = false
	}
	reply.Term = cm.currentTerm
	if !conf.GetBool("dev") {
		cm.persistToStorage(nil, nil)
	}
	cm.dlog("... RequestVote reply: %+v", reply)

	return reply, nil
}

func (cm *ConsensusModule) AppendEntries(ctx context.Context, request *raftv1.AppendEntriesRequest) (*raftv1.AppendEntriesResponse, error) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.state == Dead {
		return nil, nil
	}
	cm.dlog("AppendEntries: %+v", request)

	if request.Term > cm.currentTerm {
		cm.dlog("... term out of date in AppendEntries")
		cm.becomeFollower(request.Term)
	}

	reply := &raftv1.AppendEntriesResponse{}
	reply.Success = false
	if request.Term == cm.currentTerm {
		if cm.state != Follower {
			cm.becomeFollower(request.Term)
		}
		cm.electionResetEvent = time.Now()

		if request.PrevLogIndex == -1 ||
			(request.PrevLogIndex < int64(len(cm.log)) && request.PrevLogTerm == cm.log[request.PrevLogIndex].Term) {
			reply.Success = true

			logInsertIndex := int(request.PrevLogIndex + 1)
			newEntriesIndex := 0

			for {
				if logInsertIndex >= len(cm.log) || newEntriesIndex >= len(request.Entries) {
					break
				}
				if cm.log[logInsertIndex].Term != request.Entries[newEntriesIndex].Term {
					break
				}
				logInsertIndex++
				newEntriesIndex++
			}

			if newEntriesIndex < len(request.Entries) {
				cm.dlog("... rollback entries %v", cm.log[logInsertIndex:])
				newEntries := []LogEntry{}
				for _, e := range request.Entries[newEntriesIndex:] {
					newEntries = append(newEntries, LogEntry{Command: e.Command, Term: e.Term})
				}
				if !conf.GetBool("dev") {
					cm.persistToStorage(cm.log[logInsertIndex:], newEntries)
				}
				cm.dlog("... inserting entries %v from index %d", newEntries, logInsertIndex)
				cm.log = append(cm.log[:logInsertIndex], newEntries...)
				cm.dlog("... log is now: %v", cm.log)
			}
			// Set commit index.
			if int(request.LeaderCommit) > cm.commitIndex {
				cm.commitIndex = intMin(int(request.LeaderCommit), len(cm.log)-1)
				cm.dlog("... setting commitIndex=%d", cm.commitIndex)
				cm.newCommitReadyChan <- struct{}{}
			}
		}
	}

	reply.Term = cm.currentTerm
	if !conf.GetBool("dev") {
		cm.persistToStorage(nil, nil)
	}
	cm.dlog("AppendEntries reply: %+v", reply)

	return reply, nil
}

func (cm *ConsensusModule) lastLogIndexAndTerm() (int, int64) {
	if len(cm.log) > 0 {
		lastIndex := len(cm.log) - 1
		return lastIndex, cm.log[lastIndex].Term
	} else {
		return -1, -1
	}
}

// electionTimeout make a pseudo-random election timeout duration.
func (cm *ConsensusModule) electionTimeout() time.Duration {
	return time.Duration(150+rand.Intn(150)) * time.Millisecond // follow the raft paper
}

func (cm *ConsensusModule) runElectionTimer() {
	timeoutDuration := cm.electionTimeout()
	cm.mu.Lock()
	termStarted := cm.currentTerm
	cm.mu.Unlock()

	cm.dlog("election timer started (%v), term=%d", timeoutDuration, termStarted)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		<-ticker.C

		cm.mu.Lock()
		if cm.state != Candidate && cm.state != Follower {
			cm.dlog("in election timer state=%s, bailing out", cm.state)
			cm.mu.Unlock()
			return
		}

		if termStarted != cm.currentTerm {
			cm.dlog("in election timer term changed from %d to %d, bailing out", termStarted, cm.currentTerm)
			cm.mu.Unlock()
			return
		}

		if elapsed := time.Since(cm.electionResetEvent); elapsed >= timeoutDuration {
			cm.startElection()
			cm.mu.Unlock()
			return
		}
		cm.mu.Unlock()
	}
}

func (cm *ConsensusModule) startElection() {
	cm.state = Candidate
	cm.currentTerm += 1
	savedCurrentTerm := cm.currentTerm
	cm.electionResetEvent = time.Now()
	cm.votedFor = cm.id
	cm.dlog("becomes Candidate (currentTerm=%d); log=%v", savedCurrentTerm, cm.log)

	votesReceived := 1 // vote for itself

	if len(cm.peerIds) == 0 {
		cm.dlog("wins election with %d votes", votesReceived)
		cm.startLeader()
		return
	}

	for _, peerId := range cm.peerIds {
		go func(peerId int64) {
			cm.mu.Lock()
			savedLastLogIndex, savedLastLogTerm := cm.lastLogIndexAndTerm()
			cm.mu.Unlock()

			request := &raftv1.RequestVoteRequest{
				Term:         savedCurrentTerm,
				CandidateId:  cm.id,
				LastLogIndex: int64(savedLastLogIndex),
				LastLogTerm:  savedLastLogTerm,
			}

			cm.dlog("sending RequestVote to %d: %+v", peerId, request)
			cli := cm.server.peerClients[peerId]
			if cli == nil {
				return
			}
			if reply, err := cli.RequestVote(context.Background(), request); err == nil {
				cm.mu.Lock()
				defer cm.mu.Unlock()
				cm.dlog("received RequestVoteReply %+v", reply)

				if cm.state != Candidate {
					cm.dlog("while waiting for reply, state = %v", cm.state)
					return
				}

				if reply.Term > savedCurrentTerm {
					cm.dlog("term out of date in RequestVoteReply")
					cm.becomeFollower(reply.Term)
					return
				} else if reply.Term == savedCurrentTerm {
					if reply.VoteGranted {
						votesReceived += 1
						if votesReceived<<1 > len(cm.peerIds)+1 {
							cm.dlog("wins election with %d votes", votesReceived)
							cm.startLeader()
							return
						}
					}
				}
			}
		}(peerId)
	}

	go cm.runElectionTimer()
}

func (cm *ConsensusModule) startLeader() {
	cm.state = Leader

	for _, peerId := range cm.peerIds {
		cm.nextIndex[peerId] = len(cm.log)
		cm.matchIndex[peerId] = -1
	}
	cm.dlog("becomes Leader; term=%d, nextIndex=%v, matchIndex=%v; log=%v", cm.currentTerm, cm.nextIndex, cm.matchIndex, cm.log)

	go func() {
		ticker := time.NewTicker(20 * time.Millisecond)
		defer ticker.Stop()

		for {
			cm.leaderSendHeartbeats()
			<-ticker.C

			cm.mu.Lock()
			if cm.state != Leader {
				cm.mu.Unlock()
				return
			}
			cm.mu.Unlock()
		}
	}()
}

func (cm *ConsensusModule) leaderSendHeartbeats() {
	cm.mu.Lock()
	if cm.state != Leader {
		cm.mu.Unlock()
		return
	}
	savedCurrentTerm := cm.currentTerm
	cm.mu.Unlock()

	if len(cm.peerIds) == 0 {
		cm.mu.Lock()
		if cm.state == Leader {
			cm.commitIndex = len(cm.log) - 1
			cm.dlog("leader sets commitIndex := %d", cm.commitIndex)
			cm.newCommitReadyChan <- struct{}{}
		}
		cm.mu.Unlock()
	}
	for _, peerId := range cm.peerIds {
		go func(peerId int64) {
			cm.mu.Lock()
			ni := cm.nextIndex[peerId]
			prevLogIndex := ni - 1
			var prevLogTerm int64 = -1
			if prevLogIndex >= 0 {
				prevLogTerm = cm.log[prevLogIndex].Term
			}
			entries := cm.log[ni:]

			sendEntries := []*raftv1.LogEntry{}
			for _, e := range entries {
				sendEntries = append(sendEntries, &raftv1.LogEntry{Command: e.Command, Term: e.Term})
			}
			request := &raftv1.AppendEntriesRequest{
				Term:         savedCurrentTerm,
				LeaderId:     cm.id,
				PrevLogIndex: int64(prevLogIndex),
				PrevLogTerm:  prevLogTerm,
				Entries:      sendEntries,
				LeaderCommit: int64(cm.commitIndex),
			}
			cm.mu.Unlock()
			cm.dlog("sending AppendEntries to %v: ni=%d, args=%+v", peerId, ni, request)

			cli := cm.server.peerClients[peerId]
			if cli == nil {
				return
			}
			if reply, err := cli.AppendEntries(context.Background(), request); err == nil {
				cm.mu.Lock()
				defer cm.mu.Unlock()

				if reply.Term > savedCurrentTerm {
					cm.dlog("term out of date in heartbeat reply")
					cm.becomeFollower(reply.Term)
					return
				}

				if cm.state == Leader && savedCurrentTerm == reply.Term {
					if reply.Success {
						cm.nextIndex[peerId] = ni + len(entries)
						cm.matchIndex[peerId] = cm.nextIndex[peerId] - 1
						cm.dlog("AppendEntries reply from %d success: nextIndex := %v, matchIndex := %v", peerId, cm.nextIndex, cm.matchIndex)

						savedCommitIndex := cm.commitIndex
						for i := cm.commitIndex + 1; i < len(cm.log); i++ {
							if cm.log[i].Term == cm.currentTerm {
								matchCount := 1
								for _, peerId := range cm.peerIds {
									if cm.matchIndex[peerId] >= i {
										matchCount++
									}
								}
								if matchCount<<1 > len(cm.peerIds)+1 {
									cm.commitIndex = i
								}
							}
						}
						if cm.commitIndex != savedCommitIndex {
							cm.dlog("leader sets commitIndex := %d", cm.commitIndex)
							cm.newCommitReadyChan <- struct{}{}
						}
					} else {
						cm.nextIndex[peerId] = ni - 1
						cm.dlog("AppendEntries reply from %d !success: nextIndex := %d", peerId, ni-1)
					}
				}
			}
		}(peerId)
	}
}

func intMin(a, b int) int {
	if a < b {
		return a
	}

	return b
}
