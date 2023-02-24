package statemachine

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	"github.com/tidwall/buntdb"

	"github.com/hoorayman/popple/internal/conf"
)

const (
	defaultKVDBFileName = "data"
	LastAppliedKey      = "kvdbLastApplied"
)

type KVDB struct {
	db *buntdb.DB
}

func NewKVDB() (IKVDB, error) {
	var db *buntdb.DB

	if conf.GetBool("dev") {
		b, err := buntdb.Open(":memory:")
		if err != nil {
			return nil, err
		}
		db = b
	} else {
		b, err := buntdb.Open(filepath.Join(conf.GetString("data-dir"), defaultKVDBFileName))
		if err != nil {
			return nil, err
		}
		db = b
	}

	return &KVDB{db: db}, nil
}

func (k *KVDB) Set(key, value string) error {
	return k.db.Update(func(tx *buntdb.Tx) error {
		_, _, err := tx.Set(key, value, nil)
		return err
	})
}

func (k *KVDB) Get(key string) (string, error) {
	result := ""
	err := k.db.View(func(tx *buntdb.Tx) error {
		val, err := tx.Get(key)
		if err != nil {
			return err
		}
		result = val

		return nil
	})

	return result, err
}

func (k *KVDB) Delete(key string) error {
	return k.db.Update(func(tx *buntdb.Tx) error {
		_, err := tx.Delete(key)
		return err
	})
}

type Cmd struct {
	Op    string `json:"op" binding:"required"`
	Key   string `json:"key"`
	Value string `json:"value"`
}

var SupportOps map[string]struct{} = map[string]struct{}{"set": {}, "del": {}}

func (k *KVDB) Call(command []byte) error {
	cmd := Cmd{}
	err := json.Unmarshal(command, &cmd)
	if err != nil {
		return err
	}

	if _, ok := SupportOps[cmd.Op]; !ok {
		return fmt.Errorf("unsupported operation")
	}
	if cmd.Op == "set" {
		return k.Set(cmd.Key, cmd.Value)
	}
	if cmd.Op == "del" {
		return k.Delete(cmd.Key)
	}

	return nil
}

func (k *KVDB) CommandCheck(command []byte) bool {
	cmd := Cmd{}
	err := json.Unmarshal(command, &cmd)
	if err != nil {
		return false
	}

	if _, ok := SupportOps[cmd.Op]; !ok {
		return false
	}

	return true
}
