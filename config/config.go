// Copyright (c) Andrew Mobbs 2017

package config

import (
	"awsRender/sshCmdClient"
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"net/mail"
	"os"
	"path"
	"runtime"
	"strings"

	toml "github.com/burntsushi/toml"
	"github.com/spf13/pflag"
)

const defaultsFile = "defaults"
const defaultsFilePerm = 0644

// Settings holds various configuration options for awsRender
type Settings struct {
	InstanceID   *string
	PemFile      *string
	Username     *string
	HostKey      *string
	S3bucket     *string
	EmailAddr    *string
	ShutdownFlag *bool
}

type defaults struct {
	DefaultInstanceID string
	Instances         map[string]Settings
}

type commandline struct {
	settings     *Settings
	saveDefaults *bool
	setPrimary   *bool
	version      *bool
	debug        *bool
}

// parseOpts parses the command line options, with defaults taken from file
func commandLineOpts() *commandline {
	cl := new(commandline)
	cl.settings = new(Settings)
	cl.settings.InstanceID = pflag.StringP("instanceid", "i", "", "AWS \x1b[1mi\x1b[0mnstance ID")
	cl.settings.PemFile = pflag.StringP("keyfile", "k", "", "SSH private \x1b[1mk\x1b[0mey PEM file to access instance")
	cl.settings.Username = pflag.StringP("username", "u", "", "AWS instance \x1b[1mu\x1b[0msername")
	cl.settings.HostKey = pflag.StringP("hostkey", "H", "", "SSH \x1b[1mH\x1b[0most key")
	cl.settings.ShutdownFlag = pflag.BoolP("shutdown", "s", false, "(optional) \x1b[1ms\x1b[0mtop instance on completion")
	cl.settings.S3bucket = pflag.StringP("output", "o", "", "S3 bucket to store \x1b[1mo\x1b[0mutput files")
	cl.settings.EmailAddr = pflag.StringP("emailaddr", "e", "", "(optional) \x1b[1me\x1b[0mmail address for notifications - must be SES verified")
	cl.saveDefaults = pflag.BoolP("save-defaults", "d", false, "Save settings as future \x1b[1md\x1b[0mefaults for this Instance ID")
	cl.setPrimary = pflag.BoolP("set-primary", "p", false, "Mark this instance as \x1b[1mp\x1b[0mrimary (i.e. the one used if none specified) - implies -d")
	cl.version = pflag.BoolP("version", "V", false, "Print version & licence information")
	cl.debug = pflag.BoolP("debug-run", "", false, "Terminate without executing run script, allowing manual debug")
	pflag.Usage = usage
	pflag.Parse()
	return cl
}

// CheckSettings perfoms some checks on the configuration settings for validity
func (c *Settings) checkSettings() error {
	var err error
	if *c.PemFile == "" {
		err = fmt.Errorf("Require SSH PEM file to be specified")
	}

	if _, statErr := os.Stat(*c.PemFile); os.IsNotExist(statErr) {
		err = fmt.Errorf("Cannot locate SSH PEM file")
	}

	if *c.Username == "" {
		err = fmt.Errorf("Require SSH username to be specified")
	}

	if *c.InstanceID == "" {
		err = fmt.Errorf("Require EC2 instance ID to be specified")
	}

	if *c.S3bucket == "" {
		err = fmt.Errorf("Require result S3 bucket to be specified")
	}
	if *c.EmailAddr != "" {
		_, err = mail.ParseAddress(*c.EmailAddr)
		if err != nil {
			return err
		}
	}

	return err
}

// ExtractSSHCredentials extracts the SSH credentials from config
func (c *Settings) ExtractSSHCredentials() *sshCmdClient.SSHCredentials {
	credentials := &sshCmdClient.SSHCredentials{
		SSHHostKey:  *c.HostKey,
		SSHUsername: *c.Username,
		SSHPEMFile:  *c.PemFile,
	}
	return credentials
}

// readDefaults reads the default settings from the local filesystem
func (d *defaults) read(defaultsFile string) error {
	_, err := os.Stat(defaultsFile)
	if os.IsNotExist(err) {
		d.Instances = nil
		err = nil
	} else {
		// Defaults file exists, try reading it
		data, readErr := ioutil.ReadFile(defaultsFile)
		if readErr != nil {
			return err
		}
		d.Instances = make(map[string]Settings)
		err = toml.Unmarshal(data, d)
		if err != nil {
			return err
		}
	}
	// Fall-through - error stating file that wasn't NotExist
	return err
}

// updateDefaults updates default values for this instance ID, and optionally sets new Primary instance
func (d *defaults) updateDefaults(c *Settings, updatePrimary bool) {
	if d.Instances == nil {
		d.Instances = make(map[string]Settings)
	}
	d.Instances[*c.InstanceID] = *c
	if updatePrimary {
		d.DefaultInstanceID = *c.InstanceID
	}
}

// write writes the current config settings to the defaults file
func (d *defaults) write(defaultsFile string) error {
	// Marshal TOML of config

	f, err := os.Create(defaultsFile)
	if err != nil {
		return err
	}
	defer f.Close()
	b := bufio.NewWriter(f)
	w := toml.NewEncoder(b)
	if err != nil {
		return err
	}

	err = w.Encode(d)
	if err != nil {
		return err
	}

	return err
}

// applyDefaults applies any unset parameters that are available from defaults file
func (c *Settings) applyDefaults(d *defaults) error {
	if *c.InstanceID == "" {
		if d.DefaultInstanceID != "" {
			*c.InstanceID = d.DefaultInstanceID
		} else {
			return fmt.Errorf("Require either an instance ID on command line or a default primary instance")
		}
	}
	// Apply defaults for the InstanceID if they exist
	if _, ok := d.Instances[*c.InstanceID]; ok {
		if !pflag.Lookup("emailaddr").Changed && *d.Instances[*c.InstanceID].EmailAddr != "" {
			*c.EmailAddr = *d.Instances[*c.InstanceID].EmailAddr
		}
		if !pflag.Lookup("keyfile").Changed && *d.Instances[*c.InstanceID].PemFile != "" {
			*c.PemFile = *d.Instances[*c.InstanceID].PemFile
		}
		if !pflag.Lookup("username").Changed && *d.Instances[*c.InstanceID].Username != "" {
			*c.Username = *d.Instances[*c.InstanceID].Username
		}
		if !pflag.Lookup("hostkey").Changed && *d.Instances[*c.InstanceID].HostKey != "" {
			*c.HostKey = *d.Instances[*c.InstanceID].HostKey
		}
		if !pflag.Lookup("output").Changed && *d.Instances[*c.InstanceID].S3bucket != "" {
			*c.S3bucket = *d.Instances[*c.InstanceID].S3bucket
		}
		if !pflag.Lookup("shutdown").Changed {
			*c.ShutdownFlag = *d.Instances[*c.InstanceID].ShutdownFlag
		}
	}

	return nil
}

// findHostKey attempts to dig up the instance SSH Host Key from the
// 		~/.ssh/known_hosts under the instance ID as an alias
func (c *Settings) findHostKey() error {
	f, err := os.Open(os.Getenv("HOME") + "/.ssh/known_hosts")
	if err != nil {
		return err
	}
	defer f.Close()
	b := bufio.NewReader(f)

	scanner := bufio.NewScanner(b)
	for scanner.Scan() {
		if strings.HasPrefix(strings.TrimSpace(scanner.Text()), *c.InstanceID) {
			*c.HostKey = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(scanner.Text()), *c.InstanceID))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	return nil
}

// usage prints usage and copyright info
func usage() {
	fmt.Fprintf(os.Stderr, "awsRender [flags] <OpenSCAD file>\n")
	fmt.Fprintf(os.Stderr, "\tWill use Amazon EC2 instance specified to render a given OpenSCAD file\n")
	fmt.Fprintf(os.Stderr, "\tto STL. Results are stored in S3, optionally will shutdown instance\n")
	fmt.Fprintf(os.Stderr, "\tand/or email notification on completion. EC2 instance requires OpenSCAD,\n")
	fmt.Fprintf(os.Stderr, "\tAWS CLI, SSH access & S3 permissions to be configured.\n\n")
	fmt.Fprintf(os.Stderr, "Use of awsRender may incur fees from Amazon Web Services Inc.\nAll fees incurred in the use of awsRender are the responsibility of the user.\n")
	pflag.PrintDefaults()
}

func version() {
	fmt.Println("awsRender v1.0")
	fmt.Println("awsRender is Copyright (c) Andrew Mobbs 2017")
	fmt.Println("AWS is a trademark of Amazon Web Services, Inc.")
	fmt.Println("awsRender includes code with the following copyrights:")
	fmt.Println("github.com/spf13/pflag")
	fmt.Println("\tCopyright (c) 2012 Alex Ogier. All rights reserved")
	fmt.Println("\tCopyright (c) 2012 The Go Authors. All rights reserved.")
	fmt.Println("github.com/BurntSushi/toml")
	fmt.Println("\tCopyright (c) 2013 TOML authors")
	fmt.Println("golang.org/x/crypto/ssh")
	fmt.Println("\tCopyright (c) 2009 The Go Authors. All rights reserved.")
	fmt.Println("github.com/aws/aws-sdk-go")
	fmt.Println("\tCopyright 2015 Amazon.com, Inc. or its affiliates. All Rights Reserved.")
	fmt.Println("\tCopyright 2014-2015 Stripe, Inc.")
}

func (c *Settings) debugPrintSettings() {
	fmt.Printf("c.InstanceID :\t%s\nc.PemFile :\t%s\nc.Username :\t%s\n", *c.InstanceID, *c.PemFile, *c.Username)
	fmt.Printf("c.HostKey :\t%s\nc.S3bucket :\t%s\nc.EmailAddr :\t%s\nc.ShutdownFlag :\t%t\n", *c.HostKey, *c.S3bucket, *c.EmailAddr, *c.ShutdownFlag)
}

// GetSettings retrieves config from defaults file and command line,
// checks that the settings are vaild, and if needed updates defaults file.
// Returns pointer to settings, debug bool and error
func GetSettings() (*Settings, bool, error) {
	// Get command line options
	cl := commandLineOpts()
	c := cl.settings
	if *cl.version {
		version()
		os.Exit(0)
	}
	if *cl.debug {
		fmt.Println("Settings from command line:")
		c.debugPrintSettings()
	}
	// Get defaults
	d := new(defaults)
	var configDir string
	switch runtime.GOOS {
	case "windows":
		configDir = os.Getenv("CSIDL_APPDATA") + "\\awsRender"
	case "darwin", "linux", "solaris", "freebsd", "netbsd", "dragonfly":
		xdgConfigHome := os.Getenv("XDG_CONFIG_HOME")

		if xdgConfigHome != "" {
			configDir = os.Getenv("XDG_CONFIG_HOME") + "/awsRender"
		} else {
			configDir = os.Getenv("HOME") + "/.config/awsRender"
		}
	default:
		log.Panicf("Unsupported OS")
	}
	err := os.MkdirAll(configDir, 0755)
	if err != nil {
		return nil, false, err
	}
	configPath := path.Join(configDir, defaultsFile)

	err = d.read(configPath)
	if err != nil {
		return nil, false, err
	}

	// apply default settings to current config
	err = c.applyDefaults(d)
	if err != nil {
		return nil, false, err
	}
	if *cl.debug {
		fmt.Println("Settings after defaults applied:")
		c.debugPrintSettings()
	}
	// Validate settings before saving
	err = c.checkSettings()
	if err != nil {
		return nil, false, err
	}
	// Update defaults structure, and save (before H)
	if *cl.saveDefaults || *cl.setPrimary {
		d.updateDefaults(c, *cl.setPrimary)
		err = d.write(configPath)
		if err != nil {
			return nil, false, err
		}
	}
	// if we still don't have a host key, look elsewhere
	if c.HostKey == nil || *c.HostKey == "" {
		c.findHostKey()
	}

	if *c.HostKey == "" {
		err = fmt.Errorf("Require SSH host key to be specified (ssh-keyscan to generate)")
	}

	if *cl.debug {
		fmt.Println("Settings after Host Key search:")
		c.debugPrintSettings()
	}

	return c, *cl.debug, err
}
