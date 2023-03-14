package app

import (
	"fmt"
	"io/ioutil"
	"math/rand"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
	"github.com/hoorayman/popple/internal/statemachine"
	"github.com/spf13/cobra"
)

const (
	opPath        = "/submit"
	fetchPath     = "/fetch/%s"
	defaultScheme = "http://"
	tmpfile       = "/tmp/.popleader"
)

func init() {
	getCmd.Flags().StringVarP(&servers, "servers", "s", "", `Remote server addresses. The format is address pairs, comma separation. For example, "127.0.0.1:8876,192.168.0.2:8889,192.168.0.56:8080"`)
	setCmd.Flags().StringVarP(&servers, "servers", "s", "", `Remote server addresses. The format is address pairs, comma separation. For example, "127.0.0.1:8876,192.168.0.2:8889,192.168.0.56:8080"`)
	delCmd.Flags().StringVarP(&servers, "servers", "s", "", `Remote server addresses. The format is address pairs, comma separation. For example, "127.0.0.1:8876,192.168.0.2:8889,192.168.0.56:8080"`)
	getCmd.MarkFlagRequired("servers")
	setCmd.MarkFlagRequired("servers")
	delCmd.MarkFlagRequired("servers")
}

var servers string
var getCmd = &cobra.Command{
	Use:   "get",
	Short: "Get a key from Popple cluster",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cli := NewClient(servers)
		val, err := cli.FetchKey(args[0])
		if err != nil {
			return err
		}
		fmt.Println(val)

		return nil
	},
}

var setCmd = &cobra.Command{
	Use:   "set",
	Short: "Set a key to Popple cluster",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		cli := NewClient(servers)
		res, err := cli.SetKey(args[0], args[1])
		if err != nil {
			return err
		}
		fmt.Println(res)

		return nil
	},
}

var delCmd = &cobra.Command{
	Use:   "del",
	Short: "Delete a key from Popple cluster",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cli := NewClient(servers)
		res, err := cli.DelKey(args[0])
		if err != nil {
			return err
		}
		fmt.Println(res)

		return nil
	},
}

type client struct {
	servers string
}

func NewClient(servers string) Client {
	return &client{
		servers: servers,
	}
}

func (cli *client) FetchKey(key string) (string, error) {
	svrs := strings.Split(cli.servers, ",")
	remote := svrs[rand.Intn(len(svrs))]

	rclient := resty.New()
	resp, err := rclient.R().
		Get(defaultScheme + remote + fmt.Sprintf(fetchPath, key))
	if err != nil {
		return "", err
	}

	return string(resp.Body()), nil
}

type opResult struct {
	Code       int    `json:"code"`
	Msg        string `json:"msg"`
	Err        error  `json:"-"`
	StatusCode int    `json:"-"`
	Result     string `json:"-"`
	Remote     string `json:"-"`
}

func (cli *client) SetKey(key, val string) (string, error) {
	return cli.op("set", key, val)
}

func (cli *client) DelKey(key string) (string, error) {
	return cli.op("del", key, "")
}

func (cli *client) op(op, key, val string) (string, error) {
	svrs := strings.Split(cli.servers, ",")
	content, _ := ioutil.ReadFile(tmpfile)
	remote := string(content)
	rclient := resty.New()
	request := statemachine.Cmd{
		Op:    op,
		Key:   key,
		Value: val,
	}

	inCluster := false
	for _, svr := range svrs {
		if remote == svr {
			inCluster = true
			break
		}
	}
	if inCluster {
		firstTry := opResult{}
		resp, err := rclient.R().
			SetHeader("Accept", "application/json").
			SetBody(request).
			SetResult(&firstTry).
			Post(defaultScheme + remote + opPath)
		if err == nil && resp.StatusCode() == 200 && firstTry.Code == 0 {
			return string(resp.Body()), nil
		}
	}

	signals := []chan interface{}{}
	for _, svr := range svrs {
		signals = append(signals, func() chan interface{} {
			result := make(chan interface{})

			go func(remote string) {
				defer close(result)

				try := opResult{}
				resp, err := rclient.R().
					SetHeader("Accept", "application/json").
					SetBody(request).
					SetResult(&try).
					Post(defaultScheme + remote + opPath)
				try.Err = err
				try.Remote = remote
				if resp != nil {
					try.StatusCode = resp.StatusCode()
					try.Result = string(resp.Body())
				}

				result <- try
			}(svr)

			return result
		}())
	}

	siganal := orChannel(signals...)
	timeout := time.After(10 * time.Second)
Loop:
	for {
		select {
		case sig, ok := <-siganal:
			if !ok {
				break Loop
			}
			res := sig.(opResult)
			if res.Err == nil && res.StatusCode == 200 && res.Code == 0 {
				ioutil.WriteFile(tmpfile, []byte(res.Remote), 0644)
				return res.Result, nil
			}
		case <-timeout:
			break Loop
		}
	}

	return "", fmt.Errorf("not accepted")
}

func orChannel(in ...chan interface{}) chan interface{} {
	if len(in) == 0 {
		return nil
	}
	if len(in) == 1 {
		return in[0]
	}

	result := make(chan interface{})
	go func() {
		defer close(result)
		c1, c2 := in[0], orChannel(in[1:]...)

		for {
			select {
			case v, ok := <-c1:
				if !ok {
					c1 = nil
				} else {
					result <- v
				}
			case v, ok := <-c2:
				if !ok {
					c2 = nil
				} else {
					result <- v
				}
			}
			if c1 == nil && c2 == nil {
				break
			}
		}
	}()

	return result
}
