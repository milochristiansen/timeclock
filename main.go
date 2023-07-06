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
	"fmt"
	"io/ioutil"
	"os"
	"strings"
	"time"

	"github.com/markusmobius/go-dateparser"

	"github.com/milochristiansen/timeclock/timelog"
)

func main() {
	if len(os.Args) < 2 {
		// Make this smarter? Write or find a formatter that can wrap text with indentation based on current terminal width. 
		fmt.Fprintln(os.Stderr, "No arguments provided. Cannot determine action.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "'time'")
		fmt.Fprintln(os.Stderr, "   Edit last event time, provide a time as an argument.")
		fmt.Fprintln(os.Stderr, "'code'")
		fmt.Fprintln(os.Stderr, "    Edit last event time code, provide new code as an argument.")
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
		fmt.Fprintln(os.Stderr, "No command word.")
		fmt.Fprintln(os.Stderr, "    Create a new event. The entire command line is used to define the event.")
		fmt.Fprintln(os.Stderr, "    To be valid, all that is required is a time. If the time and/or time code")
		fmt.Fprintln(os.Stderr, "    are the first things on the command line they will be stripped and the")
		fmt.Fprintln(os.Stderr, "    remaining text will be used as the description. If they are embedded in")
		fmt.Fprintln(os.Stderr, "    the main body of the text, then the whole text is used unmodified. Time")
		fmt.Fprintln(os.Stderr, "    codes are defined by matching existing codes in the log. To define a new")
		fmt.Fprintln(os.Stderr, "    code, create the event, then set the code with 'code'.")
		os.Exit(1)
	}

	sheetP := os.ExpandEnv("${HOME}/Sync/time.log")

	// Open the timesheet
	sheetF, err := os.OpenFile(sheetP, os.O_RDWR | os.O_CREATE, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer sheetF.Close()

	content, err := ioutil.ReadAll(sheetF)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	log, err := timelog.ParseTimeLogString(string(content))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	log.Sort()

	codes := log.Codes()

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

		// TODO: Try to add some code in here to detect if the command is run in a git repo, and if so try to grab
		// commits and match them to the periods.

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
			fmt.Fprintln(os.Stderr, "No events found to edit.")
			os.Exit(1)
		}

		last.At, _, _ = ParseLine(os.Args[2:], nil)
		fmt.Printf("Changed last event time to: %v\n", last.At.Format(timelog.TimeFormat))

	// Fix time codes
	case os.Args[1] == "code":
		if last == nil {
			fmt.Fprintln(os.Stderr, "No events found to edit.")
			os.Exit(1)
		}

		last.Code = strings.Join(os.Args[2:], " ")
		fmt.Printf("Changed last event time code to: %v\n", last.Code)

	// Fix descriptions
	case os.Args[1] == "desc":
		fallthrough
	case os.Args[1] == "note":
		if last == nil {
			fmt.Fprintln(os.Stderr, "No events found to edit.")
			os.Exit(1)
		}

		last.Desc = strings.Join(os.Args[2:], " ")
		fmt.Printf("Changed last event description to: %v\n", last.Desc)

	// Handle the current state report.
	case os.Args[1] == "status":
		if last == nil {
			fmt.Println("No events.")
			return
		}

		fmt.Println(last.String())
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
			fmt.Fprintf(os.Stderr, "%s\n == %.1fh ==>\n", old.String(), last.At.Sub(old.At).Hours())
		}
		fmt.Fprintf(os.Stderr, "%s\n", last.String())
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

	// Try to find a time code. Prefer the first one, and if two start at the same spot, prefer the longest one.
	code := ""
	at := -1
	for _, c := range codes {
		nat := strings.Index(whole, c)
		if nat >= 0 && ((nat < at) || (nat == at && len(c) > len(code)) || at == -1) {
			if at != -1 {
				fmt.Fprintln(os.Stderr, "Multiple time codes found in input, using first/longest one found.")
			}
			code = c
			at = nat
		}
	}

	// If the time code and time prefix the string (in any order), strip them.
	for _, v := range []string{code, times[0].Text, code} {
		if strings.HasPrefix(whole, v) {
			whole = strings.TrimSpace(strings.TrimPrefix(whole, v))
		}
	}

	return times[0].Date.Time.Round(6 * time.Minute), code, whole
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

	// Try to find a time code. Prefer the first one, and if two start at the same spot, prefer the longest one.
	code := ""
	at := -1
	for _, c := range codes {
		nat := strings.Index(whole, c)
		if nat >= 0 && ((nat < at) || (nat == at && len(c) > len(code)) || at == -1) {
			if at != -1 {
				fmt.Fprintln(os.Stderr, "Multiple time codes found in input, using first/longest one found.")
			}
			code = c
			at = nat
		}
	}

	if len(times) > 1 {
		return &begin, &end, code
	}
	return &begin, nil, code
}
