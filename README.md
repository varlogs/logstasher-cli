## logstasher-cli

logstasher-cli is a command line utility to query and tail logstash logs

### Installation

​	

### Usage

- [Overview](#overview)
- [Setting up profile](#Setting-up-profile)
- [Tailing](#tailing)



### Overview

Make sure you have `logstasher-cli` installed by typing the following

```powershell
$ logstasher-cli  -h

NAME:
   logstasher-cli - The power of command line to search/tail logstash logs

USAGE:
   logstasher-cli [global options] '<search keyword(s)>'
   Options marked with (*) are saved between invocations of the command. Each time you specify an option marked with (*) previously stored settings are erased.
....
```

### Setting up profile

You can setup different profiles that can logstasher-cli can use to talk to multiple elasticsearch hosts. A typical setup would be to have separate profiles for each environment like staging, uat or production.

You can setup a profile by specifying the profile name the elasticsearch url

```bash
$ logstasher-cli -p staging -url 'https://staging.logstasher.com:9200'
```

Ignore the port if the elasticsearch URL is behind some proxy and listening on port 80. You can setup multiple profiles using the above command specifying a profile name and host for each profile

Any other commands in the future will require you to specify only the profile using the `-p or —profile` option. You can setup one of the configures profiles as the default profile to make the profile implicit

```bash
$ logstasher-cli -p staging --default-profile
staging setup as default profile unless -p specified
```

You can overwrite the url option at anytime by just calling the `-p` and `-url` options again. All of your profile settings are stored at `~/.logstasher` folder at your root level and you can look for yourself to see the configuration params stored by `logstasher-cli`

### Tailing



