package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"phenix/api/config"
	_ "phenix/api/scorch"
	"phenix/store"
	"phenix/util"
	"phenix/util/common"
	"phenix/web"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	phenixBase       string
	minimegaBase     string
	hostnameSuffixes string
	storeEndpoint    string
	errFile          string
)

var rootCmd = &cobra.Command{
	Use:   "phenix",
	Short: "A cli application for phēnix",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		common.PhenixBase = viper.GetString("base-dir.phenix")
		common.MinimegaBase = viper.GetString("base-dir.minimega")
		common.HostnameSuffixes = viper.GetString("hostname-suffixes")

		var (
			endpoint = viper.GetString("store.endpoint")
			errFile  = viper.GetString("log.error-file")
			errOut   = viper.GetBool("log.error-stderr")
		)

		common.ErrorFile = errFile
		common.StoreEndpoint = endpoint

		if err := store.Init(store.Endpoint(endpoint)); err != nil {
			return fmt.Errorf("initializing storage: %w", err)
		}

		if err := util.InitFatalLogWriter(errFile, errOut); err != nil {
			return fmt.Errorf("unable to initialize fatal log writer: %w", err)
		}

		if err := config.Init(); err != nil {
			return fmt.Errorf("unable to initialize default configs: %w", err)
		}

		return nil
	},
	PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
		util.CloseLogWriter()
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
	SilenceUsage: true, // don't print help when subcommands return an error
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	uid, home := getCurrentUserInfo()
	var homePath string

	if uid != "0" {
		homePath = fmt.Sprintf("%s/.config/phenix", home)
	}

	viper.SetEnvPrefix("PHENIX")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv() // read in environment variables that match

	viper.SetConfigName("config")

	// Config paths - first look in current directory, then home directory (if
	// discoverable), then finally global config directory.
	viper.AddConfigPath(".")

	if homePath != "" {
		viper.AddConfigPath(homePath)
	}

	viper.AddConfigPath("/etc/phenix")

	// If a config file is found, read it in.
	viper.ReadInConfig()

	viper.SetConfigName("users")
	viper.AddConfigPath(".")

	if homePath != "" {
		viper.AddConfigPath(homePath)
	}

	viper.AddConfigPath("/etc/phenix")

	// If a users config file is found, merge it in.
	if err := viper.MergeInConfig(); err == nil {
		viper.WatchConfig()

		viper.OnConfigChange(func(e fsnotify.Event) {
			if strings.TrimSuffix(filepath.Base(e.Name), filepath.Ext(e.Name)) == "users" {
				web.ConfigureUsers(viper.GetStringSlice("ui.users"))
			}
		})
	}

	rootCmd.PersistentFlags().StringVar(&phenixBase, "base-dir.phenix", "/phenix", "base phenix directory")
	rootCmd.PersistentFlags().StringVar(&minimegaBase, "base-dir.minimega", "/tmp/minimega", "base minimega directory")
	rootCmd.PersistentFlags().StringVar(&hostnameSuffixes, "hostname-suffixes", "-minimega,-phenix", "hostname suffixes to strip")
	rootCmd.PersistentFlags().Bool("log.error-stderr", true, "log fatal errors to STDERR")

	if uid == "0" {
		os.MkdirAll("/etc/phenix", 0755)
		os.MkdirAll("/var/log/phenix", 0755)

		rootCmd.PersistentFlags().StringVar(&storeEndpoint, "store.endpoint", "bolt:///etc/phenix/store.bdb", "endpoint for storage service")
		rootCmd.PersistentFlags().StringVar(&errFile, "log.error-file", "/var/log/phenix/error.log", "log fatal errors to file")

		common.LogFile = "/var/log/phenix/phenix.log"
	} else {
		rootCmd.PersistentFlags().StringVar(&storeEndpoint, "store.endpoint", fmt.Sprintf("bolt://%s/.phenix.bdb", home), "endpoint for storage service")
		rootCmd.PersistentFlags().StringVar(&errFile, "log.error-file", fmt.Sprintf("%s/.phenix.err", home), "log fatal errors to file")

		common.LogFile = fmt.Sprintf("%s/.phenix.log", home)
	}

	viper.BindPFlags(rootCmd.PersistentFlags())
}

func getCurrentUserInfo() (string, string) {
	u, err := user.Current()
	if err != nil {
		panic("unable to determine current user: " + err.Error())
	}

	var (
		uid  = u.Uid
		home = u.HomeDir
		sudo = os.Getenv("SUDO_USER")
	)

	// Only trust `SUDO_USER` env variable if we're currently running as root and,
	// if set, use it to lookup the actual user that ran the sudo command.
	if u.Uid == "0" && sudo != "" {
		u, err := user.Lookup(sudo)
		if err != nil {
			panic("unable to lookup sudo user: " + err.Error())
		}

		// `uid` and `home` will now reflect the user ID and home directory of the
		// actual user that ran the sudo command.
		uid = u.Uid
		home = u.HomeDir
	}

	return uid, home
}
