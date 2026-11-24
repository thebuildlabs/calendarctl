// Package cli defines the Cobra command tree. Commands contain no provider or
// persistence logic: they parse flags, call the service layer, and render.
package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/thebuildlabs/calendarctl/internal/bridge"
	"github.com/thebuildlabs/calendarctl/internal/calendar"
	"github.com/thebuildlabs/calendarctl/internal/config"
	ce "github.com/thebuildlabs/calendarctl/internal/errors"
	"github.com/thebuildlabs/calendarctl/internal/logging"
	"github.com/thebuildlabs/calendarctl/internal/output"
	"github.com/thebuildlabs/calendarctl/internal/providers/apple"
	"github.com/thebuildlabs/calendarctl/internal/providers/mock"
	"github.com/thebuildlabs/calendarctl/internal/service"
)

// version is overridden at build time via -ldflags "-X ...version=...".
var version = "0.1.0"

type App struct {
	Root         *cobra.Command
	cfg          *config.Config
	cfgPath      string
	log          *logging.Logger
	svc          *service.Service
	styleMode    string
	format       string
	quiet        bool
	yes          bool
	providerFlag string
	bridgeFlag   string

	// allowBridgeOverride is the explicit, non-interactive opt-in for sending
	// calendar data to a caller-chosen helper binary.
	allowBridgeOverride bool

	// flags bound in initFlags
	jsonFlag   bool
	yamlFlag   bool
	outputFlag string
	noColor    bool
	verbose    bool
	configFlag string
}

func NewApp() *App {
	a := &App{}
	a.Root = &cobra.Command{
		Use:               "calendarctl",
		Short:             "Manage calendars and events from the terminal",
		Long:              "calendarctl is a fast, scriptable command-line client for calendar applications.\nIt talks to Apple Calendar.app through a native EventKit bridge on macOS.",
		Version:           version,
		SilenceUsage:      true,
		SilenceErrors:     true,
		PersistentPreRunE: a.preRun,
	}
	a.initFlags()
	a.Root.AddCommand(
		a.initCommand(),
		a.calendarCommand(),
		a.eventCommand(),
		a.availabilityCommand(),
		a.configCommand(),
		a.permissionsCommand(),
		a.versionCommand(),
	)
	a.Root.SetOut(os.Stdout)
	a.Root.SetErr(os.Stderr)
	return a
}

func (a *App) initFlags() {
	f := a.Root.PersistentFlags()
	f.BoolVar(&a.jsonFlag, "json", false, "output JSON")
	f.BoolVar(&a.yamlFlag, "yaml", false, "output YAML")
	f.StringVar(&a.outputFlag, "output", "", "output format: table, json, yaml")
	f.BoolVar(&a.noColor, "no-color", false, "disable colored output")
	f.BoolVar(&a.quiet, "quiet", false, "suppress non-essential output")
	f.BoolVar(&a.verbose, "verbose", false, "enable debug logging to stderr")
	f.BoolVar(&a.yes, "yes", false, "skip interactive confirmation prompts")
	f.StringVar(&a.configFlag, "config", "", "path to configuration file")
	f.StringVar(&a.providerFlag, "provider", "", "provider override: apple, mock, auto")
	f.StringVar(&a.bridgeFlag, "bridge", "", "path to the native EventKit bridge binary")
	f.BoolVar(&a.allowBridgeOverride, "allow-bridge-override", false,
		"explicitly allow a caller-chosen bridge binary without an interactive prompt")
}

func (a *App) preRun(cmd *cobra.Command, _ []string) error {
	// Allow config and meta commands to work without a provider.
	if cmd.Annotations["skipProvider"] == "true" {
		return a.initBase()
	}
	if err := a.initBase(); err != nil {
		return err
	}
	return a.initProvider(cmd.Context())
}

func (a *App) initBase() error {
	cfg, path, err := config.Load()
	if err != nil {
		return err
	}
	if a.configFlag != "" {
		// Explicit --config replaces the path, even when the file is absent
		// (so `init` can create it).
		path = a.configFlag
		if b, e := os.ReadFile(a.configFlag); e == nil {
			if e := unmarshalConfig(b, cfg); e != nil {
				return ce.Wrap(ce.CodeInvalidInput, "config", e)
			}
		} else if !os.IsNotExist(e) {
			return ce.Wrap(ce.CodeGeneral, "config", e)
		}
	}
	cfg.Validate()
	a.cfg, a.cfgPath = cfg, path

	// Warnings are visible by default so security-relevant notices (for example
	// a bridge path override) are never hidden by quiet logging settings.
	level := logging.LevelWarn
	if a.verbose {
		level = logging.LevelDebug
	}
	a.log = logging.New(os.Stderr, level, a.jsonFlag || a.outputFlag == "json")

	a.format = cfg.OutputFormat
	if a.outputFlag != "" {
		a.format = strings.ToLower(a.outputFlag)
	}
	if a.jsonFlag {
		a.format = "json"
	}
	if a.yamlFlag {
		a.format = "yaml"
	}
	switch a.format {
	case "table", "json", "yaml":
	default:
		return ce.InvalidInput("output", fmt.Sprintf("unknown output format %q (use table, json or yaml)", a.format))
	}

	a.styleMode = cfg.Color
	if a.noColor {
		a.styleMode = "never"
	}
	return nil
}

func unmarshalConfig(b []byte, cfg *config.Config) error {
	return yaml.Unmarshal(b, cfg)
}

func (a *App) initProvider(ctx context.Context) error {
	if a.svc != nil {
		return nil
	}
	providerName := a.cfg.Provider
	if a.providerFlag != "" {
		providerName = a.providerFlag
	}
	providerName = strings.ToLower(strings.TrimSpace(providerName))
	if providerName == "" || providerName == "auto" {
		providerName = "mock"
		if runtime.GOOS == "darwin" {
			providerName = "apple"
		}
	}

	var p calendar.Provider
	var err error
	bridgePath := a.cfg.BridgePath
	if providerName == "apple" {
		// A caller-chosen helper receives the full calendar payload. Require an
		// explicit opt-in before any helper is created or executed.
		if override := bridge.ExplicitOverride(a.bridgeFlag, bridgePath); override != "" {
			bridgePath = override
			if err := a.authorizeBridgeOverride(bridgePath); err != nil {
				return err
			}
		}
	}
	switch providerName {
	case "apple":
		if runtime.GOOS != "darwin" {
			return ce.Unavailable("provider", "the apple provider requires macOS; use --provider mock for local testing")
		}
		p, err = apple.New(bridgePath, a.log)
	case "mock":
		storePath := os.Getenv("CALENDARCTL_MOCK_STORE")
		if storePath != "" {
			p, err = mock.NewFromFile(storePath)
		} else {
			p = mock.New()
		}
	default:
		return ce.InvalidInput("provider", fmt.Sprintf("unknown provider %q (use apple, mock or auto)", providerName))
	}
	if err != nil {
		return err
	}
	svc, err := service.NewService(p, a.cfg, a.log)
	if err != nil {
		return err
	}
	a.svc = svc
	_ = ctx
	return nil
}

// Service returns the initialized service (provider commands only).
func (a *App) Service() *service.Service { return a.svc }

// Renderer builds a renderer bound to stdout with the resolved format.
func (a *App) Renderer() *output.Renderer {
	r := output.New(a.format, a.quiet, os.Stdout)
	r.Style = output.NewStyle(a.styleMode, os.Stdout)
	return r
}

// RendererTo builds a renderer bound to an arbitrary writer (for tests).
func (a *App) RendererTo(w io.Writer) *output.Renderer {
	r := output.New(a.format, a.quiet, w)
	r.Style = output.NewStyle(a.styleMode, w)
	return r
}

func (a *App) versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:         "version",
		Short:       "Print version information",
		Annotations: map[string]string{"skipProvider": "true"},
		RunE: func(cmd *cobra.Command, _ []string) error {
			r := a.Renderer()
			if a.format == "table" {
				return r.Print(fmt.Sprintf("calendarctl %s", version))
			}
			return r.Render(map[string]any{"version": version, "os": runtime.GOOS, "arch": runtime.GOARCH}, output.Table{})
		},
	}
}
