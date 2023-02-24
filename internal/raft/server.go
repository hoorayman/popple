package raft

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/gin-gonic/gin"
	"github.com/soheilhy/cmux"
	"golang.org/x/sync/errgroup"

	"github.com/hoorayman/popple/internal/conf"
	raftv1 "github.com/hoorayman/popple/internal/proto/raft/v1"
	"github.com/hoorayman/popple/internal/statemachine"
)

type Server struct {
	mu sync.Mutex

	serverId int64
	peerIds  []int64

	cm       *ConsensusModule
	rpcProxy *RPCProxy

	rpcServer *grpc.Server
	listener  net.Listener

	peerClients map[int64]raftv1.RaftServiceClient

	sidAddr map[int64]string

	ready <-chan struct{}
	quit  chan struct{}
}

func NewServer(sid int64, cluster string, ready <-chan struct{}) *Server {
	s := new(Server)
	s.serverId = sid
	s.peerClients = make(map[int64]raftv1.RaftServiceClient)
	s.ready = ready
	s.quit = make(chan struct{})
	sidAddr, err := GetClusterIdAddr(cluster)
	if err != nil {
		log.Fatal("Parse --cluster fail: ", err)
	}
	s.sidAddr = sidAddr
	for sid := range s.sidAddr {
		if sid != s.serverId {
			s.peerIds = append(s.peerIds, sid)
		}
	}

	return s
}

func (s *Server) Serve() {
	s.mu.Lock()
	s.cm = NewConsensusModule(s.serverId, s.peerIds, s, s.ready)

	s.rpcServer = grpc.NewServer()
	s.rpcProxy = &RPCProxy{s.cm}
	raftv1.RegisterRaftServiceServer(s.rpcServer, s.rpcProxy)

	var err error
	s.listener, err = net.Listen("tcp", s.sidAddr[s.serverId])
	if err != nil {
		log.Fatalf("[%v] failed to listen: %s", s.serverId, err)
	}
	log.Printf("server[%v] listening at %s", s.serverId, s.listener.Addr())
	s.mu.Unlock()

	c := make(chan os.Signal)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		select {
		case <-s.quit:
			log.Printf("server[%v] quit", s.serverId)
		case <-c:
			s.listener.Close()
			os.Exit(1)
		}
	}()

	m := cmux.New(s.listener)
	grpcListener := m.MatchWithWriters(cmux.HTTP2MatchHeaderFieldSendSettings("content-type", "application/grpc"))
	httpListener := m.Match(cmux.Any())

	// http server
	if conf.GetBool("dev") {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.New()
	r.Use(gin.Recovery())
	r.POST("/submit", func(c *gin.Context) {
		var req statemachine.Cmd
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": err.Error()})
			return
		}

		data, err := json.Marshal(req)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": err.Error()})
			return
		}
		if s.cm.Submit(data) {
			c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "success"})
			return
		} else {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -2, "msg": "not accepted"})
			return
		}
	})
	r.GET("/fetch/:key", func(c *gin.Context) {
		key := c.Param("key")
		val, err := s.cm.stateMachine.Get(key)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -2, "msg": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"code": 0, "data": val})
	})

	g := errgroup.Group{}
	g.Go(func() error {
		return s.rpcServer.Serve(grpcListener)
	})
	g.Go(func() error {
		return http.Serve(httpListener, r)
	})
	g.Go(func() error {
		return m.Serve()
	})

	err = g.Wait()
	if err != nil {
		log.Fatalf("failed to serve: %s", err)
	}
}

func (s *Server) DisconnectAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.peerClients {
		if s.peerClients[id] != nil {
			s.peerClients[id] = nil
		}
	}
}

func (s *Server) Shutdown() {
	s.cm.Stop()
	close(s.quit)
}

func (s *Server) GetListenAddr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	addr := s.listener.Addr()
	if addr != nil {
		return addr.String()
	}

	return ""
}

func (s *Server) SetServerAddr(sidAddr map[int64]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sidAddr = sidAddr
}

func (s *Server) ConnectToPeer(peerId int64, addr string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peerClients[peerId] == nil {
		conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return err
		}
		s.peerClients[peerId] = raftv1.NewRaftServiceClient(conn)
	}

	return nil
}

func (s *Server) DisconnectPeer(peerId int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.peerClients[peerId] != nil {
		s.peerClients[peerId] = nil
	}

	return nil
}

func (s *Server) WaitConnectToPeers() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		<-ticker.C

		for _, sid := range s.peerIds {
			if s.peerClients[sid] == nil {
				s.ConnectToPeer(sid, s.sidAddr[sid])
			}
		}

		ready := true
		for _, sid := range s.peerIds {
			if s.peerClients[sid] == nil {
				ready = false
			}
		}
		if ready {
			return
		}
	}
}

type RPCProxy struct {
	cm *ConsensusModule
}

func (rpp *RPCProxy) RequestVote(ctx context.Context, request *raftv1.RequestVoteRequest) (*raftv1.RequestVoteResponse, error) {
	if len(os.Getenv("RAFT_UNRELIABLE_RPC")) > 0 {
		dice := rand.Intn(10)
		if dice == 9 {
			rpp.cm.dlog("drop RequestVote")
			return nil, fmt.Errorf("RPC failed")
		} else if dice == 8 {
			rpp.cm.dlog("delay RequestVote")
			time.Sleep(75 * time.Millisecond)
		}
	}

	return rpp.cm.RequestVote(ctx, request)
}

func (rpp *RPCProxy) AppendEntries(ctx context.Context, request *raftv1.AppendEntriesRequest) (*raftv1.AppendEntriesResponse, error) {
	if len(os.Getenv("RAFT_UNRELIABLE_RPC")) > 0 {
		dice := rand.Intn(10)
		if dice == 9 {
			rpp.cm.dlog("drop AppendEntries")
			return nil, fmt.Errorf("RPC failed")
		} else if dice == 8 {
			rpp.cm.dlog("delay AppendEntries")
			time.Sleep(75 * time.Millisecond)
		}
	}

	return rpp.cm.AppendEntries(ctx, request)
}

func GetClusterIdAddr(cluster string) (map[int64]string, error) {
	result := make(map[int64]string)

	servers := strings.Split(cluster, ",")
	for _, sv := range servers {
		idAddr := strings.Split(sv, "=")
		if idAddr[0] == "" {
			continue
		}
		i, err := strconv.Atoi(idAddr[0])
		if err != nil {
			return nil, err
		}
		if len(idAddr) >= 2 {
			result[int64(i)] = idAddr[1]
		} else {
			result[int64(i)] = ""
		}
	}

	return result, nil
}
