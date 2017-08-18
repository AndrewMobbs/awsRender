// Copyright (c) Andrew Mobbs 2017

package sshCmdClient

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"strings"

	"golang.org/x/crypto/ssh"
)

const statusMissingStatus = 1
const statusCmdFailedStatus = 1

// SSHCredentials stores basic credentials for an SSH connection
type SSHCredentials struct {
	SSHHostKey  string // SshHostKey is the host key for the server
	SSHUsername string // SshUsername is the user to connect with
	SSHPEMFile  string // SshPEMFile is the PEM file for the user's key
}

// SSHCmdClient is a wrapper that keeps an SSH connection open
type SSHCmdClient struct {
	client *ssh.Client
}

// Close closes the SSHCmdClient connection
func (cli *SSHCmdClient) Close() error {
	err := cli.client.Conn.Close()
	return err
}

// NewSSHCmdClient initialises a SSH connection to the given IP address
func NewSSHCmdClient(IPAddress net.IP, credentials *SSHCredentials) (*SSHCmdClient, error) {
	cli := new(SSHCmdClient)
	authMethod := func(pemFile *string) ssh.AuthMethod {
		buffer, err := ioutil.ReadFile(credentials.SSHPEMFile)
		if err != nil {
			panic(err) // Should have already tested PEM file exists
		}
		key, err := ssh.ParsePrivateKey(buffer)
		if err != nil {
			panic(err) // FIXME improve error handling for malformed file
		}
		return ssh.PublicKeys(key)
	}
	hostKey, _, _, _, err := ssh.ParseAuthorizedKey([]byte(credentials.SSHHostKey))
	if err != nil {
		return nil, fmt.Errorf("Error parsing host key : %s", err)
	}
	sshConfig := &ssh.ClientConfig{
		User: credentials.SSHUsername,
		Auth: []ssh.AuthMethod{
			authMethod(&credentials.SSHPEMFile),
		},
		HostKeyCallback:   ssh.FixedHostKey(hostKey), // Simple match on host key
		HostKeyAlgorithms: []string{hostKey.Type()},  // Specify the type of host key we have
	}
	// Dial your ssh server.
	conn, err := ssh.Dial("tcp", IPAddress.String()+":22", sshConfig)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to SSH server: %s", err)
	}

	cli.client = conn
	return cli, err
}

// RunCommand runs a command on the SSH connection and ignores StdOut and StdErr
func (cli *SSHCmdClient) RunCommand(cmd string) (exitStatus int, err error) {
	exitStatus, _, _, err = cli.RunCommandWithOutput(cmd)
	return exitStatus, err
}

// RunCommandWithOutput runs a command on the SSH connection returning StdOut & StdErr
func (cli *SSHCmdClient) RunCommandWithOutput(cmd string) (exitStatus int, stdoutBuf bytes.Buffer, stderrBuf bytes.Buffer, err error) {
	// Inspired by https://github.com/golang/crypto/blob/master/ssh/example_test.go
	session, err := cli.client.NewSession()
	if err != nil {
		return -1, stdoutBuf, stderrBuf, fmt.Errorf("unable to create session : %s", err)
	}
	defer session.Close()
	// Set up terminal modes
	modes := ssh.TerminalModes{
		ssh.ECHO:          0,     // disable echoing
		ssh.TTY_OP_ISPEED: 14400, // input speed = 14.4kbaud
		ssh.TTY_OP_OSPEED: 14400, // output speed = 14.4kbaud
	}
	// Request pseudo terminal
	if err = session.RequestPty("xterm", 40, 80, modes); err != nil {
		return -1, stdoutBuf, stderrBuf, fmt.Errorf("request for pseudo terminal failed : %s", err)
	}
	// If the remote server does not send an exit status, an error of type
	// *ExitMissingError is returned. If the command completes unsuccessfully or
	// is interrupted by a signal, the error is of type *ExitError. Other error
	// types may be returned for I/O problems.

	exitStatus = 0

	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	if err = session.Run(cmd); err != nil {
		switch exitType := err.(type) {
		case *ssh.ExitError:
			exitStatus = exitType.Waitmsg.ExitStatus()
			err = nil
		case *ssh.ExitMissingError:
			exitStatus = statusMissingStatus
		default:
			exitStatus = statusCmdFailedStatus
		}
	}

	return exitStatus, stdoutBuf, stderrBuf, err
}

// BackgroundCommand is a wrapper around RunCommand that just encloses
// the command in "nohup bash -c '((<cmd>) &) '"
// discardOutput will also append &>/dev/null - otherwise will go to nohup.out
// anything else you'll need to construct the command yourself
func (cli *SSHCmdClient) BackgroundCommand(cmd string, discardOutput bool) (exitStatus int, err error) {
	cmd = fmt.Sprintf("nohup bash -c '((%s) &)' ", cmd)
	if discardOutput {
		cmd += "&>/dev/null"
	}
	return cli.RunCommand(cmd)
}

// CopyFile copies a file from the local filesystem to the remote server
func (cli *SSHCmdClient) CopyFile(source string, destination string) error {
	filestat, err := os.Stat(source)
	if err != nil {
		return fmt.Errorf("Error statting source file %s: %s", source, err)
	}
	if !filestat.Mode().IsRegular() {
		return fmt.Errorf("Source file %s must be a regular file", filestat.Name())
	}
	file, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("Error opening source file %s: %s", source, err)
	}
	defer file.Close()

	err = cli.writeToFile(file, destination)
	if err != nil {
		return fmt.Errorf("Error copying source file %s: %s", source, err)
	}
	return err
}

// WriteBytesToFile writes a byte slice to a file on the remote server
func (cli *SSHCmdClient) WriteBytesToFile(source []byte, destination string) error {
	r := bytes.NewReader(source)
	err := cli.writeToFile(r, destination)
	return err
}

// writeToFile is the backend to write data to a file on the remote server
// Inspired by https://github.com/YuriyNasretdinov/GoSSHa/blob/master/main.go
func (cli *SSHCmdClient) writeToFile(source io.Reader, destination string) error {

	session, err := cli.client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()

	cmd := "cat >'" + strings.Replace(destination, "'", "'\\''", -1) + "'"

	stdinPipe, err := session.StdinPipe()
	if err != nil {
		return err
	}

	err = session.Start(cmd)
	if err != nil {
		return err
	}
	io.Copy(stdinPipe, source)
	err = stdinPipe.Close()
	if err != nil {
		return err
	}

	err = session.Wait()
	return err
}
