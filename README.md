## logstasher-cli

logstasher-cli is a command line utility to query and tail logstash logs

### Installation

logstasher-cli is written in Go language, and if you have [Go installed](https://golang.org/doc/install#install), you can just type in:

`go get github.com/varlogs/logstasher-cli`

This will automatically download, compile and install the app.
After that you should have `logstasher-cli` executable in your `$GOPATH/bin`.	

### Usage

- [Overview](#overview)
- [Setting up profile](#setting-up-profile)
- [List all sources](#list-all-sources)
- [Filtering by source](#filtering-by-source)
- [Time Filters](#time-filters)
  - [Duration Filter](#duration-filter)
  - [After Filter](#after-filter)
  - [Before Filter](#before-filter)
- [Filter by Request Id](#filter-by-request-id)
- [Keyword Search](#keyword-search)
- [Keyword Watch](#keyword-watch)
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

You can refer to the global options available in the help panel for easy and quick reference. A detailed write up of the various commands/options can be found below.

### Setting up profile

You can setup different profiles that logstasher-cli can use to talk to multiple elasticsearch hosts. A typical setup would have separate profiles for each environment like staging, uat or production.

You can setup a profile by specifying the profile name the elasticsearch url:

```bash
$ logstasher-cli -p staging -url 'https://staging.logstasher.com:9200'
```

Ignore the port if the elasticsearch URL is behind some proxy and listening on port 80. You can setup multiple profiles using the above command specifying a profile name and host url for each environment.

Any other commands in the future will require you to specify only the profile as `-p or —profile` option. You can setup one of the configures profiles as the default profile to make the profile implicit.

```bash
$ logstasher-cli -p staging --default-profile
staging setup as default profile unless -p specified
```

You can overwrite the url option at anytime by just calling the `-p` and `-url` options again. All of your profile settings are stored at `~/.logstasher` folder at your root level and you can look for yourself to see the configuration params stored by `logstasher-cli`

### List all sources

This is more of a command than an option to list all the sources present in ElasticSearch. The output of this command is basically a unique aggregate on `source` field from all of the available indices

```bash
$ logstasher-cli --list-sources
Profile: staging | Host: https://staging.logstasher.com:9200
AuthService
Reporter
....
```

Note that we have skipped specifying `-p and -url` options as the default profile is picked up automatically based on previous configuration steps.

### Filtering by source

Now that you know the list of available sources, you can restrict logs to one or more sources

``` shell
$ logstasher-cli -s AuthService
Profile: staging | Host: https://staging.logstasher.com:9200
Querying logs after 2016-11-16T00:17:13.02191559
....
....
Fetch more logs or quit (m/q)?
```

We will talk about a couple of items from the last command's output. Notice that the logs are retrieved after a specific timestamp. This is based on the [duration filter](#duration-filter) discussed later and is defaulted to `5m`. And the number of entries that are fetched are based on the `-n` filter and is defaulted to 100. In this case, after every 100 entries, the user will be prompted to press `m/q` to continue or to quit. `logstasher-cli` can fetch logs from the source infinitely as long as there are more entries available.

And by the way, multiple sources can be specified in this filter as comma-separated values like `-s 'AuthService,ReportingService'`

### Time Filters

#### Duration Filter

The duration filter is an easy way to trail back to the past and search for logs from a starting point in the timeline. The possible values are `2m, 5m, 10m, 30m, 1h, 3h, 6h, 12h, 1d, 2d, 7d, 15d, 30d` and is defaulted to `5m`. 

```shell
$ logstasher-cli -s AuthService -d 1d
```

will fetch log entries of AuthService starting 24 hours in the past relative to the time at which the command is executed

#### After Filter

In addition to duration filter, you can pin point a specific timestamp in the logging timeline and fetch all logs including and after that timestamp

``` shell
$ logstasher-cli -a '2016-11-16T00:40:43.852'
```

will fetch logs from all sources (since source filter is missing) starting from the specific timestamp given as value to  `-a`

#### Before Filter

Similar to after filter, you can specify a before filter to fetch logs before a specific timestamp. 

### Filter by Request Id

You can specify `id` filter to fetch all logs traced by a specific `x-request-id`

```shell
$ logstasher-cli -id 4cbff9a3
```

### Keyword Search

This might be the most used filter for `logstasher-cli`. In addition to all the above filters, you can specify one or more keywords to search in the text analysed `message` field

``` shell
$ logstasher-cli -d 12h "Exception raised"
```

This command will search for all log entries that have the keywords `exception` and `raised` in the past 12 hours. The CLI will also highlight the search keywords in the log for easy reference

### Keyword Watch

Similar to the previous command except that it does not apply any filter on the keywords, but just highlights them in the log results for easy eyeing.

``` shell
$ logstasher-cli -w 'Transaction committed'
```

will highlight the words in the log trail for easy reference

### Tailing

`logstasher-cli` offers near realtime tailing of the logs based on the applied filters. Tail mode can be enabled by passing `-t` or `—tail` option. This mode will override all of the time filters including `-d, -a, -b` as by default, this will fetch the most recent 100 log entries from the host. When there are new entries appended to the host, they will be pulled and rendered on the terminal as and when they are available. This option will make you feel right at home with elasticsearch similar to using `tail -f` on a local file.

``` shell
$ logstasher-cli -tail -s AuthService -w "403"
```

We believe you would mostly want to filter by specific sources and watch for keywords and continuously tail to assist you with debugging.

