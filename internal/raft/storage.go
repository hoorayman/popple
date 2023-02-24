package raft

import (
	"bytes"
	"io"
	"log"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/fxamacker/cbor/v2"
	"golang.org/x/sys/unix"

	"github.com/hoorayman/popple/internal/conf"
)

const (
	raftLogFileName  = "rlog"
	defaultAllocSize = 1024 * 1024 // 1M
)

type RaftPersistent struct {
	fd       int
	file     *os.File
	data     []byte
	used     *int64
	term     *int64
	votedfor *int64
}

func (rp *RaftPersistent) InitAndLoadLog(dest *[]LogEntry) error {
	file, err := os.OpenFile(filepath.Join(conf.GetString("data-dir"), raftLogFileName), os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return err
	}
	rp.file = file
	rp.fd = int(file.Fd())

	info, err := file.Stat()
	if err != nil {
		return err
	}
	size := info.Size()
	newFile := false
	if size == 0 {
		newFile = true
		err := file.Truncate(defaultAllocSize)
		if err != nil {
			return err
		}
		size = defaultAllocSize
	}

	data, err := unix.Mmap(int(file.Fd()), 0, int(size), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return err
	}
	rp.data = data
	rp.used = (*int64)(unsafe.Pointer(&data[0]))
	rp.term = (*int64)(unsafe.Pointer(&data[8]))
	rp.votedfor = (*int64)(unsafe.Pointer(&data[16]))
	if newFile {
		*rp.used = 24
		*rp.votedfor = -1
	}

	buf := bytes.NewReader(data[24:(*rp.used)])
	g := cbor.NewDecoder(buf)
	for {
		var x LogEntry
		err = g.Decode(&x)
		if err != nil {
			break
		}
		*dest = append(*dest, LogEntry{Command: x.Command, Term: x.Term})
	}
	if err != io.EOF {
		log.Fatal(err)
	}

	return nil
}

func (rp *RaftPersistent) AppendLog(rollback, entries []LogEntry) error {
	rbuf := bytes.NewBuffer(nil)
	rolle := cbor.NewEncoder(rbuf)
	for _, entry := range rollback {
		err := rolle.Encode(entry)
		if err != nil {
			return err
		}
	}
	*rp.used -= int64(rbuf.Len())

	if *rp.used < 24 {
		*rp.used = 24
	}
	currentFileSize := *rp.used
	buf := bytes.NewBuffer(nil)
	g := cbor.NewEncoder(buf)
	for _, entry := range entries {
		err := g.Encode(entry)
		if err != nil {
			return err
		}
	}

	if currentFileSize+int64(buf.Len()) > int64(len(rp.data)) {
		newSize := currentFileSize + int64(buf.Len()) + defaultAllocSize
		err := rp.file.Truncate(newSize)
		if err != nil {
			return err
		}

		unix.Munmap(rp.data)
		data, err := unix.Mmap(rp.fd, 0, int(newSize), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
		if err != nil {
			return err
		}
		rp.data = data
		rp.used = (*int64)(unsafe.Pointer(&data[0]))
		rp.term = (*int64)(unsafe.Pointer(&data[8]))
		rp.votedfor = (*int64)(unsafe.Pointer(&data[16]))
	}

	copy(rp.data[currentFileSize:], buf.Bytes())
	newUsedSize := currentFileSize + int64(buf.Len())
	*rp.used = newUsedSize

	err := unix.Msync(rp.data, unix.MS_SYNC)
	if err != nil {
		return err
	}

	return nil
}

func (rp *RaftPersistent) LogLen() int64 {
	return *rp.used
}

func (rp *RaftPersistent) SetTerm(term int64) error {
	*rp.term = term
	return unix.Msync(rp.data, unix.MS_SYNC)
}

func (rp *RaftPersistent) GetTerm() int64 {
	return *rp.term
}

func (rp *RaftPersistent) SetVotedFor(votefor int64) error {
	*rp.votedfor = votefor
	return unix.Msync(rp.data, unix.MS_SYNC)
}

func (rp *RaftPersistent) GetVotedFor() int64 {
	return *rp.votedfor
}
