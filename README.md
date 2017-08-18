## awsRender for OpenSCAD

awsRender will render [OpenSCAD](http://www.openscad.org) files to STL in the Amazon cloud.

You'll need a [basic knowledge](https://aws.amazon.com/getting-started/) of Amazon AWS administration for the initial setup, but everything else should be automated.

You must create a suitably configured Amazon EC2 Linux instance (\*BSD should work too but isn't tested). The required configuration is simply that it has OpenSCAD and the AWS CLI installed and configured for the specified user, SSH access from the location you're running awsRender from (e.g. a public IP address), and access to an S3 bucket to store output files. Given the nature of OpenSCAD STL rendering as a memory-intensive, single-threaded process, the EC2 Memory Optimized instance types are often suitable (e.g. r4.large or r4.xlarge).

awsRender is run on a remote client. You must supply the EC2 Instance ID and some other configuration information. awsRender will start the instance if necessary, then run a background OpenSCAD task to render the .scad file to STL. The resulting files are then copied to an S3 bucket. awsRender can optionally shut down the instance once rendering is complete, minimizing AWS fees. All working files are automatically removed from the instance.

Optionally, awsRender can email you a notification that rendering is complete with information about the S3 bucket that the results have been stored in. This requires an email address that has been verified with the Amazon Simple Email Service (SES).

## Binaries

Pre-built binaries for [OS X](http://www.mobbs.co.uk/awsRender/osx/awsRender-1.0), [Linux (64-bit)](http://www.mobbs.co.uk/awsRender/linux/awsRender-1.0) and [Windows](http://www.mobbs.co.uk/awsRender/windows/awsRender-1.0.exe) are available. awsRender is distributed as a single self-contained binary, there is no installer.

## Configuration and usage
### Usage
```
awsRender [flags] <OpenSCAD file>
  -e, --emailaddr string    (optional) email address for notifications - must be SES verified
  -H, --hostkey string      SSH Host key
  -i, --instanceid string   AWS instance ID
  -k, --keyfile string      SSH private key PEM file to access instance
  -o, --output string       S3 bucket to store output files
  -d, --save-defaults       Save settings as future defaults for this Instance ID
  -p, --set-primary         Mark this instance as primary (i.e. the one used if none specified) - implies -d
  -s, --shutdown            (optional) stop instance on completion
  -u, --username string     AWS instance username
  -V, --version             Print version & licence information
      --debug-run           Terminate without executing run script, allowing manual debug
```

Settings may be found from either the command line or the defaults file (see below). awsRender supports multiple instances in the defaults file, with difference settings for each instance ID. One instance may be marked as the "Primary" instance in the defaults file, which will be used if no Instance ID is specified on the command line. In this case, running awsRender can be as simple as `awsRender file.scad`.

Required settings are:  
* AWS Instance ID (-i)
* SSH credentials of username (-u), PEM file (-k) and Host key (-H)
  * username is either the default for the instance AMI (ec2-user for Amazon Linux or RHEL, ubuntu for Ubuntu etc.) or explicitly created by the instance admin.
  * PEM file is the key pair that the admin supplied when the EC2 instance was created
  * Host Key is the fingerprint of the SSH server on the EC2 host. See below for details.  
* S3 bucket for output files (-o)
  * Must be created by the admin ahead of time, and appropriate access supplied. Test access by 'aws s3 ls <bucket>' from the instance command line.

Optional settings are:
* Email address for notifications (-e)
  * Ensure that the email address is listed in AWS SES console as "verified" otherwise notifications will silently fail.
* Flag to shutdown after rendering (-s)
  * Shutdown is initiated by the script run on the instance, so doesn't require an ongoing connection from the client.

Configuration settings are:
* Store current settings in defaults file for future use (-d)
* Set current instance ID as the new "Primary" instance (-p)

### Defaults file
awsRender maintains a file of defaults in [TOML](https://github.com/toml-lang/toml) format. This is stored in $XDG_CONFIG_HOME/awsRender/defaults (usually ~/.config/awsRender/defaults) or %CSIDL_APPDATA%\\awsRender on Windows. Defaults are stored indexed by AWS instance ID. Settings for multiple instances may be maintained, accessed by specifying the instance ID on the command line. Command line settings will over-ride defaults if both are available. Command line settings are only persisted to the defaults file if requested.

As described above, one instance may be specified to be Primary in the defaults file, which will be used if no instance ID is supplied on the command line.

### SSH Host Key
awsRender requires a SSH Host Key fingerprint to ensure the connection to the instance is secure. It deliberately does not offer [Trust On First Use](https://en.wikipedia.org/wiki/Trust_on_first_use) or the option to ignore the host key. The Host Key may be supplied on the command line or in the ~/.ssh/known_hosts file under an alias of the instance ID. We can't use the public IP address to index, as these are volatile across reboots on AWS.

See the [Amazon EC2 user guide](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/AccessingInstancesLinux.html) for information about reliably determining the Host Key fingerprint from the EC2 console on first boot.  
To create a host key alias in known_hosts - `ssh -o HostKeyAlias=i-0123456789abcdef0 -i ~/.ssh/my-key.pem <host>` (or just edit the known_hosts file and replace the hostname/IP Address at the start of the appropriate line with the alias).  
To find the host keys `ssh-keyscan <host>` from a trusted connection.  
Tested algorithms are ssh-ed25519, ecdsa-sha2-nistp256 and ssh-rsa - others may work.

In the future support for finding the host key in other places could be added (e.g. under a static DNS name, PuTTY's host key store for Windows users, EC2 instance tag).

### AWS region settings
You may need to set `AWS_REGION=<region>` as an environment variable if you get MissingRegion errors. Windows seems to require this as no other means of getting the region name appears to work. See https://github.com/aws/aws-sdk-go/issues/384 for details.

## Getting started with awsRender

This isn't intended to be a full AWS tutorial, and misses out a bunch of steps around account creation and management. See Amazon's documentation for help and details around any step.

1. Sign up to [AWS](https://aws.amazon.com/account/).
2. Create your account and private key files.
  * Make a careful note of your Access Key ID and Secret Access Key, you'll need them.
  * Store the supplied PEM file somewhere safe (like your local .ssh directory).
3. Create an S3 bucket from the web console to store your results.
4. (optional) Register an email address with SES to receive notifications.
5. Launch an EC2 Ubuntu Linux instance.
  * t2.micro is available in the free tier, so good for testing, but OpenSCAD will run out of memory on anything more than a very simply model.
  * t2.large and m4.large are cheapest for models needing up to 8GB RAM.
  * r4.large, r4.xlarge etc. are best for larger models (which is probably why you're using the cloud in the first place...)
6. `sudo apt-get update`
7. `sudo apt-get install openscad aws-cli`
8. `aws configure`
  * Supply the Access Key ID and Secret Access Key you saved in step 2.
9. That should be it, you should now be able to run awsRender against this instance ID. See above for details of the command line options you'll need, and for information on getting the SSH host key.
10. You can retrieve the rendered model from S3 using the aws cli - `aws s3 cp s3://my.bucket/model/foo.stl foo.stl`
  * You could also download from the S3 web console or configure the bucket to allow static web site hosting and get the contents directly over HTTP.
  * AWS will charge for on-going data storage, it may be advisable to remove models after they're downloaded. Either the CLI or web console can do this.

## Disclaimer
awsRender automates the use of various AWS services (EC2, S3 and SES). Use of awsRender may incur fees from Amazon Web Services Inc. All fees incurred in the use of awsRender are the responsibility of the user.

awsRender is Copyright (c) Andrew Mobbs 2017
