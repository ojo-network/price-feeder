package alphavantage

import (
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"
)

var (
	// timeSeriesDateFormats are the expected date formats in time series data
	timeSeriesDateFormats = []string{
		"2006-01-02",
		"2006-01-02 15:04:05",
	}
)

type TimeSeriesValue struct {
	Time   time.Time
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

// sortTimeSeriesValuesByDate allows TimeSeriesValue
// slices to be sorted by date in ascending order
type sortTimeSeriesValuesByDate []*TimeSeriesValue

func (b sortTimeSeriesValuesByDate) Len() int           { return len(b) }
func (b sortTimeSeriesValuesByDate) Less(i, j int) bool { return b[i].Time.Before(b[j].Time) }
func (b sortTimeSeriesValuesByDate) Swap(i, j int)      { b[i], b[j] = b[j], b[i] }

// ParseTimeSeriesData will parse csv data from a reader
func ParseTimeSeriesData(r io.Reader) ([]*TimeSeriesValue, error) {

	reader := csv.NewReader(r)
	reader.ReuseRecord = true // optimization
	reader.LazyQuotes = true
	reader.TrailingComma = true
	reader.TrimLeadingSpace = true

	// strip header
	if _, err := reader.Read(); err != nil {
		if err == io.EOF {
			return nil, nil
		}
		return nil, err
	}

	values := make([]*TimeSeriesValue, 0, 64)

	for {
		record, err := reader.Read()
		if err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		value, err := parseTimeSeriesRecord(record)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}

	// sort values by date
	sort.Sort(sortTimeSeriesValuesByDate(values))

	return values, nil
}

// parseDigitalCurrencySeriesRecord will parse an individual csv record
func parseTimeSeriesRecord(s []string) (*TimeSeriesValue, error) {
	// these are the expected columns in the csv record
	const (
		timestamp = iota
		open
		high
		low
		close
		volume
	)

	value := &TimeSeriesValue{}

	d, err := parseDate(s[timestamp], timeSeriesDateFormats...)
	if err != nil {
		return nil, fmt.Errorf("%s error parsing timestamp %s", err, s[timestamp])
	}
	value.Time = d

	f, err := parseFloat(s[open])
	if err != nil {
		return nil, fmt.Errorf("%s error parsing open %s", err, s[open])
	}
	value.Open = f

	f, err = parseFloat(s[high])
	if err != nil {
		return nil, fmt.Errorf("%s error parsing high %s", err, s[high])
	}
	value.High = f

	f, err = parseFloat(s[low])
	if err != nil {
		return nil, fmt.Errorf("%s error parsing low %s", err, s[low])
	}
	value.Low = f

	f, err = parseFloat(s[close])
	if err != nil {
		return nil, fmt.Errorf("%s error parsing close %s", err, s[close])
	}
	value.Close = f

	f, err = parseFloat(s[volume])
	if err != nil {
		return nil, fmt.Errorf("%s error parsing volume %s", err, s[volume])
	}
	value.Volume = f

	return value, nil
}

// parseFloat parses a float value.
// An error is returned if the value is not a float value.
func parseFloat(val string) (float64, error) {
	return strconv.ParseFloat(val, 64)
}

// parseDate parses a date value from a string.
// An error is returned if the value is not in one of the dateFormat formats.
func parseDate(v string, dateFormat ...string) (time.Time, error) {
	for _, format := range dateFormat {
		t, err := time.Parse(format, v)
		if err != nil {
			continue
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("applicable date format not found for date %s", v)
}
