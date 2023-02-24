package raft

import (
	"strconv"
	"strings"
	"time"

	"github.com/hoorayman/popple/internal/conf"
)

func MakeCluster(n int) []*Server {
	conf.Set("dev", true) // set to dev mode

	sl := []string{}
	for i := 0; i < n; i++ {
		sl = append(sl, strconv.Itoa(i))
	}
	cluster := strings.Join(sl, ",")

	ready := make(chan struct{})
	svcs := make([]*Server, n)
	for i := 0; i < n; i++ {
		svcs[i] = NewServer(int64(i), cluster, ready)
		go svcs[i].Serve()
	}
	sidAddr := WaitSettingAddr(svcs)
	for i := 0; i < n; i++ {
		svcs[i].SetServerAddr(sidAddr)
	}
	for i := 0; i < n; i++ {
		svcs[i].WaitConnectToPeers()
	}
	close(ready)

	return svcs
}

func WaitSettingAddr(svcs []*Server) map[int64]string {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	sidAddr := make(map[int64]string)
	for {
		<-ticker.C

		for sid, server := range svcs {
			sidAddr[int64(sid)] = server.GetListenAddr()
		}

		ready := true
		for sid := range svcs {
			if sidAddr[int64(sid)] == "" {
				ready = false
			}
		}
		if ready {
			return sidAddr
		}
	}
}

// count leader
func CheckLeader(svcs []*Server) int {
	n := 0
	for _, server := range svcs {
		_, _, leader := server.cm.Report()
		if leader {
			n++
		}
	}

	return n
}

// get leader id
func GetLeader(svcs []*Server) int {
	for i, server := range svcs {
		_, _, leader := server.cm.Report()
		if leader {
			return i
		}
	}

	return -1
}
