package main

import (
	"gopkg.in/olivere/elastic.v2"
	"strings"
	"time"
	"fmt"
	"encoding/json"
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
	tailMode        bool
}

// Selects appropriate indices in EL based on configuration. This basically means that if query is date filtered,
// then it attempts to select indices in the filtered date range, otherwise it selects the last index.
func (tail *Tail) selectIndices(configuration *Configuration) {
	indices, err := tail.client.IndexNames()
	if err != nil {
		Error.Fatalln("Could not fetch available indices.", err)
	}

	if configuration.QueryDefinition.IsDateTimeFiltered()  {
		if configuration.QueryDefinition.Duration != "" && configuration.QueryDefinition.AfterDateTime == "" && configuration.QueryDefinition.BeforeDateTime == "" {
			configuration.QueryDefinition.DurationSpecified = true
			configuration.QueryDefinition.SetDurationAsAfterDateTime()
		}
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
func (t *Tail) Start(entriesPerBatch int) {
	result, err := t.initialSearch(entriesPerBatch)
	if err != nil {
		Error.Fatalln("Error in executing search query.", err)
	}
	t.processResults(result)

	if (t.tailMode) {
		t.InfinitelyTail(entriesPerBatch)
	} else {
		t.InfinitelyPromptUser(entriesPerBatch)
	}
}

func (t *Tail) InfinitelyPromptUser(entriesPerBatch int) {
	var result *elastic.SearchResult
	var err error
	for true {
		if shouldFetchMoreEntries() {
			result, err = t.FetchNextBatchOfEntries(entriesPerBatch)
			if err != nil {
				Error.Fatalln("Error in executing search query.", err)
			}
			t.processResults(result)
		} else {
			break
		}
	}
}

func (t *Tail) InfinitelyTail(entriesPerBatch int) {
	var result *elastic.SearchResult
	var err error
	delay := 500 * time.Millisecond
	for true {
		time.Sleep(delay)
		if t.lastTimeStamp != "" {
			//we can execute follow up timestamp filtered query only if we fetched at least 1 result in initial query
			result, err = t.FetchNextBatchOfEntries(9000) //TODO: needs rewrite this using scrolling, as this implementation may loose entries if there's more than 9K entries per sleep period
		} else {
			//if lastTimeStamp is not defined we have to repeat the initial search until we get at least 1 result
			result, err = t.initialSearch(entriesPerBatch)
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

func (t *Tail) FetchNextBatchOfEntries(entriesPerBatch int) (*elastic.SearchResult, error) {
	return t.client.Search().
		Indices(t.indices...).
		Sort(t.queryDefinition.TimestampField, false).
		From(0).
		Size(entriesPerBatch).
		Query(t.buildTimestampFilteredQuery()).
		Do()
}

// Initial search needs to be run until we get at least one result
// in order to fetch the timestamp which we will use in subsequent follow searches
func (t *Tail) initialSearch(entriesPerBatch int) (*elastic.SearchResult, error) {
	return t.client.Search().
		Indices(t.indices...).
		Sort(t.queryDefinition.TimestampField, t.order).
		Query(t.buildSearchQuery()).
		From(0).Size(entriesPerBatch).
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
	Trace.Println("Result: ", entry)
	fields := formatRegexp.FindAllString(t.queryDefinition.Format, -1)
	Trace.Println("Fields: ", fields)
	result := t.queryDefinition.Format
	for _, f := range fields {
		value, err := EvaluateExpression(entry, f[1:])
		if f == "%@timestamp" {
			parsedTime, timeErr := time.Parse(time.RFC3339, value)
			if timeErr == nil {
				formattedTime := parsedTime.In(localTz).Format(time.RFC3339Nano)
				value = paintTimestamp(formattedTime)
			} else {
				Trace.Println("parsing error: ", timeErr)
			}
		} else if f == "%x_request_id" && len(value) > 0 {
			value = paintRequestId(value)
		} else if f == "%source" && len(value) > 0 {
			value = paintSource(value)
		} else if f == "%message" && len(value) > 0 && (len(t.queryDefinition.Terms) > 0 || t.queryDefinition.Watch != "") {
			if len(t.queryDefinition.Terms) > 0 {
				toHighlight := strings.Join(t.queryDefinition.Terms, " ")
				value = strings.Replace(value, toHighlight, highlightContent(toHighlight), -1)
			} else if t.queryDefinition.Watch != "" {
				value = strings.Replace(value, t.queryDefinition.Watch, highlightContent(t.queryDefinition.Watch), -1)
			}
		}

		if err == nil {
			result = strings.Replace(result, f, value, -1)
		} else {
			result = strings.Replace(result, f, "", -1) //the field might not be available in the results
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
			queryTerms = append(queryTerms, term)
		}
		if len(queryTerms) > 0 {
			result := strings.Join(queryTerms, " ")
			Info.Printf("Filtering by keyword %s", result)
			query = elastic.NewQueryStringQuery(result).DefaultField("message").DefaultOperator("and")
		}
	} else {
		Info.Print("Filtering by no keywords...")
		query = elastic.NewMatchAllQuery()
	}

	if (t.queryDefinition.isSourceFiltered()) {
		sources := strings.Split(t.queryDefinition.Source, ",")
		Info.Printf("Adding source filter %s", sources)
		query = elastic.NewFilteredQuery(query).Filter(elastic.NewTermsFilter("source", sources))
	}

	if (t.queryDefinition.isRequestIdFiltered()) {
		Info.Printf("Adding x_request_id filter %s", t.queryDefinition.RequestId)
		query = elastic.NewFilteredQuery(query).Filter(elastic.NewTermFilter("x_request_id", t.queryDefinition.RequestId[0:8]))
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

	if t.queryDefinition.Duration != "" && t.queryDefinition.BeforeDateTime == "" {
		Trace.Printf("Duration query - entries for the past %s", t.queryDefinition.Duration)
		if t.lastTimeStamp == "" && t.queryDefinition.DurationSpecified {
			fmt.Println(paintInfoline("Querying logs after " + t.queryDefinition.AfterDateTime + ". Duration filter: " + t.queryDefinition.Duration))
		}
		t.queryDefinition.SetDurationAsAfterDateTime()
	}

	if t.queryDefinition.AfterDateTime != "" {
		Trace.Printf("Date range query - timestamp after: %s", t.queryDefinition.AfterDateTimeInUTC())
		filter = filter.IncludeLower(true).
			From(t.queryDefinition.AfterDateTimeInUTC())
	}
	if t.queryDefinition.BeforeDateTime != "" {
		Trace.Printf("Date range query - timestamp before: %s", t.queryDefinition.BeforeDateTimeInUTC())
		filter = filter.IncludeUpper(false).
			To(t.queryDefinition.BeforeDateTimeInUTC())
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
