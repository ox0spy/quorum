package cmd

import (
	"os"

	"github.com/rumsystem/quorum/internal/pkg/cli"
	"github.com/rumsystem/quorum/internal/pkg/logging"
	"github.com/rumsystem/quorum/internal/pkg/utils"
	"github.com/spf13/cobra"
)

var (
	ReleaseVersion string
	GitCommit      string

	logger = logging.Logger("cmd")

	// flags
	peerName         string
	peerList         cli.AddrList
	configDir        string
	keystoreDir      string
	keystoreName     string
	keystorePassword string
	dataDir          string
	seedDir          string
	backupFile       string
	isWasm           bool
	isDebug          bool
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "quorum",
	Short: "The internet alternatives",
	Long:  `An open source peer-to-peer application infrastructure to offer the internet alternatives in a decentralized and privacy oriented way.`,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if dataDir != "" {
			utils.EnsureDir(dataDir)
		}
		return nil
	},
}

func Execute() {
	if ReleaseVersion == "" {
		ReleaseVersion = "v1.0.0"
	}
	if GitCommit == "" {
		GitCommit = "devel"
	}
	utils.SetGitCommit(GitCommit)

	// set default log level to info
	lvl, err := logging.LevelFromString("info")
	if err != nil {
		logger.Fatal(err)
	}
	logging.SetAllLoggers(lvl)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func configLogger(isDebug bool) {
	logging.SetLogLevel("appsync", "error")
	logging.SetLogLevel("appdata", "error")

	if isDebug == true {
		logging.SetLogLevel("cmd", "debug")
		logging.SetLogLevel("nodesdk", "debug")
		logging.SetLogLevel("handlers", "debug")
		logging.SetLogLevel("crypto", "debug")
		logging.SetLogLevel("network", "debug")
		logging.SetLogLevel("autonat", "debug")
		logging.SetLogLevel("chain", "debug")
		logging.SetLogLevel("dbmgr", "debug")
		logging.SetLogLevel("chainctx", "debug")
		logging.SetLogLevel("syncer", "debug")
		logging.SetLogLevel("producer", "debug")
		logging.SetLogLevel("trxmgr", "debug")
		logging.SetLogLevel("conn", "debug")
		logging.SetLogLevel("rumexchange", "debug")
		logging.SetLogLevel("ssreceiver", "debug")
		logging.SetLogLevel("sssender", "debug")
		//logging.SetLogLevel("group", "debug")
		//logging.SetLogLevel("user", "debug")
		//logging.SetLogLevel("groupmgr", "debug")
		logging.SetLogLevel("ping", "debug")
		logging.SetLogLevel("chan", "debug")
		//logging.SetLogLevel("pubsub", "debug")
	}
}
