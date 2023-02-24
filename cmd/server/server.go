package server

import (
	"log"

	"github.com/spf13/cobra"

	"github.com/hoorayman/popple/internal/conf"
	"github.com/hoorayman/popple/internal/raft"
)

func init() {
	// make flags
	ServerCmd.Flags().Bool("dev", false, `Enable development mode. In this mode, Popple runs in-memory and starts. As the name implies, do not run "dev" mode in production. The default is false.`)
	ServerCmd.Flags().StringP("config", "c", "", "Path to a configuration file.")
	ServerCmd.Flags().StringP("data-dir", "", "./", "Path to raft log and database file dir.")
	ServerCmd.Flags().Int64P("sid", "", 0, "Current Server ID. default is 0")
	ServerCmd.Flags().StringP("cluster", "", "", `Define cluster servers. The format is sid:address pairs, comma separation. For example, "0=127.0.0.1:8876,1=192.168.0.2:8889,2=192.168.0.56:8080"`)
	// make config
	conf.BindPFlag("dev", ServerCmd.Flags().Lookup("dev"))
	conf.BindPFlag("config", ServerCmd.Flags().Lookup("config"))
	conf.BindPFlag("data-dir", ServerCmd.Flags().Lookup("data-dir"))
	conf.BindPFlag("sid", ServerCmd.Flags().Lookup("sid"))
	conf.BindPFlag("cluster", ServerCmd.Flags().Lookup("cluster"))
}

var ServerCmd = &cobra.Command{
	Use:   "server",
	Short: "Start a Popple server",
	RunE: func(cmd *cobra.Command, args []string) error {
		conf.InitConfig(conf.SetEnv(), conf.SetConfigFile(conf.GetString("config")))
		if conf.GetBool("dev") {
			log.Print("DevMode: ", conf.GetBool("dev"), conf.GetString("config"))
		}
		ready := make(chan struct{})
		server := raft.NewServer(conf.GetInt64("sid"), conf.GetString("cluster"), ready)
		go func() {
			server.WaitConnectToPeers()
			close(ready)
		}()
		server.Serve()

		return nil
	},
}
