// Copyright (c) Andrew Mobbs 2017

package main

import (
	"awsRender/config"
	"awsRender/ec2RunCmd"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/spf13/pflag"
)

// checkInstance runs a set of checks to ensure instance is OK to run
// OpenSCAD render process
// TODO - look at using goroutines to run checks in parallel
func checkInstance(ins *ec2RunCmd.EC2RemoteClient, settings *config.Settings) {
	// Check OpenSCAD is installed and runnable
	cmd := fmt.Sprintf("openscad --version &> /dev/null")
	exitStatus, err := ins.RunCommand(cmd)
	if err != nil {
		log.Fatal(err)
	}
	if exitStatus != 0 {
		log.Fatal("Non-zero exit status from attempt to run OpenSCAD on instance. Check OpenSCAD installed.")
	}
	// Check instance is configured to use aws cli (and aws cli installed...)
	cmd = fmt.Sprintf("aws ec2 describe-instances --instance-id %s > /dev/null", ins.InstanceID)
	exitStatus, err = ins.RunCommand(cmd)
	if err != nil {
		log.Fatal("Error running AWS CLI test", err)
	}
	if exitStatus != 0 {
		log.Fatal("Non-zero exit status from AWS EC2 CLI test on target instance. Check AWS CLI installed and configured.")
	}
	// Check instance can see S3 bucket
	cmd = fmt.Sprintf("aws s3 ls %s > /dev/null", *settings.S3bucket)
	exitStatus, err = ins.RunCommand(cmd)
	if err != nil {
		log.Fatal("Error running S3 test", err)
	}
	if exitStatus != 0 {
		log.Fatal("Non-zero exit status from AWS S3 CLI test on target instance. Check instance has correct permission on S3 bucket.")
	}
}

// checkSourceFile performs some checks on the SCAD source file
func checkSourceFile(sourceFile string) {
	filestat, err := os.Stat(sourceFile)
	if err != nil {
		log.Fatal("Error statting source file: ", err)
	}
	if !filestat.Mode().IsRegular() {
		log.Fatalf("Source file %s must be a regular file", filestat.Name())
	}
	if !strings.HasSuffix(sourceFile, ".scad") {
		log.Fatal("Source file must be a .scad file")
	}
	// TODO - if there's a local openscad >= 2015.03 could try png preview to
	//         validate the SCAD file before kicking off a remote render?
}

// makeWorkingDir Creates the working directory on the target instance
func makeWorkingDir(instance *ec2RunCmd.EC2RemoteClient) string {
	exitStatus, workDir, _, err := instance.RunCommandWithOutput("mktemp -d -p.")
	if err != nil {
		log.Fatal("Error creating working directory :", err)
	}
	if exitStatus != 0 {
		log.Fatal("Non-Zero exit status creating working directory")
	}
	exitStatus, homeDir, _, err := instance.RunCommandWithOutput("env | grep HOME | cut -d'=' -f2")
	if err != nil {
		log.Fatal("Error creating working directory :", err)
	}
	if exitStatus != 0 {
		log.Fatal("Non-Zero exit status creating working directory")
	}

	return strings.TrimSpace(homeDir.String()) + strings.TrimLeft(strings.TrimSpace(workDir.String()), ".")
}

// createRunScript creates the shell script on the target instance
func createRunScript(sourceFile string, workDir string, settings *config.Settings) string {
	notificationGenerator := ""
	notificationScript := ""
	if *settings.EmailAddr != "" {
		notificationGenerator = fmt.Sprintf("printf -v notificationMessage 'Subject={Data=\"OpenSCAD render - %%s\",Charset=UTF-8},Body={Text={Data=\"Render of file %s complete. Result was %%s. Output put in S3 bucket %s .\",Charset=UTF-8}}' ${renderResult} ${renderResult}\n", sourceFile, *settings.S3bucket)
		notificationScript = fmt.Sprintf("aws ses send-email --from %s --to %s --message \"${notificationMessage}\"\n", *settings.EmailAddr, *settings.EmailAddr)
	}
	shutdownScript := ""
	if *settings.ShutdownFlag == true {
		shutdownScript = fmt.Sprintf("aws ec2 stop-instances --instance-id %s\n", *settings.InstanceID)
	}
	outFile := strings.TrimSuffix(sourceFile, ".scad") + ".stl"
	// FIXME - this is probably better done in golang templates, but the syntax
	// made my head hurt
	runScript := fmt.Sprintf(`
#!/bin/bash -x

cd %s
openscad -o %s %s 2>openscad.err > openscad.out
if [[ $? -ne 0 || ! -f %s ]] # Non-zero exit, or .stl file doesn't exist
then
  # render failed - dump dmesg to help debug memory problems
    dmesg > dmesg.out
    renderResult=FAILED
else
    renderResult=SUCCESS
fi
for f in %s %s openscad.err openscad.out dmesg.out
do
    if [[ -s ${f} ]]
    then
        aws s3 cp ${f} %s
    fi
done
# Email notification if address given
%s
%s

# Tidy up, and if necessary stop instance
cd ~
rm -rf %s
%s
`,
		workDir,
		outFile,
		sourceFile,
		outFile,
		sourceFile,
		outFile,
		*settings.S3bucket,
		notificationGenerator,
		notificationScript,
		workDir,
		shutdownScript)

	return runScript
}

func main() {
	// Get configuration for this render
	settings, debug, err := config.GetSettings()
	if err != nil {
		log.Fatal(err)
	}

	credentials := settings.ExtractSSHCredentials()

	// Check input file
	if len(pflag.Args()) == 0 {
		log.Fatal("No input file.") // TODO - add stdin support
	}
	sourceFile := pflag.Args()[0]
	checkSourceFile(sourceFile) // will call log.Fatal if problems

	log.Printf("Initializing instance %s", *settings.InstanceID)
	// Set up the EC2 instance
	instance, err := ec2RunCmd.NewEC2RemoteClient(settings.InstanceID, credentials)
	if err != nil {
		log.Fatal(err)
	}
	defer instance.Close()
	checkInstance(instance, settings) // will call log.Fatal if problems
	log.Printf("Setting up rendering on %s", instance.InstanceID)
	// Create working directory on instance
	workDir := makeWorkingDir(instance)
	// Copy source file to instance
	err = instance.CopyFile(sourceFile, workDir+"/"+sourceFile)
	if err != nil {
		log.Fatalf("Error copying file %s to target %s : %s\n", sourceFile, workDir+"/"+sourceFile, err)
	}
	// Build run script, copy it to the instance and make it executable
	runScript := createRunScript(sourceFile, workDir, settings)
	err = instance.WriteBytesToFile([]byte(runScript), workDir+"/run.sh")
	if err != nil {
		log.Fatalf("Error writing run script : %s", err)
	}
	exitStatus, err := instance.RunCommand("chmod a+x " + workDir + "/run.sh")
	if err != nil || exitStatus != 0 {
		log.Fatalf("Error making run script executable : %s", err)
	}
	if !debug {
		// Run the remote script to do the work as nohup'd background command
		// TODO - possibly add a dry-run option to do all but this step?
		exitStatus, err = instance.BackgroundCommand(workDir+"/run.sh", true)
		if err != nil || exitStatus != 0 {
			log.Fatalf("Error running script : %s", err)
		}
		n := ""
		s := ""
		if *settings.EmailAddr != "" {
			n = fmt.Sprintf("Notification will be sent to %s. ", *settings.EmailAddr)
		}
		if *settings.ShutdownFlag {
			s = fmt.Sprintf("Instance will be stopped on completion. ")
		}
		log.Printf("Render of %s started on %s. Output to %s. %s%s", sourceFile, instance.InstanceID, *settings.S3bucket, n, s)
	} else {
		log.Printf("DEBUG MODE - render script not started. Files in working directory %s on instance %s.", workDir, instance.InstanceID)
	}

	os.Exit(0)
}
