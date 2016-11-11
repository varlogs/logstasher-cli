package main

import (
	"runtime"
	"os"
	"encoding/json"
	"io/ioutil"
	"github.com/codegangsta/cli"
)

type SearchTarget struct {
	Url          string
	TunnelUrl    string        `json:"-"`
	IndexPattern string
}

type QueryDefinition struct {
	Terms          []string
	Format         string
	TimestampField string
	AfterDateTime  string  `json:"-"`
	BeforeDateTime string  `json:"-"`
	Source         string
	RequestId      string
}

type Commands struct {
	ListSources bool
}

type Configuration struct {
	Profile		string
	SearchTarget    SearchTarget
	QueryDefinition QueryDefinition
	Commands 	Commands
	InitialEntries  int
	TailMode        bool        `json:"-"`
	User            string
	Password        string  `json:"-"`
	Verbose         bool        `json:"-"`
	MoreVerbose     bool        `json:"-"`
	TraceRequests   bool        `json:"-"`
	SSHTunnelParams string
	SaveQuery       bool        `json:"-"`
}

var confDir = ".logstasher"

//When changing this array, make sure to also make appropriate changes in CopyConfigRelevantSettingsTo
var configRelevantFlags = []string{"url", "f", "i", "u", "ssh"}

func userHomeDir() string {
	if runtime.GOOS == "windows" {
		home := os.Getenv("HOMEDRIVE") + os.Getenv("HOMEPATH")
		if home == "" {
			home = os.Getenv("USERPROFILE")
		}
		return home
	}
	return os.Getenv("HOME")
}

func (c *Configuration) Copy() *Configuration {
	result := new(Configuration)

	c.CopyConfigRelevantSettingsTo(result)
	c.CopyNonConfigRelevantSettingsTo(result)

	return result
}

//When making change here make sure configRelevantFlags global var is also changed
func (c *Configuration) CopyConfigRelevantSettingsTo(dest *Configuration) {
	//copy config relevant configuration settings
	dest.Profile = c.Profile
	dest.SearchTarget.TunnelUrl = c.SearchTarget.TunnelUrl
	dest.SearchTarget.Url = c.SearchTarget.Url
	dest.SearchTarget.IndexPattern = c.SearchTarget.IndexPattern
	dest.QueryDefinition.Format = c.QueryDefinition.Format
	dest.QueryDefinition.Terms = make([]string, len(c.QueryDefinition.Terms))
	copy(dest.QueryDefinition.Terms, c.QueryDefinition.Terms)
	dest.User = c.User
	dest.SSHTunnelParams = c.SSHTunnelParams
}

func (c *Configuration) CopyNonConfigRelevantSettingsTo(dest *Configuration) {
	//copy non-config relevant settings
	dest.QueryDefinition.TimestampField = c.QueryDefinition.TimestampField
	dest.QueryDefinition.AfterDateTime = c.QueryDefinition.AfterDateTime
	dest.QueryDefinition.BeforeDateTime = c.QueryDefinition.BeforeDateTime
	dest.TailMode = c.TailMode
	dest.InitialEntries = c.InitialEntries
	dest.Password = c.Password
	dest.Verbose = c.Verbose
	dest.MoreVerbose = c.MoreVerbose
	dest.TraceRequests = c.TraceRequests
}

func (c *Configuration) SaveDefault() {
	confDirPath := userHomeDir() + string(os.PathSeparator) + confDir;
	if _, err := os.Stat(confDirPath); os.IsNotExist(err) {
		//conf directory doesn't exist, let's create it
		err := os.Mkdir(confDirPath, 0700)
		if (err != nil) {
			Error.Printf("Failed to create configuration directory %s, %s\n", confDirPath, err)
			return
		}
	}
	confJson, err := json.MarshalIndent(c, "", "  ")
	if (err != nil) {
		Error.Printf("Failed to marshall configuration to json: %s.\n", err)
		return
	}
	confFile := confDirPath + string(os.PathSeparator) + c.Profile + ".json"
	err = ioutil.WriteFile(confFile, confJson, 0700)
	if (err != nil) {
		Error.Printf("Failed to save configuration to file %s, %s\n", confFile, err)
		return
	}
}

func LoadProfile(profile string) (conf *Configuration, err error) {
	confDirPath := userHomeDir() + string(os.PathSeparator) + confDir;
	if _, err := os.Stat(confDirPath); os.IsNotExist(err) {
		//conf directory doesn't exist, let's create it
		err := os.Mkdir(confDirPath, 0700)
		if (err != nil) {
			return nil, err
		}
	}
	confFile := confDirPath + string(os.PathSeparator) + profile + ".json";
	var config *Configuration
	confBytes, err := ioutil.ReadFile(confFile)
	if (err != nil) {
		return nil, err
	}
	err = json.Unmarshal(confBytes, &config)
	if (err != nil) {
		return nil, err
	}
	return config, nil
}

func (config *Configuration) Flags() []cli.Flag {
	cli.VersionFlag.Usage = "Print the version"
	cli.HelpFlag.Usage = "Show help"
	return []cli.Flag{
		cli.StringFlag{
			Name:        "p,profile",
			Value:       "default",
			Usage:       "You can setup a profile for each environment (staging, production) or for each platform with a unique ElasticSearch URL",
			Destination: &config.Profile,
		},
		cli.StringFlag{
			Name:        "url",
			Value:       "http://127.0.0.1:9200",
			Usage:       "(*) ElasticSearch URL",
			Destination: &config.SearchTarget.Url,
		},
		cli.StringFlag{
			Name:        "f,format",
			Value:       "%@timestamp %x_request_id %source %message",
			Usage:       "(*) Message format for the entries - field names are referenced using % sign, for example '%@timestamp %message'",
			Destination: &config.QueryDefinition.Format,
		},
		cli.StringFlag{
			Name:        "i,index-pattern",
			Value:       "logstash-[0-9].*",
			Usage:       "(*) Index pattern - logstasher will attempt to tail only the latest of logstash's indexes matched by the pattern",
			Destination: &config.SearchTarget.IndexPattern,
			Hidden: true,
		},
		cli.StringFlag{
			Name:        "ts,timestamp-field",
			Value:       "@timestamp",
			Usage:       "(*) Timestamp field name used for tailing entries",
			Destination: &config.QueryDefinition.TimestampField,
			Hidden: true,
		},
		cli.BoolFlag{
			Name:        "t,tail",
			Usage:       "Follow ElasticSearch for more updates and continously tail",
			Destination: &config.TailMode,
		},
		cli.IntFlag{
			Name:        "n",
			Value:       100,
			Usage:       "Number of entries fetched initially",
			Destination: &config.InitialEntries,
		},
		cli.BoolFlag{
			Name:        "list-sources",
			Usage:       "List all the sources",
			Destination: &config.Commands.ListSources,
		},
		cli.StringFlag{
			Name:        "s,src",
			Value:       "",
			Usage:       "Filter by one or more sources (-s 'Foo', -s 'Foo,Bar')",
			Destination: &config.QueryDefinition.Source,
		},
		cli.StringFlag{
			Name:        "id",
			Value:       "",
			Usage:       "Filter by x-request-id",
			Destination: &config.QueryDefinition.RequestId,
		},
		cli.StringFlag{
			Name:        "a,after",
			Value:       "",
			Usage:       "List results after specified date (example: -a \"2016-06-17T15:00\")",
			Destination: &config.QueryDefinition.AfterDateTime,
		},
		cli.StringFlag{
			Name:        "b,before",
			Value:       "",
			Usage:       "List results before specified date (example: -b \"2016-06-17T15:00\")",
			Destination: &config.QueryDefinition.BeforeDateTime,
		},
		cli.BoolFlag{
			Name:        "save",
			Usage:       "Save query terms - next invocation of logstasher (without parameters) will use saved query terms. Any additional terms specified will be applied with AND operator to saved terms",
			Destination: &config.SaveQuery,
		},
		cli.StringFlag{
			Name:        "u",
			Value:       "",
			Usage:       "(*) Username for http basic auth, password is supplied over password prompt",
			Destination: &config.User,
		},
		cli.StringFlag{
			Name:        "ssh,ssh-tunnel",
			Value:       "",
			Usage:       "(*) Use ssh tunnel to connect. Format for the argument is [localport:][user@]sshhost.tld[:sshport]",
			Destination: &config.SSHTunnelParams,
			Hidden: true,
		},
		cli.BoolFlag{
			Name:        "v1",
			Usage:       "Enable verbose output (for debugging)",
			Destination: &config.Verbose,
		},
		cli.BoolFlag{
			Name:        "v2",
			Usage:       "Enable even more verbose output (for debugging)",
			Destination: &config.MoreVerbose,
		},
		cli.BoolFlag{
			Name:        "v3",
			Usage:       "Same as v2 but also trace requests and responses (for debugging)",
			Destination: &config.TraceRequests,
		},
		cli.VersionFlag,
		cli.HelpFlag,
	}
}

//logstasher will work in list-only (no follow) mode if appropriate flag is set or if query has date-time filtering enabled
func (c *Configuration) isTailMode() bool {
	return c.TailMode
}

func (q *QueryDefinition) IsDateTimeFiltered() bool {
	return q.AfterDateTime != "" || q.BeforeDateTime != ""
}

func (q *QueryDefinition) isSourceFiltered() bool {
	return q.Source != ""
}

func (q *QueryDefinition) isRequestIdFiltered() bool {
	return q.RequestId != ""
}

func IsConfigRelevantFlagSet(c *cli.Context) bool {
	for _, flag := range configRelevantFlags {
		if c.IsSet(flag) {
			return true
		}
	}
	return false
}
