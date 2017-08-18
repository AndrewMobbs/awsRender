// Copyright (c) Andrew Mobbs 2017

package ec2RunCmd

import (
	"bytes"
	"fmt"
	"log"
	"net"

	"awsRender/sshCmdClient"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
)

// EC2RemoteClient stores stuff about an AWS EC2 instance
type EC2RemoteClient struct {
	InstanceID     string
	instanceIP     net.IP
	sshCredentials *sshCmdClient.SSHCredentials
	session        *session.Session
	ec2Client      *ec2.EC2
	cmdClient      *sshCmdClient.SSHCmdClient
}

// NewEC2RemoteClient creates and initialise a new EC2RemoteClient object, given an AWS Instance ID
func NewEC2RemoteClient(InstanceID *string, credentials *sshCmdClient.SSHCredentials) (*EC2RemoteClient, error) {
	ins := new(EC2RemoteClient)
	ins.InstanceID = *InstanceID

	session, err := session.NewSession()
	if err != nil {
		return nil, err
	}

	ec2Client := ec2.New(session)

	ins.session = session
	ins.ec2Client = ec2Client
	ins.sshCredentials = credentials

	err = ins.makeReady()

	return ins, err
}

// Close tears down all sessions and connections as appropriate
func (ins *EC2RemoteClient) Close() error {
	return ins.cmdClient.Close()
}

// startInstance starts an EC2 instance, and waits for it to become ready
func (ins *EC2RemoteClient) startInstance() error {
	log.Printf("Starting EC2 Instance %s", ins.InstanceID)
	_, err := ins.ec2Client.StartInstances(&ec2.StartInstancesInput{InstanceIds: aws.StringSlice([]string{ins.InstanceID})})
	if err != nil {
		return fmt.Errorf("Error starting instance : %s", err)
	}
	log.Printf("Waiting for Instance %s to become ready (may take a few minutes)", ins.InstanceID)
	err = ins.ec2Client.WaitUntilInstanceStatusOk(&ec2.DescribeInstanceStatusInput{InstanceIds: aws.StringSlice([]string{ins.InstanceID})})
	if err != nil {
		return fmt.Errorf("Error waiting for instance to become available : %s", err)
	}
	return err
}

// getIPAddress retrieves the public IP address from AWS. Returns error if no address found
func (ins *EC2RemoteClient) getIPAddress() error {
	result, err := ins.ec2Client.DescribeInstances(&ec2.DescribeInstancesInput{InstanceIds: aws.StringSlice([]string{ins.InstanceID})})
	if err != nil {
		return fmt.Errorf("Error getting instance details : %s", err)
	}
	ins.instanceIP = net.ParseIP(*result.Reservations[0].Instances[0].PublicIpAddress)
	if ins.instanceIP == nil {
		return fmt.Errorf("Error parsing IP address")
	}
	return err
}

// makeReady prepares an EC2 instance for running remote SSH commands
func (ins *EC2RemoteClient) makeReady() error {
	// Check Instance is running - will error if instance doesn't exist
	result, err := ins.ec2Client.DescribeInstanceStatus(&ec2.DescribeInstanceStatusInput{InstanceIds: aws.StringSlice([]string{ins.InstanceID})})

	if err != nil {
		return fmt.Errorf("Error getting instance status : %s", err)
	}

	// Start instance if needed
	if len(result.InstanceStatuses) == 0 || *result.InstanceStatuses[0].InstanceState.Name != "running" {
		err = ins.startInstance()
		if err != nil {
			return fmt.Errorf("Error starting instance : %s", err)
		}
	}

	// Get Public IP address from ec2
	err = ins.getIPAddress()
	if err != nil {
		return fmt.Errorf("Error getting IP address : %s", err)
	}

	// Set up SSH connection
	ins.cmdClient, err = sshCmdClient.NewSSHCmdClient(ins.instanceIP, ins.sshCredentials)
	if err != nil {
		return err
	}
	// Check we can at least run a trivial command
	exitStatus, err := ins.RunCommand("true")
	if err != nil || exitStatus != 0 {
		return fmt.Errorf("Error running commands on instance : %s", err)
	}

	return err
}

// RunCommand is a wrapper around the SSH client to run a command
// abstracts the SSH connection details from the EC2 client interface
// RunCommandWithOutput discards the stdout and stderr from the command
func (ins *EC2RemoteClient) RunCommand(cmd string) (exitStatus int, err error) {
	exitStatus, err = ins.cmdClient.RunCommand(cmd)
	return exitStatus, err
}

// RunCommandWithOutput is a wrapper around the SSH client to run a command
// abstracts the SSH connection details from the EC2 client interface
// RunCommandWithOutput provides the stdout and stderr from the command
func (ins *EC2RemoteClient) RunCommandWithOutput(cmd string) (exitStatus int, stdoutBuf bytes.Buffer, stderrBuf bytes.Buffer, err error) {
	exitStatus, stdoutBuf, stderrBuf, err = ins.cmdClient.RunCommandWithOutput(cmd)
	return exitStatus, stdoutBuf, stderrBuf, err
}

// BackgroundCommand is a wrapper around the SSH client to run a command
// abstracts the SSH connection details from the EC2 client interface
func (ins *EC2RemoteClient) BackgroundCommand(cmd string, discardOutput bool) (int, error) {
	exitStatus, err := ins.cmdClient.BackgroundCommand(cmd, discardOutput)
	return exitStatus, err
}

// CopyFile copies a file from the local filesystem to that on the EC2 instance
func (ins *EC2RemoteClient) CopyFile(source string, destination string) error {
	err := ins.cmdClient.CopyFile(source, destination)
	return err
}

// WriteBytesToFile writes a []byte to a specified file on the EC2 instance
func (ins *EC2RemoteClient) WriteBytesToFile(source []byte, destination string) error {
	err := ins.cmdClient.WriteBytesToFile(source, destination)
	return err
}
