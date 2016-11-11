package main

import (
	"gopkg.in/olivere/elastic.v2"
	"strings"
	"time"
	"fmt"
	"encoding/json"
	"github.com/fatih/color"
	"regexp"
)

//
// Structure that holds data necessary to perform tailing.
//
type Tail struct {
	client          *elastic.Client  //elastic search client that we'll use to contact EL
	queryDefinition *QueryDefinition //structure containing query definition and formatting
	indices         []string         //indices to search through
	lastTimeStamp   string           //timestamp of the last result
	order           bool             //search order - true = ascending (may be reversed in case date-after filtering)
}

// Selects appropriate indices in EL based on configuration. This basically means that if query is date filtered,
// then it attempts to select indices in the filtered date range, otherwise it selects the last index.
func (tail *Tail) selectIndices(configuration *Configuration) {
	indices, err := tail.client.IndexNames()
	if err != nil {
		Error.Fatalln("Could not fetch available indices.", err)
	}

	if configuration.QueryDefinition.IsDateTimeFiltered() {
		startDate := configuration.QueryDefinition.AfterDateTime
		endDate := configuration.QueryDefinition.BeforeDateTime
		if startDate == "" && endDate != "" {
			lastIndex := findLastIndex(indices, configuration.SearchTarget.IndexPattern)
			lastIndexDate := extractYMDDate(lastIndex, ".")
			if lastIndexDate.Before(extractYMDDate(endDate, "-")) {
				startDate = lastIndexDate.Format(dateFormatDMY)
			} else {
				startDate = endDate
			}
		}
		if endDate == "" {
			endDate = time.Now().Format(dateFormatDMY)
		}
		tail.indices = findIndicesForDateRange(indices, configuration.SearchTarget.IndexPattern, startDate, endDate)

	} else {
		index := findLastIndex(indices, configuration.SearchTarget.IndexPattern)
		result := [...]string{index}
		tail.indices = result[:]
	}
	Info.Printf("Using indices: %s", tail.indices)
}

// Start the tailer
func (t *Tail) Start(follow bool, initialEntries int) {
	result, err := t.initialSearch(initialEntries)
	if err != nil {
		Error.Fatalln("Error in executing search query.", err)
	}
	t.processResults(result)
	delay := 500 * time.Millisecond
	for follow {
		time.Sleep(delay)
		if t.lastTimeStamp != "" {
			//we can execute follow up timestamp filtered query only if we fetched at least 1 result in initial query
			result, err = t.client.Search().
				Indices(t.indices...).
				Sort(t.queryDefinition.TimestampField, false).
				From(0).
				Size(9000).//TODO: needs rewrite this using scrolling, as this implementation may loose entries if there's more than 9K entries per sleep period
				Query(t.buildTimestampFilteredQuery()).
				Do()
		} else {
			//if lastTimeStamp is not defined we have to repeat the initial search until we get at least 1 result
			result, err = t.initialSearch(initialEntries)
		}
		if err != nil {
			Error.Fatalln("Error in executing search query.", err)
		}
		t.processResults(result)

		//Dynamic delay calculation for determining delay between search requests
		if result.TotalHits() > 0 && delay > 500 * time.Millisecond {
			delay = 500 * time.Millisecond
		} else if delay <= 2000 * time.Millisecond {
			delay = delay + 500 * time.Millisecond
		}
	}
}

// Initial search needs to be run until we get at least one result
// in order to fetch the timestamp which we will use in subsequent follow searches
func (t *Tail) initialSearch(initialEntries int) (*elastic.SearchResult, error) {
	return t.client.Search().
		Indices(t.indices...).
		Sort(t.queryDefinition.TimestampField, t.order).
		Query(t.buildSearchQuery()).
		From(0).Size(initialEntries).
		Do()
}

func (t *Tail) ListAllSources() (*elastic.SearchResult, error) {
	tagg := elastic.NewTermsAggregation().Field("source").Size(100).Order("_term", true)
	return t.client.Search().
		Indices(t.indices...).
		Aggregation("source", tagg).
		Size(0).
		Do()
}

func (t *Tail) processSources(searchResult *elastic.SearchResult) {
	sources, ok := searchResult.Aggregations.Terms("source")
	if ok {
		for _, res := range sources.Buckets {
			fmt.Println(res.Key)
		}
	}
}


// Process the results (e.g. prints them out based on configured format)
func (t *Tail) processResults(searchResult *elastic.SearchResult) {
	//Trace.Printf("Fetched page of %d results out of %d total.\n", len(searchResult.Hits.Hits), searchResult.Hits.TotalHits)
	hits := searchResult.Hits.Hits

	if t.order {
		for i := 0; i < len(hits); i++ {
			hit := hits[i]
			entry := t.processHit(hit)
			t.lastTimeStamp = entry[t.queryDefinition.TimestampField].(string)
		}
	} else {
		//when results are in descending order, we need to process them in reverse
		for i := len(hits) - 1; i >= 0; i-- {
			hit := hits[i]
			entry := t.processHit(hit)
			t.lastTimeStamp = entry[t.queryDefinition.TimestampField].(string)
		}
	}
}

func (t *Tail) processHit(hit *elastic.SearchHit) map[string]interface{} {
	var entry map[string]interface{}
	err := json.Unmarshal(*hit.Source, &entry)
	if err != nil {
		Error.Fatalln("Failed parsing ElasticSearch response.", err)
	}
	t.printResult(entry)
	return entry
}

// Print result according to format
func (t *Tail) printResult(entry map[string]interface{}) {
	//Trace.Println("Result: ", entry)
	fields := formatRegexp.FindAllString(t.queryDefinition.Format, -1)
	//Trace.Println("Fields: ", fields)
	result := t.queryDefinition.Format
	for _, f := range fields {
		value, err := EvaluateExpression(entry, f[1:])
		if f == "%@timestamp" {

			parsedTime, err := time.Parse(time.RFC3339, value)
			if err == nil {
				formattedTime := parsedTime.In(localTz).Format("2006-01-02 15:04:05.999")
				value = color.GreenString(rightPad2Len(formattedTime, " ", 23))
			} else {
				Trace.Println("parsing error: ", err)
			}
		} else if f == "%x_request_id" && len(value) > 0 {
			value = color.MagentaString(value)
		} else if f == "%source" && len(value) > 0 {
			value = color.CyanString(value)
		}
		if err == nil {
			result = strings.Replace(result, f, value, -1)
		}
	}
	fmt.Println(result)
}

func rightPad2Len(s string, padStr string, overallLen int) string {
	var padCountInt int
	padCountInt = 1 + ((overallLen - len(padStr)) / len(padStr))
	var retStr = s + strings.Repeat(padStr, padCountInt)
	return retStr[:overallLen]
}

func (t *Tail) buildSearchQuery() elastic.Query {
	var query elastic.Query

	elastic.SetTraceLog(Trace)

	if len(t.queryDefinition.Terms) > 0 {
		queryTerms := []string{}
		for _, term := range t.queryDefinition.Terms {
			if strings.HasPrefix(term, "id") {
				tokens := strings.Split(term, ":")
				query = elastic.NewTermFilter("x_request_id", tokens[1])
				Trace.Printf("Adding x_request_id filter %s", tokens[1])
			} else {
				queryTerms = append(queryTerms, term)
			}
		}
		if len(queryTerms) > 0 {
			result := strings.Join(queryTerms, " ")
			Trace.Printf("Running query string query: %s", result)
			query = elastic.NewQueryStringQuery(result)
		}
	} else {
		Trace.Print("Running query match all query.")
		query = elastic.NewMatchAllQuery()
	}

	if (t.queryDefinition.isSourceFiltered()) {
		sources := strings.Split(t.queryDefinition.Source, ",")
		Trace.Printf("Adding source filter %s", sources)
		query = elastic.NewFilteredQuery(query).Filter(elastic.NewTermsFilter("source", sources))
	}

	if t.queryDefinition.IsDateTimeFiltered() {
		// we have date filtering turned on, apply filter
		filter := t.buildDateTimeRangeFilter()
		query = elastic.NewFilteredQuery(query).Filter(filter)
	}
	return query
}

//Builds range filter on timestamp field. You should only call this if start or end date times are defined
//in query definition
func (t *Tail) buildDateTimeRangeFilter() elastic.RangeFilter {
	filter := elastic.NewRangeFilter(t.queryDefinition.TimestampField)
	if t.queryDefinition.AfterDateTime != "" {
		Trace.Printf("Date range query - timestamp after: %s", t.queryDefinition.AfterDateTime)
		filter = filter.IncludeLower(true).
			From(t.queryDefinition.AfterDateTime)
	}
	if t.queryDefinition.BeforeDateTime != "" {
		Trace.Printf("Date range query - timestamp before: %s", t.queryDefinition.BeforeDateTime)
		filter = filter.IncludeUpper(false).
			To(t.queryDefinition.BeforeDateTime)
	}
	return filter
}

func (t *Tail) buildTimestampFilteredQuery() elastic.Query {
	query := elastic.NewFilteredQuery(t.buildSearchQuery()).Filter(
		elastic.NewRangeFilter(t.queryDefinition.TimestampField).
			IncludeUpper(false).
			Gt(t.lastTimeStamp))
	return query
}

func findIndicesForDateRange(indices []string, indexPattern string, startDate string, endDate string) []string {
	start := extractYMDDate(startDate, "-")
	end := extractYMDDate(endDate, "-")
	result := make([]string, 0, len(indices))
	for _, idx := range indices {
		matched, _ := regexp.MatchString(indexPattern, idx)
		if matched {
			idxDate := extractYMDDate(idx, ".")
			if (idxDate.After(start) || idxDate.Equal(start)) && (idxDate.Before(end) || idxDate.Equal(end)) {
				result = append(result, idx)
			}
		}
	}
	return result
}

func findLastIndex(indices []string, indexPattern string) string {
	var lastIdx string
	for _, idx := range indices {
		matched, _ := regexp.MatchString(indexPattern, idx)
		if matched {
			if &lastIdx == nil {
				lastIdx = idx
			} else if idx > lastIdx {
				lastIdx = idx
			}
		}
	}
	return lastIdx
}
