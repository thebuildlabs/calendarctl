// Package config loads and persists calendarctl's YAML configuration.
// It never stores credentials or account tokens.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const ConfigFileName = "config.yaml"

// Config is the on-disk configuration schema.
type Config struct {
	Version               int    `yaml:"version" json:"version"`
	Provider              string `yaml:"provider" json:"provider"`
	DefaultCalendar       string `yaml:"defaultCalendar" json:"defaultCalendar"`
	Timezone              string `yaml:"timezone" json:"timezone"`
	OutputFormat          string `yaml:"outputFormat" json:"outputFormat"`
	Color                 string `yaml:"color" json:"color"` // auto | always | never
	ConfirmDestructive    bool   `yaml:"confirmDestructive" json:"confirmDestructive"`
	DateFormat            string `yaml:"dateFormat" json:"dateFormat"`
	TimeFormat            string `yaml:"timeFormat" json:"timeFormat"`
	DefaultReminderMins   []int  `yaml:"defaultReminderMinutes" json:"defaultReminderMinutes"`
	WorkingHoursStart     string `yaml:"workingHoursStart" json:"workingHoursStart"`
	WorkingHoursEnd       string `yaml:"workingHoursEnd" json:"workingHoursEnd"`
	DefaultMeetingMinutes int    `yaml:"defaultMeetingMinutes" json:"defaultMeetingMinutes"`
	BridgePath            string `yaml:"bridgePath" json:"bridgePath"`
}

func Default() *Config {
	return &Config{
		Version:               1,
		Provider:              "auto",
		DefaultCalendar:       "",
		Timezone:              "", // empty means system local
		OutputFormat:          "table",
		Color:                 "auto",
		ConfirmDestructive:    true,
		DateFormat:            "Mon Jan 02, 2006",
		TimeFormat:            "15:04",
		DefaultReminderMins:   []int{15},
		WorkingHoursStart:     "09:00",
		WorkingHoursEnd:       "18:00",
		DefaultMeetingMinutes: 30,
	}
}

// Path returns the configuration file location, honoring XDG_CONFIG_HOME.
func Path() (string, error) {
	if p := os.Getenv("CALENDARCTL_CONFIG"); p != "" {
		return p, nil
	}
	if base := os.Getenv("XDG_CONFIG_HOME"); base != "" {
		return filepath.Join(base, "calendarctl", ConfigFileName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if dir := os.Getenv("HOME"); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "Library", "Application Support")); err == nil {
			return filepath.Join(dir, "Library", "Application Support", "calendarctl", ConfigFileName), nil
		}
	}
	return filepath.Join(home, ".config", "calendarctl", ConfigFileName), nil
}

// Load reads config from disk, falling back to defaults when absent.
// Environment variables with CALENDARCTL_ prefix override file values.
func Load() (*Config, string, error) {
	path, err := Path()
	if err != nil {
		return nil, "", err
	}
	cfg := Default()
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			applyEnv(cfg)
			return cfg, path, nil
		}
		return nil, path, fmt.Errorf("read config %s: %w", path, err)
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return nil, path, fmt.Errorf("parse config %s: %w", path, err)
	}
	if cfg.Version == 0 {
		cfg.Version = 1
	}
	applyEnv(cfg)
	cfg.Validate()
	return cfg, path, nil
}

func (c *Config) Validate() {
	if c.OutputFormat == "" {
		c.OutputFormat = "table"
	}
	c.OutputFormat = strings.ToLower(strings.TrimSpace(c.OutputFormat))
	switch c.OutputFormat {
	case "json", "yaml", "table":
	default:
		c.OutputFormat = "table"
	}
	if c.Color == "" {
		c.Color = "auto"
	}
	c.Color = strings.ToLower(strings.TrimSpace(c.Color))
	switch c.Color {
	case "auto", "always", "never":
	default:
		c.Color = "auto"
	}
	if c.Provider == "" {
		c.Provider = "auto"
	}
	if c.DefaultMeetingMinutes <= 0 {
		c.DefaultMeetingMinutes = 30
	}
	if c.WorkingHoursStart == "" {
		c.WorkingHoursStart = "09:00"
	}
	if c.WorkingHoursEnd == "" {
		c.WorkingHoursEnd = "18:00"
	}
	if len(c.DefaultReminderMins) == 0 {
		c.DefaultReminderMins = []int{15}
	}
	if c.TimeFormat == "" {
		c.TimeFormat = "15:04"
	}
	if c.DateFormat == "" {
		c.DateFormat = "Mon Jan 02, 2006"
	}
}

// Init writes a default config to the resolved path.
func Init(force bool) (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	return InitTo(path, force)
}

// InitTo writes a default config to an explicit path.
func InitTo(path string, force bool) (string, error) {
	if path == "" {
		resolved, err := Path()
		if err != nil {
			return "", err
		}
		path = resolved
	}
	if !force {
		if _, err := os.Stat(path); err == nil {
			return path, fmt.Errorf("config already exists at %s (use --force to overwrite)", path)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create config directory: %w", err)
	}
	b, err := yaml.Marshal(Default())
	if err != nil {
		return "", err
	}
	header := "# calendarctl configuration\n# Credentials and calendar tokens are NEVER stored here.\n\n"
	if err := os.WriteFile(path, append([]byte(header), b...), 0o600); err != nil {
		return "", fmt.Errorf("write config: %w", err)
	}
	return path, nil
}

// SaveTo persists a configuration to an explicit path with safe permissions.
func SaveTo(path string, c *Config) error {
	if path == "" {
		resolved, err := Path()
		if err != nil {
			return err
		}
		path = resolved
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create config directory: %w", err)
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	header := "# calendarctl configuration\n# Credentials and calendar tokens are NEVER stored here.\n\n"
	if err := os.WriteFile(path, append([]byte(header), b...), 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// YAML returns the config serialized as YAML.
func (c *Config) YAML() (string, error) {
	b, err := yaml.Marshal(c)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Location resolves the configured timezone; an empty value means local time.
func (c *Config) Location() (*time.Location, error) {
	if strings.TrimSpace(c.Timezone) == "" {
		return time.Local, nil
	}
	return time.LoadLocation(c.Timezone)
}

// TimezoneName returns an IANA identifier for the effective timezone, used
// when writing events. It falls back to TZ and /etc/localtime before the
// generic "Local" label.
func (c *Config) TimezoneName() string {
	if strings.TrimSpace(c.Timezone) != "" {
		return c.Timezone
	}
	if tz := strings.TrimSpace(os.Getenv("TZ")); tz != "" {
		return tz
	}
	if link, err := os.Readlink("/etc/localtime"); err == nil {
		if i := strings.Index(link, "zoneinfo/"); i >= 0 {
			return link[i+len("zoneinfo/"):]
		}
	}
	return time.Local.String()
}

func applyEnv(c *Config) {
	setStr := func(key string, dst *string) {
		if v, ok := os.LookupEnv("CALENDARCTL_" + key); ok {
			*dst = v
		}
	}
	setBool := func(key string, dst *bool) {
		if v, ok := os.LookupEnv("CALENDARCTL_" + key); ok {
			if b, err := strconv.ParseBool(v); err == nil {
				*dst = b
			}
		}
	}
	setInt := func(key string, dst *int) {
		if v, ok := os.LookupEnv("CALENDARCTL_" + key); ok {
			if n, err := strconv.Atoi(v); err == nil {
				*dst = n
			}
		}
	}
	setStr("PROVIDER", &c.Provider)
	setStr("DEFAULT_CALENDAR", &c.DefaultCalendar)
	setStr("TIMEZONE", &c.Timezone)
	setStr("OUTPUT_FORMAT", &c.OutputFormat)
	setStr("COLOR", &c.Color)
	setStr("DATE_FORMAT", &c.DateFormat)
	setStr("TIME_FORMAT", &c.TimeFormat)
	setStr("BRIDGE_PATH", &c.BridgePath)
	setBool("CONFIRM_DESTRUCTIVE", &c.ConfirmDestructive)
	setInt("DEFAULT_MEETING_MINUTES", &c.DefaultMeetingMinutes)
}
