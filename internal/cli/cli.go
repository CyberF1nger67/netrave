// SPDX-License-Identifier: GPL-3.0-only

// Package cli implements the netrave command line interface. It is deliberately
// thin: lifecycle operations delegate to init adapters, installation to the
// installer package and the HTTP service to the existing server package.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	"netrave/internal/config"
	"netrave/internal/daemon"
	"netrave/internal/database"
	initpkg "netrave/internal/init"
	"netrave/internal/installer"
	"netrave/internal/network"
	"netrave/internal/platform"
	"netrave/internal/server"
	"netrave/internal/version"
)

const usage = `Usage: netrave <command> [options]

Commands:
  serve       Run the HTTP server in the foreground (default)
  daemon      Run the supervisor daemon (foreground by default)
  status      Show daemon/service status
  start       Start the installed service
  stop        Stop the installed service
  restart     Restart the installed service
  install     Install binary, configuration and service
  uninstall   Remove the service and program files
  doctor      Print platform and dependency diagnostics
  netdiag     Show TAP, bridge, nftables and br_netfilter state (JSON)
  logs        Show daemon logs
  version     Print version information
  help        Show this help

Run "netrave <command> -h" for command-specific flags.
`

// Run executes the CLI and returns a process exit code.
func Run(args []string) int {
	if len(args) == 0 {
		cfg, err := config.ParseArgs(nil)
		if err != nil {
			return fail(err)
		}
		return runServer(cfg)
	}
	command := args[0]
	if strings.HasPrefix(command, "-") {
		cfg, err := config.ParseArgs(args)
		if err != nil {
			return fail(err)
		}
		return runServer(cfg)
	}
	rest := args[1:]
	switch command {
	case "serve":
		cfg, err := config.ParseArgs(rest)
		if err != nil {
			return fail(err)
		}
		return runServer(cfg)
	case "daemon":
		return runDaemon(rest)
	case "status":
		return runStatus(rest)
	case "start", "stop", "restart":
		return runService(command, rest)
	case "install":
		return runInstall(rest)
	case "uninstall":
		return runUninstall(rest)
	case "doctor":
		return runDoctor(rest)
	case "netdiag":
		return runNetDiag(rest)
	case "logs":
		return runLogs(rest)
	case "version", "--version", "-version":
		fmt.Printf("netrave %s (commit %s, built %s)\n", version.Version, version.Commit, version.Date)
		return 0
	case "help", "--help", "-h":
		fmt.Print(usage)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", command, usage)
		return 2
	}
}

func runServer(cfg *config.Config) int {
	if err := server.Run(cfg); err != nil {
		return fail(err)
	}
	return 0
}

func runDaemon(args []string) int {
	foreground := true
	filtered := make([]string, 0, len(args))
	for _, arg := range args {
		switch arg {
		case "--foreground", "-foreground":
			foreground = true
		case "--background", "-background":
			foreground = false
		default:
			filtered = append(filtered, arg)
		}
	}
	cfg, err := config.ParseArgs(filtered)
	if err != nil {
		return fail(err)
	}
	if err := cfg.Validate(); err != nil {
		return fail(err)
	}
	if !foreground {
		if err := daemon.Background(cfg); err != nil {
			return fail(err)
		}
		return 0
	}
	if err := daemon.Run(context.Background(), cfg); err != nil {
		return fail(err)
	}
	return 0
}

func serviceFlags(name string) (*flag.FlagSet, *string, *string, *string, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "/", "filesystem root (for packaging/tests)")
	initOverride := fs.String("init", "", "init system: systemd, openrc or runit")
	configPath := fs.String("config", installer.DefaultConfigPath, "configuration file")
	service := fs.String("service", "netrave", "service name")
	return fs, root, initOverride, configPath, service
}

func serviceConfig(cfgPath, service, dataDir, logFile, socket, binary string) initpkg.ServiceConfig {
	return initpkg.ServiceConfig{
		Name: service, Binary: binary, ConfigFile: cfgPath, DataDir: dataDir,
		LogFile: logFile, Socket: socket, User: "root", Group: "root",
	}
}

// installedConfigPath returns the configuration file of the host installation,
// or "" when netrave is not installed or the file is not readable. Development
// runs and unprivileged commands therefore keep using the built-in defaults.
func installedConfigPath() string {
	path := installer.DefaultConfigPath
	file, err := os.Open(path)
	if err != nil {
		return ""
	}
	_ = file.Close()
	return path
}

// loadCommandConfig loads the requested configuration file, the installed one,
// or the built-in defaults. CLI commands must use the same configuration as the
// service: otherwise `netrave logs` looks for the daemon socket in the
// development default under the data directory instead of /run/netrave.
func loadCommandConfig(explicit string) (*config.Config, error) {
	if explicit == "" {
		explicit = os.Getenv("NETRAVE_CONFIG")
	}
	if explicit == "" {
		explicit = installedConfigPath()
	}
	if explicit == "" {
		return config.ParseArgs(nil)
	}
	return config.Load(explicit)
}

func hasConfigFlag(args []string) bool {
	for _, arg := range args {
		if arg == "-config" || arg == "--config" ||
			strings.HasPrefix(arg, "-config=") || strings.HasPrefix(arg, "--config=") {
			return true
		}
	}
	return false
}

func adapterFor(initOverride, root string, cfg initpkg.ServiceConfig) (initpkg.Adapter, error) {
	kind := platform.InitKind(strings.ToLower(initOverride))
	if kind == "" {
		detected := platform.DetectInit(root, "")
		if detected == platform.InitUnknown {
			return nil, errors.New("init system not detected; pass --init systemd|openrc|runit")
		}
		kind = detected
	}
	return initpkg.New(kind, root, initpkg.ExecRunner{}, cfg)
}

func runStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	jsonOutput := fs.Bool("json", false, "print JSON")
	socket := fs.String("socket", "", "daemon socket path")
	configPath := fs.String("config", "", "configuration file (default: installed config)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	path := *socket
	cfg, cfgErr := loadCommandConfig(*configPath)
	if path == "" && cfgErr == nil {
		path = cfg.SocketPath
	}
	response, err := daemon.Call(path, daemon.Request{Command: "status"})
	if err != nil {
		if cfgErr == nil {
			cfgPath := cfg.ConfigPath
			if cfgPath == "" {
				cfgPath = installer.DefaultConfigPath
			}
			service := serviceConfig(cfgPath, "netrave", cfg.DataDir, cfg.LogFile, cfg.SocketPath, "/usr/local/bin/netrave")
			if adapter, adapterErr := adapterFor("", "/", service); adapterErr == nil {
				status, _ := adapter.Status()
				fmt.Printf("daemon is not reachable: %v\nservice: %s\n", err, status)
				if strings.Contains(strings.ToLower(status), "active") || strings.Contains(strings.ToLower(status), "run") {
					return 0
				}
				return 1
			}
		}
		if cfgErr != nil {
			fmt.Fprintf(os.Stderr, "daemon is not reachable: %v (configuration: %v)\n", err, cfgErr)
			return 1
		}
		fmt.Fprintf(os.Stderr, "daemon is not reachable: %v\n", err)
		return 1
	}
	if *jsonOutput {
		data, _ := json.MarshalIndent(response, "", "  ")
		fmt.Println(string(data))
		return 0
	}
	fmt.Printf("state: %s\npid: %d\nchild pid: %d\nuptime: %ds\nrestarts: %d\nversion: %s\n",
		response.State, response.PID, response.ChildPID, response.Uptime, response.Restarts, response.Version)
	return 0
}

func runService(command string, args []string) int {
	fs, root, initOverride, configPath, serviceName := serviceFlags(command)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fail(err)
	}
	service := serviceConfig(*configPath, *serviceName, cfg.DataDir, cfg.LogFile, cfg.SocketPath, "/usr/local/bin/netrave")
	adapter, err := adapterFor(*initOverride, *root, service)
	if err != nil {
		return fail(err)
	}
	var actionErr error
	switch command {
	case "start":
		actionErr = adapter.Start()
	case "stop":
		actionErr = adapter.Stop()
	case "restart":
		actionErr = adapter.Restart()
	}
	if actionErr != nil {
		return fail(actionErr)
	}
	status, _ := adapter.Status()
	fmt.Printf("%s: %s\n", command, status)
	return 0
}

func runInstall(args []string) int {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "/", "filesystem root")
	initOverride := fs.String("init", "", "init system override")
	binarySource := fs.String("source", "", "binary to install (default: current executable)")
	binaryPath := fs.String("binary", "/usr/local/bin/netrave", "installed binary path")
	configPath := fs.String("config", "", "configuration file (default: detected)")
	dataDir := fs.String("data", "/var/lib/netrave", "data directory")
	logDir := fs.String("log-dir", "/var/log/netrave", "log directory")
	socket := fs.String("socket", "/run/netrave/daemon.sock", "daemon socket")
	addr := fs.String("addr", ":8080", "HTTP listen address")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	result, err := installer.Install(context.Background(), installer.Options{
		Root: *root, InitSystem: *initOverride, BinarySource: *binarySource,
		BinaryPath: *binaryPath, ConfigPath: *configPath, DataDir: *dataDir,
		LogDir: *logDir, SocketPath: *socket, Addr: *addr,
	})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("installed %s %s\n", result.Platform.OSRelease.Pretty, version.Version)
	fmt.Printf("init: %s\nbinary: %s\nconfig: %s\ndata: %s\nsocket: %s\nstatus: %s\n",
		result.Adapter, result.Binary, result.Config, result.DataDir, result.Socket, result.Status)
	if result.ConfigKept {
		fmt.Printf("config: existing file kept unchanged (%s)\n", result.Config)
	}
	return 0
}

func runUninstall(args []string) int {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "/", "filesystem root")
	initOverride := fs.String("init", "", "init system override")
	configPath := fs.String("config", "", "configuration file (default: detected)")
	binaryPath := fs.String("binary", "/usr/local/bin/netrave", "installed binary path")
	dataDir := fs.String("data", "/var/lib/netrave", "data directory")
	logDir := fs.String("log-dir", "/var/log/netrave", "log directory")
	socket := fs.String("socket", "/run/netrave/daemon.sock", "daemon socket")
	purge := fs.Bool("purge", false, "remove configuration and user data")
	yes := fs.Bool("yes", false, "confirm destructive purge")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *purge && !*yes {
		fmt.Fprintln(os.Stderr, "refusing to purge data without --yes")
		return 2
	}
	kind, err := installer.Uninstall(context.Background(), installer.Options{
		Root: *root, InitSystem: *initOverride, ConfigPath: *configPath,
		BinaryPath: *binaryPath, DataDir: *dataDir, LogDir: *logDir,
		SocketPath: *socket, Purge: *purge,
	})
	if err != nil {
		return fail(err)
	}
	fmt.Printf("uninstalled (init: %s)\n", kind)
	if *purge {
		fmt.Println("configuration and data directories removed")
	} else {
		fmt.Println("configuration and data preserved; use --purge --yes to remove them")
	}
	return 0
}

func runDoctor(args []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	initOverride := fs.String("init", "", "init system override")
	jsonOutput := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	info, problems := installer.Doctor(*initOverride)
	if *jsonOutput {
		data, _ := json.MarshalIndent(map[string]any{"platform": info, "problems": problems}, "", "  ")
		fmt.Println(string(data))
	} else {
		fmt.Printf("distribution: %s (%s, %s)\n", info.OSRelease.Pretty, info.OSRelease.ID, info.Family)
		fmt.Printf("architecture: %s\ninit: %s\nroot: %v\nkvm: %v\nip_forward: %v\n",
			info.Arch, info.Init, info.Root, info.KVM, info.IPForward)
		for tool, present := range info.Tools {
			fmt.Printf("tool %-22s %v\n", tool, present)
		}
		if command := info.Adapter.InstallCommand(); command != "" {
			fmt.Printf("packages: sudo %s\n", command)
		}
	}
	for _, problem := range problems {
		fmt.Fprintf(os.Stderr, "warning: %s\n", problem)
	}
	return 0
}

// runNetDiag prints the state of the labs' network path: adapters, TAPs, bridges,
// nftables rules and br_netfilter. Requires root to read the host's nftables.
func runNetDiag(args []string) int {
	// -lab is parsed here, other flags (-data, -db, ...) by the shared config.ParseArgs.
	var labName string
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-lab" || args[i] == "--lab":
			if i+1 < len(args) {
				i++
				labName = args[i]
			}
		case strings.HasPrefix(args[i], "-lab="):
			labName = strings.TrimPrefix(args[i], "-lab=")
		case strings.HasPrefix(args[i], "--lab="):
			labName = strings.TrimPrefix(args[i], "--lab=")
		default:
			rest = append(rest, args[i])
		}
	}
	if !hasConfigFlag(rest) && os.Getenv("NETRAVE_CONFIG") == "" {
		if path := installedConfigPath(); path != "" {
			rest = append([]string{"--config", path}, rest...)
		}
	}
	cfg, err := config.ParseArgs(rest)
	if err != nil {
		return fail(err)
	}
	db, err := database.Open(cfg.DBPath)
	if err != nil {
		return fail(err)
	}
	defer db.Close()
	var names []string
	if labName != "" {
		names = []string{labName}
	} else {
		for _, l := range db.Labs.Labs() {
			names = append(names, l.Name)
		}
		sort.Strings(names)
	}
	nm := network.New(db, cfg)
	out := map[string]*network.Diag{}
	for _, name := range names {
		out[name] = nm.Diagnostics(name)
	}
	data, _ := json.MarshalIndent(out, "", "  ")
	fmt.Println(string(data))
	return 0
}

func runLogs(args []string) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	lines := fs.Int("n", 100, "number of lines")
	socket := fs.String("socket", "", "daemon socket")
	logFile := fs.String("file", "", "log file")
	configPath := fs.String("config", "", "configuration file (default: installed config)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	cfg, cfgErr := loadCommandConfig(*configPath)
	path := *socket
	if path == "" && cfgErr == nil {
		path = cfg.SocketPath
	}
	var callErr error
	if path != "" {
		response, err := daemon.Call(path, daemon.Request{Command: "logs", Lines: *lines})
		if err == nil {
			fmt.Println(response.Logs)
			return 0
		}
		callErr = err
	}
	file := *logFile
	if file == "" && cfgErr == nil {
		file = cfg.LogFile
	}
	if file != "" {
		if data, err := os.ReadFile(file); err == nil {
			parts := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			if len(parts) > *lines {
				parts = parts[len(parts)-*lines:]
			}
			fmt.Println(strings.Join(parts, "\n"))
			return 0
		} else if callErr == nil {
			callErr = err
		}
	}
	if cfgErr != nil {
		if callErr != nil {
			callErr = fmt.Errorf("%w (configuration: %v)", callErr, cfgErr)
		} else {
			callErr = cfgErr
		}
	}
	if callErr == nil {
		callErr = errors.New("daemon socket and log file are unknown")
	}
	return fail(fmt.Errorf("logs unavailable: %w", callErr))
}

func fail(err error) int {
	fmt.Fprintf(os.Stderr, "netrave: %v\n", err)
	return 1
}
