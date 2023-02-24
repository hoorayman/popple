package raft

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/hoorayman/popple/internal/statemachine"
)

func TestLeader(t *testing.T) {
	tests := []struct {
		name string
		args int
		want int
	}{
		{
			name: "1 node",
			args: 1,
			want: 1,
		},
		{
			name: "3 nodes",
			args: 3,
			want: 1,
		},
		{
			name: "5 nodes",
			args: 5,
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svcs := MakeCluster(tt.args)
			time.Sleep(3 * time.Second)
			n := CheckLeader(svcs)
			fmt.Println("leader count: ", n)
			if n != tt.want {
				t.Errorf("leader must be elected and be one!")
			}
		})
	}
}

func TestSubmit(t *testing.T) {
	tests := []struct {
		name  string
		nodes int
		args  []statemachine.Cmd
		want  struct{ Key, Val string }
	}{
		{
			name:  "1 node",
			nodes: 1,
			args: []statemachine.Cmd{
				{
					Op:    "set",
					Key:   "mykey",
					Value: "",
				},
				{
					Op:    "set",
					Key:   "mykey",
					Value: "val",
				},
				{
					Op:    "set",
					Key:   "mykey",
					Value: "value",
				},
			},
			want: struct{ Key, Val string }{Key: "mykey", Val: "value"},
		},
		{
			name:  "3 nodes",
			nodes: 3,
			args: []statemachine.Cmd{
				{
					Op:    "set",
					Key:   "mykey",
					Value: "",
				},
				{
					Op:    "set",
					Key:   "mykey",
					Value: "val",
				},
				{
					Op:    "set",
					Key:   "mykey",
					Value: "value",
				},
			},
			want: struct{ Key, Val string }{Key: "mykey", Val: "value"},
		},
		{
			name:  "5 nodes",
			nodes: 5,
			args: []statemachine.Cmd{
				{
					Op:    "set",
					Key:   "mykey",
					Value: "",
				},
				{
					Op:    "set",
					Key:   "mykey",
					Value: "val",
				},
				{
					Op:    "set",
					Key:   "mykey",
					Value: "value",
				},
			},
			want: struct{ Key, Val string }{Key: "mykey", Val: "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svcs := MakeCluster(tt.nodes)
			time.Sleep(3 * time.Second)
			leaderID := GetLeader(svcs)
			for _, cmd := range tt.args {
				payload, _ := json.Marshal(cmd)
				svcs[leaderID].cm.Submit(payload)
			}
			time.Sleep(3 * time.Second)
			for i, server := range svcs {
				val, _ := server.cm.stateMachine.Get(tt.want.Key)
				if val != tt.want.Val {
					t.Errorf("server[%d] fsm state error. key: %s with value: %s but not wanted: %s", i, tt.want.Key, val, tt.want.Val)
				}
			}
		})
	}
}

func TestLeaderDown(t *testing.T) {
	tests := []struct {
		name string
		args int
		want int
	}{
		{
			name: "1 node",
			args: 1,
			want: 0,
		},
		{
			name: "3 nodes",
			args: 3,
			want: 1,
		},
		{
			name: "5 nodes",
			args: 5,
			want: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svcs := MakeCluster(tt.args)
			time.Sleep(3 * time.Second)
			leaderID := GetLeader(svcs)
			_, term, _ := svcs[leaderID].cm.Report()
			svcs[leaderID].Shutdown()
			for _, server := range svcs {
				server.DisconnectPeer(int64(leaderID))
			}
			time.Sleep(3 * time.Second)
			newLeaderID := GetLeader(svcs)
			var newTerm int64 = -1
			if tt.args > 1 {
				_, newTerm, _ = svcs[newLeaderID].cm.Report()
			}
			if (newLeaderID == -1 || newLeaderID == leaderID || newTerm <= term) && tt.want != 0 {
				t.Errorf("no new leader elected after old leader down!")
			}
		})
	}
}

func TestNetworkPartition(t *testing.T) {
	tests := []struct {
		name      string
		args      int
		partition int
	}{
		{
			name:      "3 nodes",
			args:      3,
			partition: 1,
		},
		{
			name:      "5 nodes",
			args:      5,
			partition: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svcs := MakeCluster(tt.args)
			time.Sleep(3 * time.Second)
			leaderID := GetLeader(svcs)
			_, term, _ := svcs[leaderID].cm.Report()

			count := 0
			for _, id := range svcs[leaderID].peerIds {
				if count < tt.partition {
					svcs[leaderID].DisconnectPeer(id)
				}
				count++
			}
			time.Sleep(3 * time.Second)
			newLeaderID := GetLeader(svcs)
			_, newTerm, _ := svcs[newLeaderID].cm.Report()
			if newLeaderID == leaderID || newTerm == term {
				t.Errorf("leader not exchange after network partition!")
			}
		})
	}
}

func TestFollowerDown(t *testing.T) {
	tests := []struct {
		name string
		args int
		down int
	}{
		{
			name: "3 nodes",
			args: 3,
			down: 1,
		},
		{
			name: "5 nodes",
			args: 5,
			down: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svcs := MakeCluster(tt.args)
			time.Sleep(3 * time.Second)
			leaderID := GetLeader(svcs)
			_, term, _ := svcs[leaderID].cm.Report()

			count := 0
			for _, id := range svcs[leaderID].peerIds {
				if count < tt.down {
					svcs[id].Shutdown()
					svcs[leaderID].DisconnectPeer(id)
				}
				count++
			}
			time.Sleep(3 * time.Second)
			newLeaderID := GetLeader(svcs)
			_, newTerm, _ := svcs[newLeaderID].cm.Report()
			if newLeaderID != leaderID || newTerm != term {
				t.Errorf("leader not work after follower down!")
			}
		})
	}
}
