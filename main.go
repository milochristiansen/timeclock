/*
Copyright 2023 by Milo Christiansen

This software is provided 'as-is', without any express or implied warranty. In
no event will the authors be held liable for any damages arising from the use of
this software.

Permission is granted to anyone to use this software for any purpose, including
commercial applications, and to alter it and redistribute it freely, subject to
the following restrictions:

1. The origin of this software must not be misrepresented; you must not claim
that you wrote the original software. If you use this software in a product, an
acknowledgment in the product documentation would be appreciated but is not
required.

2. Altered source versions must be plainly marked as such, and must not be
misrepresented as being the original software.

3. This notice may not be removed or altered from any source distribution.
*/

package main

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/markusmobius/go-dateparser"

	"github.com/milochristiansen/timeclock/timelog"
)

// Exit Codes:
// 0: OK
// 1: General error
// 2: Invalid argument count
//
// 5: Invalid environment
// 6: Could not find/read config file
// 7: Could not find/read timecode file
// 8: Could not find/read timelog file

func main() {
	if len(os.Args) < 2 {
		// Make this smarter? Write or find a formatter that can wrap text with indentation based on current terminal width. 
		fmt.Fprintln(os.Stderr, "No arguments provided. Cannot determine action.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "'time'")
		fmt.Fprintln(os.Stderr, "   Edit last event time, provide a time as an argument.")
		fmt.Fprintln(os.Stderr, "'code'")
		fmt.Fprintln(os.Stderr, "    Edit last event time code, provide new code as an argument.")
		fmt.Fprintln(os.Stderr, "    Timecodes may not contain spaces!")
		fmt.Fprintln(os.Stderr, "'desc' or 'note'")
		fmt.Fprintln(os.Stderr, "    Edit last event description, provide new description as an argument.")
		fmt.Fprintln(os.Stderr, "'status'")
		fmt.Fprintln(os.Stderr, "    Prints the current last event.")
		fmt.Fprintln(os.Stderr, "'report'")
		fmt.Fprintln(os.Stderr, "    Print a report. You must provide a time to set the start point for the")
		fmt.Fprintln(os.Stderr, "    report. Optionally, you may also provide a time code to limit the report")
		fmt.Fprintln(os.Stderr, "    to only events that match the time code.")
		fmt.Fprintln(os.Stderr, "    The special hardcoded timecode 'clean' may be used to output only periods")
		fmt.Fprintln(os.Stderr, "    that have a non-blank timecode.")
		fmt.Fprintln(os.Stderr, "'info'")
		fmt.Fprintln(os.Stderr, "    List all known time codes.")
		fmt.Fprintln(os.Stderr, "'test'")
		fmt.Fprintln(os.Stderr, "    Process all following input as if you were creating an event, but don't")
		fmt.Fprintln(os.Stderr, "    actually write anything to the timelog.")
		fmt.Fprintln(os.Stderr, "No command word.")
		fmt.Fprintln(os.Stderr, "    Create a new event. The entire command line is used to define the event.")
		fmt.Fprintln(os.Stderr, "    To be valid, all that is required is a time. If the time and/or time code")
		fmt.Fprintln(os.Stderr, "    are the first things on the command line they will be stripped and the")
		fmt.Fprintln(os.Stderr, "    remaining text will be used as the description. If they are embedded in")
		fmt.Fprintln(os.Stderr, "    the main body of the text, then the whole text is used unmodified. Time")
		fmt.Fprintln(os.Stderr, "    codes are defined by matching existing codes in the log. To define a new")
		fmt.Fprintln(os.Stderr, "    code, create the event, then set the code with 'code'.")
		os.Exit(2)
	}

	// Find/create the configuration directory.
	configdir, ok := os.LookupEnv("XDG_CONFIG_HOME")
	if !ok || configdir == "" {
		home, ok := os.LookupEnv("HOME")
		if !ok || home == "" {
			fmt.Fprintln(os.Stderr, "Both XDG_CONFIG_HOME and HOME do not exist or are invalid.")
			os.Exit(5)
		}

		configdir = home + "/.config"
	}
	configdir += "/sctime"

	err := os.MkdirAll(configdir, 0777)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error ensuring existence of config directory:")
		fmt.Fprintln(os.Stderr, err)
		os.Exit(6)
	}

	// Load the config file
	config := map[string]string{
		"logfile": "$HOME/sctime.log",
		"codefile": "$CONFIG/codes.txt",
	}

	configraw, err := ioutil.ReadFile(configdir + "/config.ini")
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "Config file does not exist, writing defaults.")
		file, err := os.Create(configdir + "/config.ini")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error opening config file for writing:")
			fmt.Fprintln(os.Stderr, err)
			os.Exit(6)
		}
		for k, v := range config {
			fmt.Fprintln(file, k + "=" + v)
		}
		file.Close()
		os.Exit(6)
	} else if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading config file:")
		fmt.Fprintln(os.Stderr, err)
		os.Exit(6)
	}

	ParseINI(string(configraw), config)

	for k := range config {
		config[k] = os.Expand(config[k], func(s string) string {
			if s == "CONFIG" {
				return configdir
			}
			return os.Getenv(s)
		})
	}

	// Load and filter timecodes
	codesraw, err := ioutil.ReadFile(config["codefile"])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading timecode file:")
		fmt.Fprintln(os.Stderr, err)
		os.Exit(7)
	}
	codes := strings.Split(string(codesraw), "\n")
	codes = slices.DeleteFunc(codes, func(e string) bool {
		if e == "" {
			return true
		}
		if strings.ContainsAny(e, " \t") {
			return true
		}
		if strings.HasPrefix(e, "#") || strings.HasPrefix(e, "//") || strings.HasPrefix(e, ";") {
			return true
		}
		return false
	})

	// Now on to our regularly scheduled program

	// Open the timesheet
	sheetF, err := os.OpenFile(config["logfile"], os.O_RDWR | os.O_CREATE, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	defer sheetF.Close()

	content, err := ioutil.ReadAll(sheetF)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}

	log, err := timelog.ParseTimeLogString(string(content))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	log.Sort()

	if os.Args[1] == "report" {
		begin, end, code := ParseReportRequest(os.Args[2:], append(codes, "clean"))
		
		var periods []*timelog.Period
		if end == nil {
			periods = log.After(*begin).Periods()
		} else {
			periods = log.Between(*begin, *end).Periods()
		}
		if code == "clean" {
			periods = timelog.FilterOutPeriods(periods, "")

			fmt.Printf("Showing report for all non-empty time codes.\n")
		} else if code != "" {
			periods = timelog.FilterInPeriods(periods, code)

			fmt.Printf("Showing report for time code: %v\n", code)
		} else {
			fmt.Printf("Showing report for all time codes.\n")
		}
		if end == nil {
			fmt.Printf("Periods after: %v\n", begin.Format(timelog.TimeFormat))
		} else {
			fmt.Printf("Periods between: %v - %v\n", begin.Format(timelog.TimeFormat), end.Format(timelog.TimeFormat))
		}

		running := map[string]time.Duration{}
		for _, p := range periods {
			running[p.Code] += p.Length()
			fmt.Println(p.String())
		}
		for c, t := range running {
			if c == "" {
				continue
			}
			fmt.Printf("%s: %2.1f hours\n", c, t.Hours())
		}

		return
	}

	// Grab the last event in the sheet for later convenience.
	var last *timelog.Event
	if len(log) > 0 {
		last = log[len(log)-1]
	}

	switch {
	// Fix times
	case os.Args[1] == "info":
		if last == nil {
			fmt.Fprintln(os.Stderr, "No events found.")
			os.Exit(1)
		}

		fmt.Printf("%v\n", strings.Join(codes, "\n"))

	// Fix times
	case os.Args[1] == "time":
		if last == nil {
			fmt.Fprintln(os.Stderr, "No events found.")
			os.Exit(1)
		}

		last.At, _, _ = ParseLine(os.Args[2:], nil)
		fmt.Printf("Changed last event time to: %v\n", last.At.Format(timelog.TimeFormat))

	// Fix time codes
	case os.Args[1] == "code":
		if last == nil {
			fmt.Fprintln(os.Stderr, "No events found.")
			os.Exit(1)
		}

		last.Code = strings.Join(os.Args[2:], ":")
		if strings.ContainsAny(last.Code, " \t") {
			fmt.Fprintln(os.Stderr, "Provided code contains whitespace.")
			os.Exit(1)
		}

		// Check if code is new, and if it is add it to the timecode file.
		found := false
		for _, v := range codes {
			if v == last.Code {
				found = true
				break
			}
		}
		if !found {
			codes = append(codes, last.Code)
			err := ioutil.WriteFile("", []byte(strings.Join(codes, "\n")), 0666)
			if last == nil {
				fmt.Fprintln(os.Stderr, "Could not write modified code file, aborted.")
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}

		fmt.Printf("Changed last event time code to: %v\n", last.Code)

	// Fix descriptions
	case os.Args[1] == "desc":
		fallthrough
	case os.Args[1] == "note":
		if last == nil {
			fmt.Fprintln(os.Stderr, "No events found.")
			os.Exit(1)
		}

		last.Desc = strings.Join(os.Args[2:], " ")
		fmt.Printf("Changed last event description to: %v\n", last.Desc)

	// Handle the current state report.
	case os.Args[1] == "status":
		if last == nil {
			fmt.Fprintln(os.Stderr, "No events found.")
			os.Exit(1)
		}

		fmt.Println(last.String())
		return

	// Test input handling.
	case os.Args[1] == "test":
		if len(os.Args) <= 2 {
			fmt.Fprintln(os.Stderr, "Not enough arguments.")
			os.Exit(1)
		}

		t, c, d := ParseLine(os.Args[2:], codes)
		last = &timelog.Event{
			At:   t,
			Code: c,
			Desc: d,
		}
		fmt.Printf("%s\n", last.String())
		if c == "" {
			fmt.Fprintln(os.Stderr, "No time code found, use 'code' to specify one.")
		}
		if d == "" {
			fmt.Fprintln(os.Stderr, "No description found, use 'note' to specify one.")
		}
		return

	// Handle the default clock in/out action
	default:
		t, c, d := ParseLine(os.Args[1:], codes)
		old := last

		if t.Before(old.At) {
			fmt.Fprintf(os.Stderr, "Given time (%s) is before previous event time (%s).\n", t.Format(timelog.TimeFormat), old.At.Format(timelog.TimeFormat))
			os.Exit(1)
		}

		last = &timelog.Event{
			At:   t,
			Code: c,
			Desc: d,
		}
		log = append(log, last)

		if old != nil {
			fmt.Printf("%s\n == %.1fh ==>\n", old.String(), last.At.Sub(old.At).Hours())
		}
		fmt.Printf("%s\n", last.String())
		if c == "" {
			fmt.Fprintln(os.Stderr, "No time code found, use 'code' to specify one.")
		}
		if d == "" {
			fmt.Fprintln(os.Stderr, "No description found, use 'note' to specify one.")
		}
	}

	// Reset the file so we can dump any output back where we got it.
	// You can't just truncate, you can't just reset the pointer, you need to do *both*
	err = sheetF.Truncate(0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_, err = sheetF.Seek(0, 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	// Dump the new timesheet.
	err = log.Format(sheetF)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

type FoundCode struct {
	Code string
	Found string
	Distance int
}

// FindAllTimecodes finds all possible timecodes in the input line, and returns them ranked by how likely they are.
func FindAllTimecodes(candidates []string, codes []string) []FoundCode {
	// Find all possible matches
	// This is done code by code to make duplicate elimination sane.
	found := []FoundCode{}
	for _, code := range codes {
		best := FoundCode{Distance: -1}
		for _, candidate := range candidates {
			// Not documented in the library, but -1 is no match, 0 is "perfect" match
			found := fuzzy.RankMatchNormalizedFold(candidate, code)
			if found == -1 || found < best.Distance {
				continue
			}

			best.Distance = found
			best.Code = code
			best.Found = candidate
		}
		if best.Distance != -1 {
			found = append(found, best)
		}
	}

	sort.Slice(found, func(i, j int) bool {
		return found[i].Distance < found[j].Distance
	})

	return found
}

var DateParser = dateparser.Parser{}

// Returns the first time found, a time code if one is found, and the whole line with minor editing.
func ParseLine(l []string, codes []string) (time.Time, string, string) {
	whole := strings.Join(l, " ")

	// Try to find a time in the description
	times, err := DateParser.SearchWithLanguage(&dateparser.Configuration{
		CurrentTime: time.Now().Local(),
	}, "en", whole)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if len(times) == 0 {
		fmt.Fprintln(os.Stderr, "No time found. (use \"now\" for current time.)")
		os.Exit(1)
	}

	if len(times) > 1 {
		fmt.Fprintln(os.Stderr, "Multiple times found in input, using first one found.")
	}

	// Try to find a time code.
	code := FoundCode{}
	found := FindAllTimecodes(l, codes)
	if len(found) > 1 {
		fmt.Fprintln(os.Stderr, "Multiple possible time codes found in input, using best match.")
	}
	if len(found) > 0 {
		code = found[0]
	}

	// If the time code and time prefix the string (in any order), strip them.
	for _, v := range []string{code.Code, times[0].Text, code.Found} {
		if strings.HasPrefix(whole, v) {
			whole = strings.TrimSpace(strings.TrimPrefix(whole, v))
		}
	}

	return times[0].Date.Time.Round(6 * time.Minute), code.Code, whole
}

// Returns the first two times found and a code if provided.
func ParseReportRequest(l []string, codes []string) (*time.Time, *time.Time, string) {
	whole := strings.Join(l, " ")

	// Try to find a time in the description
	times, err := DateParser.SearchWithLanguage(&dateparser.Configuration{
		CurrentTime: time.Now().Local(),
	}, "en", whole)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if len(times) == 0 {
		fmt.Fprintln(os.Stderr, "No time found. (use \"now\" for current time.)")
		os.Exit(1)
	}


	var begin, end time.Time

	begin = times[0].Date.Time
	if len(times) > 1 {
		end = times[1].Date.Time

		if begin.After(end) {
			begin, end = end, begin
		}
	}

	if len(times) > 2 {
		fmt.Fprintln(os.Stderr, "Multiple times found in input, using first two found.")
	}

	// Try to find a time code.
	code := FoundCode{}
	found := FindAllTimecodes(l, codes)
	if len(found) > 1 {
		fmt.Fprintln(os.Stderr, "Multiple possible time codes found in input, using best match.")
	}
	if len(found) > 0 {
		code = found[0]
	}

	if len(times) > 1 {
		return &begin, &end, code.Code
	}
	return &begin, nil, code.Code
}

// This is prehistoric code, based on stuff originally written for Rubble
func ParseINI(input string, result map[string]string) {
	lines := strings.Split(input, "\n")
	for i := range lines {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		parts[0] = strings.TrimSpace(parts[0])
		parts[1] = strings.TrimSpace(parts[1])
		if un, err := strconv.Unquote(parts[1]); err == nil {
			parts[1] = un
		}
		result[parts[0]] = parts[1]
	}
}
