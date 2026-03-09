package main

import (
	"context"
	"cuhk/asgn/raft"
	"fmt"
	"google.golang.org/grpc"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	Follower = iota
	Candidate
	Leader
)

const heartbeatScheduleBias = 50 * time.Millisecond

type voteReqMsg struct {
	args  *raft.RequestVoteArgs
	reply chan *raft.RequestVoteReply
}

type appendEntryReqMsg struct {
	args  *raft.AppendEntriesArgs
	reply chan *raft.AppendEntriesReply
}

type proposeReqMsg struct {
	args  *raft.ProposeArgs
	reply chan *raft.ProposeReply
}

type voteRespMsg struct {
	reply *raft.RequestVoteReply
}

type appendRespMsg struct {
	peerID int32
	reply  *raft.AppendEntriesReply
}

type getValueReqMsg struct {
	args  *raft.GetValueArgs
	reply chan *raft.GetValueReply
}

type pendingPropose struct {
	index  int
	op     raft.Operation
	key    string
	status raft.Status
	reply  chan *raft.ProposeReply
}

type raftNode struct {
	log []*raft.LogEntry

	currentTerm       int
	votedFor          int32
	kvStore           map[string]int32
	commitIndex       int
	lastApplied       int
	nextIndex         map[int32]int
	matchIndex        map[int32]int
	nodeId            int32
	state             int
	currentLeaderId   int32
	hostConnectionMap map[int32]raft.RaftNodeClient
	heartBeatInterval time.Duration
	electionTimeout   time.Duration

	reqVoteChan      chan voteReqMsg
	appendEntryChan  chan appendEntryReqMsg
	proposeChan      chan proposeReqMsg
	getValueChan     chan getValueReqMsg
	resetTimeoutChan chan time.Duration
	setHeartBeatChan chan time.Duration
	voteRespChan     chan voteRespMsg
	appendRespChan   chan appendRespMsg
}

func main() {
	ports := os.Args[2]
	myport, _ := strconv.Atoi(os.Args[1])
	nodeID, _ := strconv.Atoi(os.Args[3])
	heartBeatInterval, _ := strconv.Atoi(os.Args[4])
	electionTimeout, _ := strconv.Atoi(os.Args[5])

	portStrings := strings.Split(ports, ",")

	// A map where
	// 		the key is the node id
	//		the value is the {hostname:port}
	nodeidPortMap := make(map[int]int)
	for i, portStr := range portStrings {
		port, _ := strconv.Atoi(portStr)
		nodeidPortMap[i] = port
	}

	// Create and start the Raft Node.
	_, err := NewRaftNode(myport, nodeidPortMap,
		nodeID, heartBeatInterval, electionTimeout)

	if err != nil {
		log.Fatalln("Failed to create raft node:", err)
	}

	// Run the raft node forever.
	select {}
}

// Desc:
// NewRaftNode creates a new RaftNode. This function should return only when
// all nodes have joined the ring, and should return a non-nil error if this node
// could not be started in spite of dialing any other nodes.
//
// Params:
// myport: the port of this new node. We use tcp in this project.
//			   	Note: Please listen to this port rather than nodeidPortMap[nodeId]
// nodeidPortMap: a map from all node IDs to their ports.
// nodeId: the id of this node
// heartBeatInterval: the Heart Beat Interval when this node becomes leader. In millisecond.
// electionTimeout: The election timeout for this node. In millisecond.
func NewRaftNode(myport int, nodeidPortMap map[int]int, nodeId, heartBeatInterval,
	electionTimeout int) (raft.RaftNodeServer, error) {
	delete(nodeidPortMap, nodeId)

	hostConnectionMap := make(map[int32]raft.RaftNodeClient)

	rn := raftNode{
		log:               make([]*raft.LogEntry, 1),
		currentTerm:       0,
		votedFor:          -1,
		kvStore:           make(map[string]int32),
		commitIndex:       0,
		lastApplied:       0,
		nextIndex:         make(map[int32]int),
		matchIndex:        make(map[int32]int),
		nodeId:            int32(nodeId),
		state:             Follower,
		currentLeaderId:   -1,
		hostConnectionMap: hostConnectionMap,
		heartBeatInterval: time.Duration(heartBeatInterval) * time.Millisecond,
		electionTimeout:   time.Duration(electionTimeout) * time.Millisecond,
		reqVoteChan:       make(chan voteReqMsg),
		appendEntryChan:   make(chan appendEntryReqMsg),
		proposeChan:       make(chan proposeReqMsg),
		getValueChan:      make(chan getValueReqMsg),
		resetTimeoutChan:  make(chan time.Duration),
		setHeartBeatChan:  make(chan time.Duration),
		voteRespChan:      make(chan voteRespMsg, len(nodeidPortMap)),
		appendRespChan:    make(chan appendRespMsg, len(nodeidPortMap)),
	}

	l, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", myport))
	if err != nil {
		log.Println("Fail to listen port", err)
		os.Exit(1)
	}

	s := grpc.NewServer()
	raft.RegisterRaftNodeServer(s, &rn)

	log.Printf("Start listening to port: %d", myport)
	go s.Serve(l)

	for tmpHostId, hostPorts := range nodeidPortMap {
		hostId := int32(tmpHostId)
		numTry := 0
		for {
			numTry++

			conn, err := grpc.Dial(fmt.Sprintf("127.0.0.1:%d", hostPorts), grpc.WithInsecure(), grpc.WithBlock())
			client := raft.NewRaftNodeClient(conn)
			if err != nil {
				log.Println("Fail to connect other nodes. ", err)
				time.Sleep(1 * time.Second)
			} else {
				rn.hostConnectionMap[hostId] = client
				break
			}
		}
	}
	log.Printf("Successfully connect all nodes")

	go rn.eventLoop()

	return &rn, nil
}

// Desc:
// Propose initializes proposing a new operation, and replies with the
// result of committing this operation. Propose should not return until
// this operation has been committed, or this node is not leader now.
//
// If the we put a new <k, v> pair or deleted an existing <k, v> pair
// successfully, it should return OK; If it tries to delete an non-existing
// key, a KeyNotFound should be returned; If this node is not leader now,
// it should return WrongNode as well as the currentLeader id.
//
// Params:
// args: the operation to propose
// reply: as specified in Desc
func (rn *raftNode) Propose(ctx context.Context, args *raft.ProposeArgs) (*raft.ProposeReply, error) {
	replyChan := make(chan *raft.ProposeReply)
	rn.proposeChan <- proposeReqMsg{args: args, reply: replyChan}
	reply := <-replyChan
	return reply, nil
}

// Desc:GetValue
// GetValue looks up the value for a key, and replies with the value or with
// the Status KeyNotFound.
//
// Params:
// args: the key to check
// reply: the value and status for this lookup of the given key
func (rn *raftNode) GetValue(ctx context.Context, args *raft.GetValueArgs) (*raft.GetValueReply, error) {
	replyChan := make(chan *raft.GetValueReply)
	rn.getValueChan <- getValueReqMsg{args: args, reply: replyChan}
	reply := <-replyChan
	return reply, nil
}

// Desc:
// Receive a RecvRequestVote message from another Raft Node. Check the paper for more details.
//
// Params:
// args: the RequestVote Message, you must include From(src node id) and To(dst node id) when
// you call this API
// reply: the RequestVote Reply Message
func (rn *raftNode) RequestVote(ctx context.Context, args *raft.RequestVoteArgs) (*raft.RequestVoteReply, error) {
	replyChan := make(chan *raft.RequestVoteReply)
	rn.reqVoteChan <- voteReqMsg{args: args, reply: replyChan}
	reply := <-replyChan
	return reply, nil
}

// Desc:
// Receive a RecvAppendEntries message from another Raft Node. Check the paper for more details.
//
// Params:
// args: the AppendEntries Message, you must include From(src node id) and To(dst node id) when
// you call this API
// reply: the AppendEntries Reply Message
func (rn *raftNode) AppendEntries(ctx context.Context, args *raft.AppendEntriesArgs) (*raft.AppendEntriesReply, error) {
	replyChan := make(chan *raft.AppendEntriesReply)
	rn.appendEntryChan <- appendEntryReqMsg{args: args, reply: replyChan}
	reply := <-replyChan
	return reply, nil
}

// Desc:
// Set electionTimeOut as args.Timeout milliseconds.
// You also need to stop current ticker and reset it to fire every args.Timeout milliseconds.
//
// Params:
// args: the heartbeat duration
// reply: no use
func (rn *raftNode) SetElectionTimeout(ctx context.Context, args *raft.SetElectionTimeoutArgs) (*raft.SetElectionTimeoutReply, error) {
	rn.resetTimeoutChan <- time.Duration(args.Timeout) * time.Millisecond
	var reply raft.SetElectionTimeoutReply
	return &reply, nil
}

// Desc:
// Set heartBeatInterval as args.Interval milliseconds.
// You also need to stop current ticker and reset it to fire every args.Interval milliseconds.
//
// Params:
// args: the heartbeat duration
// reply: no use
func (rn *raftNode) SetHeartBeatInterval(ctx context.Context, args *raft.SetHeartBeatIntervalArgs) (*raft.SetHeartBeatIntervalReply, error) {
	rn.setHeartBeatChan <- time.Duration(args.Interval) * time.Millisecond
	var reply raft.SetHeartBeatIntervalReply
	return &reply, nil
}

//NO NEED TO TOUCH THIS FUNCTION
func (rn *raftNode) CheckEvents(context.Context, *raft.CheckEventsArgs) (*raft.CheckEventsReply, error) {
	return nil, nil
}

func (rn *raftNode) eventLoop() {
	lastElectionReset := time.Now()
	lastHeartbeatSent := time.Now()
	votesGranted := 0
	pendingProposes := make([]pendingPropose, 0)

	for {
		select {
		case msg := <-rn.proposeChan:
			if rn.state != Leader {
				msg.reply <- &raft.ProposeReply{Status: raft.Status_WrongNode, CurrentLeader: rn.currentLeaderId}
				continue
			}

			entry := &raft.LogEntry{
				Term:  int32(rn.currentTerm),
				Op:    msg.args.Op,
				Key:   msg.args.Key,
				Value: msg.args.V,
			}
			rn.log = append(rn.log, entry)
			entryIndex := rn.getLastLogIndex()
			status := raft.Status_OK
			pendingProposes = append(pendingProposes, pendingPropose{
				index:  entryIndex,
				op:     msg.args.Op,
				key:    msg.args.Key,
				status: status,
				reply:  msg.reply,
			})
			rn.broadcastAppendEntries(false)
			lastHeartbeatSent = time.Now().Add(heartbeatScheduleBias)
			continue
		default:
		}

		wait := rn.electionTimeout
		if rn.state == Leader {
			wait = rn.heartBeatInterval
			elapsed := time.Since(lastHeartbeatSent)
			if elapsed < wait {
				wait -= elapsed
			} else {
				wait = 0
			}
		} else {
			elapsed := time.Since(lastElectionReset)
			if elapsed < wait {
				wait -= elapsed
			} else {
				wait = 0
			}
		}

		select {
		case <-time.After(wait):
			if rn.state == Leader {
				rn.sendHeartbeats()
				lastHeartbeatSent = time.Now()
			} else {
				votesGranted = rn.startElection()
				lastElectionReset = time.Now()
			}

		case timeout := <-rn.resetTimeoutChan:
			rn.electionTimeout = timeout
			lastElectionReset = time.Now()

		case interval := <-rn.setHeartBeatChan:
			rn.heartBeatInterval = interval
			lastHeartbeatSent = time.Now()

		case msg := <-rn.reqVoteChan:
			reply := &raft.RequestVoteReply{
				From:        rn.nodeId,
				To:          msg.args.From,
				Term:        int32(rn.currentTerm),
				VoteGranted: false,
			}

			if msg.args.Term < int32(rn.currentTerm) {
				msg.reply <- reply
				continue
			}

			if msg.args.Term > int32(rn.currentTerm) {
				rn.currentTerm = int(msg.args.Term)
				rn.state = Follower
				rn.votedFor = -1
				rn.currentLeaderId = -1
			}

			reply.Term = int32(rn.currentTerm)
			if (rn.votedFor == -1 || rn.votedFor == msg.args.CandidateId) && rn.candidateLogUpToDate(msg.args.LastLogIndex, msg.args.LastLogTerm) {
				rn.votedFor = msg.args.CandidateId
				reply.VoteGranted = true
				lastElectionReset = time.Now()
			}

			msg.reply <- reply

		case resp := <-rn.voteRespChan:
			if resp.reply == nil {
				continue
			}
			if resp.reply.Term > int32(rn.currentTerm) {
				rn.currentTerm = int(resp.reply.Term)
				rn.state = Follower
				rn.votedFor = -1
				rn.currentLeaderId = -1
				lastElectionReset = time.Now()
				continue
			}
			if rn.state != Candidate || resp.reply.Term != int32(rn.currentTerm) || !resp.reply.VoteGranted {
				continue
			}
			votesGranted++
			if votesGranted > rn.clusterSize()/2 {
				rn.becomeLeader()
				rn.sendHeartbeats()
				lastHeartbeatSent = time.Now()
			}

		case msg := <-rn.appendEntryChan:
			args := msg.args
			reply := &raft.AppendEntriesReply{
				From:       rn.nodeId,
				To:         args.From,
				Term:       int32(rn.currentTerm),
				Success:    false,
				MatchIndex: int32(rn.getLastLogIndex()),
			}

			if args.Term < int32(rn.currentTerm) {
				msg.reply <- reply
				continue
			}

			if args.Term > int32(rn.currentTerm) {
				rn.currentTerm = int(args.Term)
				rn.votedFor = -1
			}
			if rn.state != Follower {
				rn.state = Follower
			}
			rn.currentLeaderId = args.LeaderId
			lastElectionReset = time.Now()

			if int(args.PrevLogIndex) >= len(rn.log) {
				reply.Term = int32(rn.currentTerm)
				msg.reply <- reply
				continue
			}
			if args.PrevLogIndex > 0 && rn.log[args.PrevLogIndex].Term != args.PrevLogTerm {
				reply.Term = int32(rn.currentTerm)
				msg.reply <- reply
				continue
			}

			insertIndex := int(args.PrevLogIndex) + 1
			for i, entry := range args.Entries {
				logIdx := insertIndex + i
				if logIdx < len(rn.log) {
					if rn.log[logIdx].Term != entry.Term {
						rn.log = rn.log[:logIdx]
						rn.log = append(rn.log, entry)
					}
				} else {
					rn.log = append(rn.log, entry)
				}
			}

			if args.LeaderCommit > int32(rn.commitIndex) {
				lastNewEntryIndex := int(args.PrevLogIndex) + len(args.Entries)
				if int(args.LeaderCommit) < lastNewEntryIndex {
					rn.commitIndex = int(args.LeaderCommit)
				} else {
					rn.commitIndex = lastNewEntryIndex
				}
				rn.applyCommittedEntries()
			}

			reply.Success = true
			reply.Term = int32(rn.currentTerm)
			reply.MatchIndex = int32(int(args.PrevLogIndex) + len(args.Entries))
			msg.reply <- reply

		case resp := <-rn.appendRespChan:
			if resp.reply == nil {
				continue
			}
			if resp.reply.Term > int32(rn.currentTerm) {
				rn.currentTerm = int(resp.reply.Term)
				rn.state = Follower
				rn.votedFor = -1
				rn.currentLeaderId = -1
				lastElectionReset = time.Now()
				continue
			}
			if rn.state != Leader || resp.reply.Term != int32(rn.currentTerm) {
				continue
			}
			if resp.reply.Success {
				rn.matchIndex[resp.peerID] = int(resp.reply.MatchIndex)
				rn.nextIndex[resp.peerID] = int(resp.reply.MatchIndex) + 1
				if rn.advanceLeaderCommit() {
					appliedStatuses := rn.applyCommittedEntries()
					pendingProposes = rn.finishCommittedProposals(pendingProposes, appliedStatuses)
				}
			} else if rn.nextIndex[resp.peerID] > 1 {
				rn.nextIndex[resp.peerID]--
			}

		case msg := <-rn.getValueChan:
			if value, ok := rn.kvStore[msg.args.Key]; ok {
				msg.reply <- &raft.GetValueReply{V: value, Status: raft.Status_KeyFound}
			} else {
				msg.reply <- &raft.GetValueReply{Status: raft.Status_KeyNotFound}
			}

		case msg := <-rn.proposeChan:
			if rn.state != Leader {
				msg.reply <- &raft.ProposeReply{Status: raft.Status_WrongNode, CurrentLeader: rn.currentLeaderId}
				continue
			}

			entry := &raft.LogEntry{
				Term:  int32(rn.currentTerm),
				Op:    msg.args.Op,
				Key:   msg.args.Key,
				Value: msg.args.V,
			}
			rn.log = append(rn.log, entry)
			entryIndex := rn.getLastLogIndex()
			status := raft.Status_OK
			pendingProposes = append(pendingProposes, pendingPropose{
				index:  entryIndex,
				op:     msg.args.Op,
				key:    msg.args.Key,
				status: status,
				reply:  msg.reply,
			})
			rn.broadcastAppendEntries(false)
			lastHeartbeatSent = time.Now().Add(heartbeatScheduleBias)
		}
	}
}

func (rn *raftNode) clusterSize() int {
	return len(rn.hostConnectionMap) + 1
}

func (rn *raftNode) getLastLogIndex() int {
	return len(rn.log) - 1
}

func (rn *raftNode) getLastLogTerm() int32 {
	if len(rn.log) <= 1 {
		return 0
	}
	return rn.log[len(rn.log)-1].Term
}

func (rn *raftNode) candidateLogUpToDate(lastLogIndex, lastLogTerm int32) bool {
	myLastTerm := rn.getLastLogTerm()
	if lastLogTerm != myLastTerm {
		return lastLogTerm > myLastTerm
	}
	return int(lastLogIndex) >= rn.getLastLogIndex()
}

func (rn *raftNode) startElection() int {
	rn.state = Candidate
	rn.currentTerm++
	rn.votedFor = rn.nodeId
	rn.currentLeaderId = -1

	log.Printf("Node %d starts election for term %d", rn.nodeId, rn.currentTerm)

	lastLogIndex := rn.getLastLogIndex()
	lastLogTerm := rn.getLastLogTerm()

	for peerID, client := range rn.hostConnectionMap {
		args := &raft.RequestVoteArgs{
			From:         rn.nodeId,
			To:           peerID,
			Term:         int32(rn.currentTerm),
			CandidateId:  rn.nodeId,
			LastLogIndex: int32(lastLogIndex),
			LastLogTerm:  lastLogTerm,
		}
		go rn.sendRequestVote(client, args)
	}

	return 1
}

func (rn *raftNode) becomeLeader() {
	rn.state = Leader
	rn.currentLeaderId = rn.nodeId
	next := rn.getLastLogIndex() + 1
	for peerID := range rn.hostConnectionMap {
		rn.nextIndex[peerID] = next
		rn.matchIndex[peerID] = 0
	}
}

func (rn *raftNode) sendRequestVote(client raft.RaftNodeClient, args *raft.RequestVoteArgs) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	reply, err := client.RequestVote(ctx, args)
	if err != nil {
		return
	}
	rn.voteRespChan <- voteRespMsg{reply: reply}
}

func (rn *raftNode) sendHeartbeats() {
	rn.broadcastAppendEntries(false)
}

func (rn *raftNode) buildAppendEntriesArgs(peerID int32) *raft.AppendEntriesArgs {
	nextIndex := rn.nextIndex[peerID]
	if nextIndex <= 0 {
		nextIndex = 1
	}

	prevLogIndex := nextIndex - 1
	prevLogTerm := int32(0)
	if prevLogIndex > 0 && prevLogIndex < len(rn.log) {
		prevLogTerm = rn.log[prevLogIndex].Term
	}

	entries := make([]*raft.LogEntry, 0)
	if nextIndex < len(rn.log) {
		entries = append(entries, rn.log[nextIndex:]...)
	}

	return &raft.AppendEntriesArgs{
		From:         rn.nodeId,
		To:           peerID,
		Term:         int32(rn.currentTerm),
		LeaderId:     rn.nodeId,
		PrevLogIndex: int32(prevLogIndex),
		PrevLogTerm:  prevLogTerm,
		Entries:      entries,
		LeaderCommit: int32(rn.commitIndex),
	}
}

func (rn *raftNode) broadcastAppendEntries(waitAll bool) {
	doneCh := make(chan struct{}, len(rn.hostConnectionMap))
	for peerID, client := range rn.hostConnectionMap {
		args := rn.buildAppendEntriesArgs(peerID)
		go func(peerID int32, client raft.RaftNodeClient, args *raft.AppendEntriesArgs) {
			rn.sendAppendEntries(peerID, client, args)
			doneCh <- struct{}{}
		}(peerID, client, args)
	}

	if waitAll {
		for i := 0; i < len(rn.hostConnectionMap); i++ {
			<-doneCh
		}
	}
}

func (rn *raftNode) sendAppendEntries(peerID int32, client raft.RaftNodeClient, args *raft.AppendEntriesArgs) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	reply, err := client.AppendEntries(ctx, args)
	if err != nil {
		return
	}
	rn.appendRespChan <- appendRespMsg{peerID: peerID, reply: reply}
}

func (rn *raftNode) applyCommittedEntries() map[int]raft.Status {
	appliedStatuses := make(map[int]raft.Status)
	for rn.lastApplied < rn.commitIndex {
		rn.lastApplied++
		entry := rn.log[rn.lastApplied]
		switch entry.Op {
		case raft.Operation_Put:
			rn.kvStore[entry.Key] = entry.Value
			appliedStatuses[rn.lastApplied] = raft.Status_OK
		case raft.Operation_Delete:
			if _, ok := rn.kvStore[entry.Key]; ok {
				delete(rn.kvStore, entry.Key)
				appliedStatuses[rn.lastApplied] = raft.Status_OK
			} else {
				appliedStatuses[rn.lastApplied] = raft.Status_KeyNotFound
			}
		}
	}
	return appliedStatuses
}

func (rn *raftNode) finishCommittedProposals(pending []pendingPropose, appliedStatuses map[int]raft.Status) []pendingPropose {
	remaining := make([]pendingPropose, 0, len(pending))
	for _, proposal := range pending {
		if status, ok := appliedStatuses[proposal.index]; ok {
			proposal.reply <- &raft.ProposeReply{Status: status}
		} else {
			remaining = append(remaining, proposal)
		}
	}
	return remaining
}

func (rn *raftNode) advanceLeaderCommit() bool {
	advanced := false
	for idx := rn.commitIndex + 1; idx < len(rn.log); idx++ {
		if rn.log[idx].Term != int32(rn.currentTerm) {
			continue
		}
		count := 1
		for _, matchIdx := range rn.matchIndex {
			if matchIdx >= idx {
				count++
			}
		}
		if count > rn.clusterSize()/2 {
			rn.commitIndex = idx
			advanced = true
		}
	}
	return advanced
}
