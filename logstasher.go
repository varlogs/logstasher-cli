package main

import (
	//"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/codegangsta/cli"
	"golang.org/x/crypto/ssh/terminal"
	"gopkg.in/olivere/elastic.v2"
)


// Regexp for parsing out format fields
var formatRegexp = regexp.MustCompile("%[A-Za-z0-9@_.-]+")
var localTz, _ = time.LoadLocation("Local")

const dateFormatDMY = "2006-01-02"

// Create a new Tailer using configuration
func NewTail(configuration *Configuration) *Tail {
	tail := new(Tail)

	var client *elastic.Client
	var err error
	var url = configuration.SearchTarget.Url
	if !strings.HasPrefix(url, "http") {
		url = "http://" + url
		Trace.Printf("Adding http:// prefix to given url. Url: " + url)
	}

	if !Must(regexp.MatchString(".*:\\d+", url)) && Must(regexp.MatchString("http://[^/]+$", url)) {
		url += ":9200"
		Trace.Printf("No port was specified, adding default port 9200 to given url. Url: " + url)
	}

	//if a tunnel is successfully created, we need to connect to tunnel url (which is localhost on tunnel port)
	if configuration.SearchTarget.TunnelUrl != "" {
		url = configuration.SearchTarget.TunnelUrl
	}

	defaultOptions := []elastic.ClientOptionFunc{
		elastic.SetURL(url),
		elastic.SetSniff(false),
		elastic.SetHealthcheckTimeoutStartup(10 * time.Second),
		elastic.SetHealthcheckTimeout(2 * time.Second),
	}

	if configuration.User != "" {
		defaultOptions = append(defaultOptions,
			elastic.SetBasicAuth(configuration.User, configuration.Password))
	}

	if configuration.TraceRequests {
		defaultOptions = append(defaultOptions,
			elastic.SetTraceLog(Trace))
	}

	tail.tailMode = configuration.TailMode

	if (tail.tailMode) {
		fmt.Printf("In Tail Mode... Starting with the most recent %d entries!\n", configuration.InitialEntries)
	}

	client, err = elastic.NewClient(defaultOptions...)

	if err != nil {
		Error.Fatalf("Could not connect Elasticsearch client to %s: %s.", url, err)
	}
	tail.client = client

	tail.queryDefinition = &configuration.QueryDefinition


	tail.selectIndices(configuration)

	//If we're date filtering on start date, then the sort needs to be ascending
	if configuration.QueryDefinition.AfterDateTime != "" || configuration.QueryDefinition.Duration != "" {
		tail.order = true //ascending
	} else {
		tail.order = false //descending
	}
	return tail
}


// Extracts and parses YMD date (year followed by month followed by day) from a given string. YMD values are separated by
// separator character given as argument.
func extractYMDDate(dateStr, separator string) time.Time {
	dateRegexp := regexp.MustCompile(fmt.Sprintf(`(\d{4}%s\d{2}%s\d{2})`, separator, separator))
	match := dateRegexp.FindAllStringSubmatch(dateStr, -1)
	if len(match) == 0 {
		Error.Fatalf("Failed to extract date: %s\n", dateStr)
	}
	result := match[0]
	parsed, err := time.Parse(fmt.Sprintf("2006%s01%s02", separator, separator), result[0])
	if err != nil {
		Error.Fatalf("Failed parsing date: %s", err)
	}
	return parsed
}

func main() {

	config := new(Configuration)
	app := cli.NewApp()
	app.Name = "logstasher-cli"
	app.Usage = "The power of command line to search/tail logstash logs"
	app.HideHelp = true
	app.Version = VERSION
	app.ArgsUsage = "'<search keyword(s)>'\n   Options marked with (*) are saved between invocations of the command. Each time you specify an option marked with (*) previously stored settings are erased."
	app.Flags = config.Flags()
	app.Action = func(c *cli.Context) {

		if c.IsSet("help") {
			cli.ShowAppHelp(c)
			os.Exit(0)
		}
		if config.MoreVerbose || config.TraceRequests {
			InitLogging(os.Stderr, os.Stderr, os.Stderr, true)
		} else if config.Verbose {
			InitLogging(ioutil.Discard, os.Stderr, os.Stderr, false)
		} else {
			InitLogging(ioutil.Discard, ioutil.Discard, os.Stderr, false)
		}

		if !IsConfigRelevantFlagSet(c) {
			loadedConfig, err := LoadProfile(config.Profile)
			if err != nil {
				Info.Printf("Failed to find or open previous default configuration: %s\n", err)
			} else {
				Info.Printf("Loaded previous config and connecting to host %s.\n", loadedConfig.SearchTarget.Url)
				loadedConfig.CopyConfigRelevantSettingsTo(config)

				//if config.MoreVerbose {
				//	confJs, _ := json.MarshalIndent(loadedConfig, "", "  ")
				//	//Trace.Println("Loaded config:")
				//	//Trace.Println(string(confJs))
				//
				//	confJs, _ = json.MarshalIndent(loadedConfig, "", "  ")
				//	//Trace.Println("Final (merged) config:")
				//	//Trace.Println(string(confJs))
				//}
			}
		}

		if config.User != "" {
			fmt.Print("Enter password: ")
			config.Password = readPasswd()
		}

		fmt.Println(paintSystemParams(config))
		//reset TunnelUrl to nothing, we'll point to the tunnel if we actually manage to create it
		config.SearchTarget.TunnelUrl = ""
		if config.SSHTunnelParams != "" {
			//We need to start ssh tunnel and make el client connect to local port at localhost in order to pass
			//traffic through the tunnel
			elurl, err := url.Parse(config.SearchTarget.Url)
			if err != nil {
				Error.Fatalf("Failed to parse hostname/port from given URL: %s\n", config.SearchTarget.Url)
			}
			Trace.Printf("SSHTunnel remote host: %s\n", elurl.Host)

			tunnel := NewSSHTunnelFromHostStrings(config.SSHTunnelParams, elurl.Host)
			//Using the TunnelUrl configuration param, we will signify the client to connect to tunnel
			config.SearchTarget.TunnelUrl = fmt.Sprintf("http://localhost:%d", tunnel.Local.Port)

			Info.Printf("Starting SSH tunnel %d:%s@%s:%d to %s:%d", tunnel.Local.Port, tunnel.Config.User,
				tunnel.Server.Host, tunnel.Server.Port, tunnel.Remote.Host, tunnel.Remote.Port)
			go tunnel.Start()
			Trace.Print("Sleeping for a second until tunnel is established...")
			time.Sleep(1 * time.Second)
		}

		var configToSave *Configuration

		args := c.Args()

		if config.SaveQuery {
			if args.Present() {
				config.QueryDefinition.Terms = []string{args.First()}
				config.QueryDefinition.Terms = append(config.QueryDefinition.Terms, args.Tail()...)
			} else {
				config.QueryDefinition.Terms = []string{}
			}
			configToSave = config.Copy()
			Trace.Printf("Saving query terms. Total terms: %d\n", len(configToSave.QueryDefinition.Terms))
		} else {
			Trace.Printf("Not saving query terms. Total terms: %d\n", len(config.QueryDefinition.Terms))
			configToSave = config.Copy()
			if args.Present() {
				if len(config.QueryDefinition.Terms) > 1 {
					config.QueryDefinition.Terms = append(config.QueryDefinition.Terms, "AND")
					config.QueryDefinition.Terms = append(config.QueryDefinition.Terms, args...)
				} else {
					config.QueryDefinition.Terms = []string{args.First()}
					config.QueryDefinition.Terms = append(config.QueryDefinition.Terms, args.Tail()...)
				}
			}
		}

		tail := NewTail(config)
		//If we don't exit here we can save the defaults
		configToSave.SaveDefault()

		if (config.Commands.ListSources) {
			result, err := tail.ListAllSources()
			if err != nil {
				Error.Fatalln("Error in executing search query.", err)
			}
			tail.processSources(result)
		} else {
			tail.Start(config.InitialEntries)
		}

	}

	app.Run(os.Args)

}

// Helper function to avoid boilerplate error handling for regex matches
// this way they may be used in single value context
func Must(result bool, err error) bool {
	if err != nil {
		Error.Panic(err)
	}
	return result
}

// Read password from the console
func readPasswd() string {
	bytePassword, err := terminal.ReadPassword(0)
	if err != nil {
		Error.Fatalln("Failed to read password.")
	}
	fmt.Println()
	return string(bytePassword)
}

// Expression evaluation function. It uses map as a model and evaluates expression given as
// the parameter using dot syntax:
// "foo" evaluates to model[foo]
// "foo.bar" evaluates to model[foo][bar]
// If a key given in the expression does not exist in the model, function will return empty string and
// an error.
func EvaluateExpression(model interface{}, fieldExpression string) (string, error) {
	if fieldExpression == "" {
		return fmt.Sprintf("%v", model), nil
	}
	parts := strings.SplitN(fieldExpression, ".", 2)
	expression := parts[0]
	var nextModel interface{} = ""
	modelMap, ok := model.(map[string]interface{})
	if ok {
		value := modelMap[expression]
		if value != nil {
			nextModel = value
		} else {
			return "", errors.New(fmt.Sprintf("Failed to evaluate expression %s on given model (model map does not contain that key?).", fieldExpression))
		}
	} else {
		return "", errors.New(fmt.Sprintf("Model on which %s is to be evaluated is not a map.", fieldExpression))
	}
	nextExpression := ""
	if len(parts) > 1 {
		nextExpression = parts[1]
	}
	return EvaluateExpression(nextModel, nextExpression)
}
