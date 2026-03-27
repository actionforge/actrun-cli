package cmd

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/actionforge/actrun-cli/agent"
	"github.com/actionforge/actrun-cli/build"
	"github.com/actionforge/actrun-cli/core"
	"github.com/actionforge/actrun-cli/sessions"
	"github.com/actionforge/actrun-cli/utils"
	u "github.com/actionforge/actrun-cli/utils"

	"github.com/fatih/color"
	"github.com/inconshreveable/mousetrap"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	// initialize all nodes
	_ "github.com/actionforge/actrun-cli/nodes"
)

var (
	flagConfigFile         string
	flagConcurrency        string
	flagSessionToken       string
	flagEnvFile            string
	flagCreateDebugSession bool
	flagLocal              bool
	flagLocalGhServer      bool

	finalConfigFile         string
	finalConcurrency        string
	finalSessionToken       string
	finalConfigValueSource  string
	finalCreateDebugSession bool
	finalLocal              bool
	finalLocalGhServer      bool

	finalGraphFile string
	finalGraphArgs []string
)

var cmdRoot = &cobra.Command{
	Use:     "actrun [filename|url] [flags]",
	Short:   "actrun is a tool for running action graphs.",
	Version: build.GetFulllVersionInfo(),
	Args:    cobra.ArbitraryArgs,
	Run:     cmdRootRun,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		if flagEnvFile != "" {
			err := utils.LoadEnvFile(flagEnvFile)
			if err != nil {
				// Return this error too, so Cobra knows something went wrong
				return err
			}
			utils.LogOut.Debugf("loaded .env file from %s\n", flagEnvFile)
		}
		return nil
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		// env_file has already been processed, so no need for the variable, but
		// calling GetConfigValue to ensure that LogOut.Debugf is also printed here.
		_, _ = u.ResolveCliParam("env_file", u.ResolveCliParamOpts{
			Flag:      true,
			FlagValue: flagEnvFile,
			Env:       false,
			Optional:  true,
			ActPrefix: false,
		})

		finalConfigFile, _ = u.ResolveCliParam("config_file", u.ResolveCliParamOpts{
			Flag:      true,
			FlagValue: flagConfigFile,
			Env:       true,
			Optional:  true,
			ActPrefix: true,
		})

		finalConcurrency, _ = u.ResolveCliParam("concurrency", u.ResolveCliParamOpts{
			Flag:      true,
			FlagValue: flagConcurrency,
			Env:       true,
			Optional:  true,
			ActPrefix: true,
		})

		defaultGraphFile, _ := u.ResolveCliParam("graph_file", u.ResolveCliParamOpts{
			Flag:      false, // only provided via env, config, or positional arg
			Env:       true,
			Optional:  true,
			ActPrefix: true,
		})

		finalSessionToken, finalConfigValueSource = u.ResolveCliParam("session_token", utils.ResolveCliParamOpts{
			Flag:      true,
			Env:       true,
			Optional:  true,
			FlagValue: flagSessionToken,
			ActPrefix: true,
		})

		finalCreateDebugSessionStr, _ := u.ResolveCliParam("create_debug_session", u.ResolveCliParamOpts{
			Flag:      true,
			FlagValue: fmt.Sprintf("%v", flagCreateDebugSession),
			Env:       true,
			Optional:  true,
			ActPrefix: true,
		})
		finalCreateDebugSession = finalCreateDebugSessionStr == "true" || finalCreateDebugSessionStr == "1"

		finalLocal = flagLocal
		finalLocalGhServer = flagLocalGhServer

		// the block below is used to distinguish between implicit graph files (eg if defined in an env var) + graph flags
		// vs explicit graph file (eg provided by positional arg) + graph flags.

		// first we check if the user explicitly used "--" to separate args
		// ArgsLenAtDash returns -1 if "--" was not provided.
		// If it returns 0, it means "--" was the FIRST argument (e.g. "actrun -- some_arg")
		dashIdx := cmd.ArgsLenAtDash()
		if dashIdx == 0 {
			// the user explicitly said "Everything here is an arg for the graph"
			// eg `actrun -- foo` or `actrun -- --debug`
			finalGraphFile = defaultGraphFile
			finalGraphArgs = args
		} else if dashIdx > 0 {
			// user provided arguments before "--". the first is the graph file,
			// any subsequent args before "--" (and all after) are passed to the graph
			finalGraphFile = args[0]
			finalGraphArgs = args[1:]
		} else {
			// no "--" was used. That means we assume the first argument is the graph file, and the rest are graph args.

			if len(args) == 0 {
				// no args at all
				finalGraphFile = defaultGraphFile
				finalGraphArgs = []string{}
			} else {
				// we expect the first arg to be the graph file, rest are graph args
				firstArg := args[0]
				finalGraphFile = firstArg
				finalGraphArgs = args[1:]
			}
		}

		if finalCreateDebugSession && finalSessionToken != "" {
			return errors.New("both --session-token and --create-debug-session cannot be used together")
		} else if finalCreateDebugSession && finalGraphFile == "" {
			return errors.New("when using --create-debug-session, a graph file must be specified")
		}

		if finalLocal && finalSessionToken != "" {
			return errors.New("--local and --session-token cannot be used together")
		}
		if finalLocal && finalCreateDebugSession {
			return errors.New("--local and --create-debug-session cannot be used together")
		}

		return nil
	},
}

func cmdRootRun(cmd *cobra.Command, args []string) {

	utils.SetConcurrencyEnabled(finalConcurrency == "" || finalConcurrency == "true" || finalConcurrency == "1")

	// start a local WS server for local connections (eg the vscode extension)
	if finalLocal {
		err := sessions.RunLocalMode(finalConfigFile)
		if err != nil {
			utils.LogErr.Print(err.Error())
			os.Exit(1)
		}
		return
	}

	// if we still have no graph file, go to Session Mode
	if finalGraphFile == "" || finalCreateDebugSession {
		trapfn := func() {
			if mousetrap.StartedByExplorer() {
				fmt.Print("\nPress Enter to exit...")
				_, _ = bufio.NewReader(os.Stdin).ReadBytes('\n')
			}
		}

		err := sessions.RunSessionMode(finalConfigFile, finalGraphFile, finalSessionToken, finalConfigValueSource)
		if err != nil {
			utils.LogErr.Print(err.Error())
			trapfn()
			os.Exit(1)
		}
		trapfn()
		return
	}

	var err error
	opts := core.RunOpts{
		ConfigFile:      finalConfigFile,
		OverrideSecrets: nil,
		OverrideInputs:  nil,
		Args:            finalGraphArgs,
		LocalGhServer:   finalLocalGhServer,
	}

	// If running under an agent (BUILD_JOB_ID + BUILD_SERVER_URL + BUILD_AGENT_TOKEN),
	// create a node state reporter that tracks active nodes and sends updates
	// to the orchestrator in real time.
	jobID := os.Getenv("BUILD_JOB_ID")
	serverURL := os.Getenv("BUILD_SERVER_URL")
	agentToken := os.Getenv("BUILD_AGENT_TOKEN")
	if jobID != "" && serverURL != "" && agentToken != "" {
		client := agent.NewClient(serverURL, agentToken)
		reporter := agent.NewNodeReporter(client, jobID)
		defer reporter.Close()
		opts.NodeStateCallback = reporter.OnNodeState
	}

	if core.IsSharedGraphURL(finalGraphFile) {
		err = core.RunGraphFromURL(context.Background(), finalGraphFile, opts, nil)
	} else {
		err = core.RunGraphFromFile(context.Background(), finalGraphFile, opts, nil)
	}

	if err != nil {
		core.PrintError(finalGraphFile, err)
		os.Exit(1)
	}
}

func Execute() {
	defer core.RecoverHandler(true)

	if err := cmdRoot.Execute(); err != nil {
		os.Exit(1)
	}
}

// Allow both variations, both --env-file and --env_file will work
func normalizeFlag(f *pflag.FlagSet, name string) pflag.NormalizedName {
	return pflag.NormalizedName(strings.ReplaceAll(name, "_", "-"))
}

func init() {
	flag.Usage = func() {
		fmt.Print("\n")
		fmt.Fprintf(os.Stderr, "Usage: %s", os.Args[0])
		flag.VisitAll(func(f *flag.Flag) {
			defValue := f.DefValue
			if defValue == "" {
				defValue = "'...'"
			}
			fmt.Fprintf(os.Stderr, " -%s=%s", f.Name, defValue)
		})
		fmt.Print("\n\n")
	}

	// Enable backwards compatibility: allow both --env-file and --env_file
	// SetGlobalNormalizationFunc propagates to all subcommands, not just root.
	cmdRoot.SetGlobalNormalizationFunc(normalizeFlag)

	cmdRoot.PersistentFlags().StringVar(&flagEnvFile, "env-file", "", "Absolute path to an env file (.env) to load before execution")

	cmdRoot.Flags().StringVar(&flagConfigFile, "config-file", "", "The config file to use")
	cmdRoot.Flags().StringVar(&flagConcurrency, "concurrency", "", "Enable or disable concurrency")
	cmdRoot.Flags().StringVar(&flagSessionToken, "session-token", "", "The session token from your browser")
	cmdRoot.Flags().BoolVar(&flagCreateDebugSession, "create-debug-session", false, "Create a debug session by connecting to the web app")
	cmdRoot.Flags().BoolVar(&flagLocal, "local", false, "Start a local WebSocket server for direct editor connection")
	cmdRoot.Flags().BoolVar(&flagLocalGhServer, "local-gh-server", false, "Start a local server mimicking GitHub Actions artifact, cache, and OIDC services")

	// disable interspersed flag parsing to allow passing arbitrary flags to graphs.
	// it stops cobra from parsing flags once it hits positional argument
	// or the "--" terminator.
	cmdRoot.Flags().SetInterspersed(false)

	color.NoColor = os.Getenv("ACT_NOCOLOR") == "true"
}

func init() {
	cobra.MousetrapHelpText = ""
}
