package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/zsuroy/ctty/internal/config"
	"github.com/zsuroy/ctty/internal/credential"
	"github.com/zsuroy/ctty/internal/validation"
)

// Shared non-interactive host flags (bound in add/edit init).
var (
	hostFlagName           string
	hostFlagHostname       string
	hostFlagUser           string
	hostFlagPort           string
	hostFlagIdentityFile   string
	hostFlagProxyJump      string
	hostFlagProxyCommand   string
	hostFlagOptions        []string
	hostFlagTags           string
	hostFlagPassword       string
	hostFlagFormat         string
	hostFlagForce          bool
	hostFlagNonInteractive bool
)

// hostMutatingFlagNames are flags that trigger non-interactive mode when set.
var hostMutatingFlagNames = []string{
	"name", "hostname", "user", "port", "identity-file",
	"proxy-jump", "proxy-command", "option", "tags", "password",
	"force", "non-interactive",
}

// wantNonInteractive reports whether add/edit should skip the TUI.
// Trigger: --non-interactive is set, OR any host-config flag was explicitly
// provided on the command line (e.g. --hostname, --name, --user, ...).
func wantNonInteractive(cmd *cobra.Command) bool {
	if hostFlagNonInteractive {
		return true
	}
	for _, name := range hostMutatingFlagNames {
		if f := cmd.Flags().Lookup(name); f != nil && f.Changed {
			return true
		}
	}
	return false
}

func registerHostCLIFlags(cmd *cobra.Command, includeForce bool) {
	cmd.Flags().StringVar(&hostFlagName, "name", "", "Host alias (required in non-interactive mode)")
	cmd.Flags().StringVar(&hostFlagHostname, "hostname", "", "Remote hostname or IP")
	cmd.Flags().StringVar(&hostFlagUser, "user", "", "SSH username")
	cmd.Flags().StringVar(&hostFlagPort, "port", "", "SSH port (default 22)")
	cmd.Flags().StringVar(&hostFlagIdentityFile, "identity-file", "", "Path to identity (private key) file")
	cmd.Flags().StringVar(&hostFlagProxyJump, "proxy-jump", "", "ProxyJump bastion host")
	cmd.Flags().StringVar(&hostFlagProxyCommand, "proxy-command", "", "ProxyCommand")
	cmd.Flags().StringArrayVarP(&hostFlagOptions, "option", "o", nil, "SSH config option (repeatable), e.g. Compression=yes")
	cmd.Flags().StringVar(&hostFlagTags, "tags", "", "Comma-separated tags")
	cmd.Flags().StringVar(&hostFlagPassword, "password", "", "Optional password stored in credential vault only (never written to SSH config, never printed)")
	cmd.Flags().StringVar(&hostFlagFormat, "format", "", "Output format: json for agent-friendly success payload")
	cmd.Flags().BoolVar(&hostFlagNonInteractive, "non-interactive", false, "Force non-interactive mode (skip TUI form)")
	if includeForce {
		cmd.Flags().BoolVar(&hostFlagForce, "force", false, "Overwrite an existing host with the same name")
	}
}

func parseTagsCSV(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	var tags []string
	for _, tag := range strings.Split(s, ",") {
		tag = strings.TrimSpace(tag)
		tag = strings.TrimPrefix(tag, "#")
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

func optionsFromFlags(opts []string) string {
	if len(opts) == 0 {
		return ""
	}
	// Join as repeated -o forms so ParseSSHOptionsFromCommand handles them.
	var parts []string
	for _, o := range opts {
		o = strings.TrimSpace(o)
		if o == "" {
			continue
		}
		if strings.HasPrefix(o, "-o") {
			parts = append(parts, o)
		} else {
			parts = append(parts, "-o "+o)
		}
	}
	return config.ParseSSHOptionsFromCommand(strings.Join(parts, " "))
}

func resolveConfigPath() (string, error) {
	if configFile != "" {
		return configFile, nil
	}
	return config.GetDefaultSSHConfigPath()
}

func loadHostByName(name, cfgPath string) (*config.SSHHost, error) {
	var hosts []config.SSHHost
	var err error
	if cfgPath != "" {
		hosts, err = config.ParseSSHConfigFile(cfgPath)
	} else {
		hosts, err = config.ParseSSHConfig()
	}
	if err != nil {
		return nil, err
	}
	for i := range hosts {
		if hosts[i].Name == name {
			return &hosts[i], nil
		}
	}
	return nil, fmt.Errorf("host %q not found", name)
}

type hostCLIResult struct {
	OK       bool     `json:"ok"`
	Action   string   `json:"action"`
	Name     string   `json:"name"`
	Hostname string   `json:"hostname"`
	User     string   `json:"user,omitempty"`
	Port     string   `json:"port,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Error    string   `json:"error,omitempty"`
}

func writeHostCLIResult(action string, host config.SSHHost, err error) {
	res := hostCLIResult{
		OK:       err == nil,
		Action:   action,
		Name:     host.Name,
		Hostname: host.Hostname,
		User:     host.User,
		Port:     host.Port,
		Tags:     host.Tags,
	}
	if err != nil {
		res.Error = err.Error()
	}
	if hostFlagFormat == "json" {
		b, mErr := json.Marshal(res)
		if mErr != nil {
			fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", mErr)
			os.Exit(1)
		}
		fmt.Println(string(b))
		if err != nil {
			os.Exit(1)
		}
		return
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Host %q %s successfully.\n", host.Name, action)
}

func storePasswordIfSet(hostName, password string) error {
	if password == "" {
		return nil
	}
	return credential.SetPassword(hostName, password)
}

func addHostNonInteractive(cmd *cobra.Command, positionalName string) {
	name := strings.TrimSpace(hostFlagName)
	if name == "" {
		name = strings.TrimSpace(positionalName)
	}
	hostname := strings.TrimSpace(hostFlagHostname)

	if name == "" || hostname == "" {
		fmt.Fprintf(os.Stderr, "Error: non-interactive add requires --name and --hostname (or pass the alias as the argument with --hostname).\n")
		fmt.Fprintf(os.Stderr, "Trigger: --non-interactive or any of --name/--hostname/--user/--port/--identity-file/--proxy-jump/--proxy-command/--option/--tags/--password/--force.\n")
		os.Exit(1)
	}

	port := strings.TrimSpace(hostFlagPort)
	if port == "" {
		port = "22"
	}
	user := strings.TrimSpace(hostFlagUser)
	identity := strings.TrimSpace(hostFlagIdentityFile)

	if err := validation.ValidateHost(name, hostname, port, identity); err != nil {
		writeHostCLIResult("added", config.SSHHost{Name: name, Hostname: hostname}, err)
		return
	}

	host := config.SSHHost{
		Name:         name,
		Hostname:     hostname,
		User:         user,
		Port:         port,
		Identity:     identity,
		ProxyJump:    strings.TrimSpace(hostFlagProxyJump),
		ProxyCommand: strings.TrimSpace(hostFlagProxyCommand),
		Options:      optionsFromFlags(hostFlagOptions),
		Tags:         parseTagsCSV(hostFlagTags),
	}

	cfgPath, err := resolveConfigPath()
	if err != nil {
		writeHostCLIResult("added", host, err)
		return
	}

	exists, err := config.HostExistsInFile(name, cfgPath)
	if err != nil {
		writeHostCLIResult("added", host, err)
		return
	}

	action := "added"
	if exists {
		if !hostFlagForce {
			writeHostCLIResult("added", host, fmt.Errorf("host %q already exists (use --force to overwrite)", name))
			return
		}
		action = "updated"
		err = config.UpdateSSHHostInFile(name, host, cfgPath)
	} else {
		err = config.AddSSHHostToFile(host, cfgPath)
	}
	if err != nil {
		writeHostCLIResult(action, host, err)
		return
	}

	if err := storePasswordIfSet(name, hostFlagPassword); err != nil {
		writeHostCLIResult(action, host, fmt.Errorf("host saved but failed to store password: %w", err))
		return
	}

	writeHostCLIResult(action, host, nil)
}

func editHostNonInteractive(cmd *cobra.Command, targetName string) {
	cfgPath, err := resolveConfigPath()
	if err != nil {
		writeHostCLIResult("updated", config.SSHHost{Name: targetName}, err)
		return
	}

	existing, err := loadHostByName(targetName, cfgPath)
	if err != nil {
		writeHostCLIResult("updated", config.SSHHost{Name: targetName}, err)
		return
	}

	host := *existing

	if cmd.Flags().Changed("name") && strings.TrimSpace(hostFlagName) != "" {
		host.Name = strings.TrimSpace(hostFlagName)
	}
	if cmd.Flags().Changed("hostname") {
		host.Hostname = strings.TrimSpace(hostFlagHostname)
	}
	if cmd.Flags().Changed("user") {
		host.User = strings.TrimSpace(hostFlagUser)
	}
	if cmd.Flags().Changed("port") {
		host.Port = strings.TrimSpace(hostFlagPort)
		if host.Port == "" {
			host.Port = "22"
		}
	}
	if cmd.Flags().Changed("identity-file") {
		host.Identity = strings.TrimSpace(hostFlagIdentityFile)
	}
	if cmd.Flags().Changed("proxy-jump") {
		host.ProxyJump = strings.TrimSpace(hostFlagProxyJump)
	}
	if cmd.Flags().Changed("proxy-command") {
		host.ProxyCommand = strings.TrimSpace(hostFlagProxyCommand)
	}
	if cmd.Flags().Changed("option") {
		host.Options = optionsFromFlags(hostFlagOptions)
	}
	if cmd.Flags().Changed("tags") {
		host.Tags = parseTagsCSV(hostFlagTags)
	}

	if err := validation.ValidateHost(host.Name, host.Hostname, host.Port, host.Identity); err != nil {
		writeHostCLIResult("updated", host, err)
		return
	}

	sourceFile := existing.SourceFile
	if sourceFile == "" {
		sourceFile = cfgPath
	}

	if err := config.UpdateSSHHostInFile(targetName, host, sourceFile); err != nil {
		writeHostCLIResult("updated", host, err)
		return
	}

	if cmd.Flags().Changed("password") {
		if err := storePasswordIfSet(host.Name, hostFlagPassword); err != nil {
			writeHostCLIResult("updated", host, fmt.Errorf("host updated but failed to store password: %w", err))
			return
		}
	}

	writeHostCLIResult("updated", host, nil)
}
