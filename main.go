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
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"text/template"
	"time"

	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/manifoldco/promptui"
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
// 9: Could not find/read report file

//go:embed reports/*
var builtinReports embed.FS

type ReportData struct {
	Begin   *time.Time
	End     *time.Time
	Periods []*timelog.Period
	Totals  map[string]time.Duration

	Weeks []*ReportWeek
}

type ReportWeek struct {
	Year   int // 4 digit year
	Number int // ISO Week number

	Periods []*timelog.Period

	Totals map[string][8]time.Duration // Mon-Sun, plus week total
	Daily  [8]time.Duration            // Totals for all codes
}

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
		fmt.Fprintln(os.Stderr, "    to only events that match the time code. If no code is provided, the 'all'")
		fmt.Fprintln(os.Stderr, "    code is automatically added.")
		fmt.Fprintln(os.Stderr, "    The special hardcoded timecode 'empty' may be used to output periods that")
		fmt.Fprintln(os.Stderr, "    have a blank timecode, and the code 'all' will output all periods that")
		fmt.Fprintln(os.Stderr, "    have a non-blank timecode.")
		fmt.Fprintln(os.Stderr, "    To actually see all events, you must use 'empty' and 'all' together!")
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
		fmt.Fprintln(os.Stderr, "    the main body of the text, then the whole text is used unmodified. To")
		fmt.Fprintln(os.Stderr, "    define a new code, create the event, then set the code with 'code'.")
		os.Exit(2)
	}

	ToolMode := false
	if os.Args[0] == "timetool" {
		ToolMode = true
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
		"logfile":    "$HOME/sctime.log",
		"codefile":   "$CONFIG/codes.txt",
		"reportsdir": "$CONFIG/reports",
	}

	configraw, err := os.ReadFile(configdir + "/config.ini")
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintln(os.Stderr, "Config file does not exist, writing defaults.")
		file, err := os.Create(configdir + "/config.ini")
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error opening config file for writing:")
			fmt.Fprintln(os.Stderr, err)
			os.Exit(6)
		}
		for k, v := range config {
			fmt.Fprintln(file, k+"="+v)
		}
		file.Close()

		err = os.MkdirAll(configdir + "/reports", 0777)
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error ensuring existence of reports directory:")
			fmt.Fprintln(os.Stderr, err)
			os.Exit(6)
		}

		// We did all the stuff we wanted to do, but this whole branch is still an error condition, so...
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
	codesraw, err := os.ReadFile(config["codefile"])
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading timecode file:")
		fmt.Fprintln(os.Stderr, err)
		os.Exit(7)
	}
	codes := strings.Split(string(codesraw), "\n")
	for i := range codes {
		codes[i] = strings.TrimSpace(codes[i])
	}
	codes = slices.DeleteFunc(codes, func(e string) bool {
		if e == "" {
			return true
		}
		if strings.HasPrefix(e, "#") || strings.HasPrefix(e, "//") || strings.HasPrefix(e, ";") {
			return true
		}
		return false
	})

	// Create a timecode tree for hierarchical filtering.
	codetree := timelog.GenerateTimecodeTree(codes)

	// Now on to our regularly scheduled program

	// Open the timesheet
	sheetF, err := os.OpenFile(config["logfile"], os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(8)
	}
	defer sheetF.Close()

	content, err := io.ReadAll(sheetF)
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

	// Reporting
	if os.Args[1] == "report" {
		// Load the templates
		templates := template.New("")
		loadTemplatesFrom(builtinReports, templates)
		loadTemplatesFrom(os.DirFS(config["reportsdir"]), templates)

		begin, end, fcode, template := ParseReportRequest(os.Args[2:], append(codes, "empty", "all"), templates)

		var all []*timelog.Period
		if end == nil {
			all = log.After(*begin).Periods()
		} else {
			all = log.Between(*begin, *end).Periods()
		}

		if len(fcode) == 0 {
			fcode = append(fcode, "all")
			fmt.Fprintln(os.Stderr, "No timecodes provided, using 'all'")
		} else {
			fmt.Fprintf(os.Stderr, "Timecodes: %v\n", strings.Join(fcode, ", "))
		}

		var periods []*timelog.Period
		for _, code := range fcode {
			if code == "empty" {
				periods = append(periods, timelog.FilterInPeriods(all, "")...)
				all = timelog.FilterOutPeriods(all, "")
				continue
			}
			if code == "all" {
				periods = append(periods, timelog.FilterOutPeriods(all, "")...)
				all = timelog.FilterInPeriods(all, "")
				continue
			}

			code, hasWildcard := strings.CutSuffix(code, ":...")

			if hasWildcard {
				periods = append(periods, timelog.FilterInPeriodsChildren(all, code, codetree)...)
				continue
			}
			periods = append(periods, timelog.FilterInPeriods(all, code)...)
			all = timelog.FilterOutPeriods(all, code)
		}

		// Since the way we build the event list leaves them in whatever jumbled up order they happen to end up in, sort.
		sort.Slice(periods, func(i, j int) bool {
			return periods[i].Begin.Before(periods[j].Begin)
		})

		if end == nil {
			fmt.Fprintf(os.Stderr, "Periods after: %v\n", begin.Format(timelog.TimeFormat))
		} else {
			fmt.Fprintf(os.Stderr, "Periods between: %v - %v\n", begin.Format(timelog.TimeFormat), end.Format(timelog.TimeFormat))
		}

		if len(periods) == 0 {
			fmt.Fprintln(os.Stderr, "No periods in given time range.")
			return
		}

		running := map[string]time.Duration{}
		for _, p := range periods {
			running[p.Code] += p.Length()
		}

		// Now, generate the week data
		weeks := []*ReportWeek{}
		var cw *ReportWeek
		for _, p := range periods {
			cy, cwn := p.Begin.ISOWeek()
			if cw == nil || cwn != cw.Number || cy != cw.Year {
				cw = &ReportWeek{Year: cy, Number: cwn, Totals: map[string][8]time.Duration{}}
				weeks = append(weeks, cw)
			}

			cw.Periods = append(cw.Periods, p)
			d := p.Begin.Weekday() - 1
			if d < 0 {
				d = 6
			}
			v := cw.Totals[p.Code]
			v[d] = v[d] + p.Length()
			v[7] = v[7] + p.Length()
			cw.Totals[p.Code] = v
			cw.Daily[d] = cw.Daily[d] + p.Length()
			cw.Daily[7] = cw.Daily[7] + p.Length()
		}

		w := tabwriter.NewWriter(os.Stdout, 2, 4, 1, ' ', 0)
		err = template.Execute(w, ReportData{
			Begin:   begin,
			End:     end,
			Periods: periods,
			Totals:  running,
			Weeks:   weeks,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "Error executing report template:")
			fmt.Fprintln(os.Stderr, err)
			return
		}
		w.Flush()

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

		last.At, _, _ = ParseLine(os.Args[2:], nil, false)
		fmt.Printf("Changed last event time to: %v\n", last.At.Format(timelog.TimeFormat))

	// Fix time codes
	case os.Args[1] == "code":
		if last == nil {
			fmt.Fprintln(os.Stderr, "No events found.")
			os.Exit(1)
		}

		last.Code = strings.Join(os.Args[2:], " ")

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
			err := os.WriteFile("", []byte(strings.Join(codes, "\n")), 0666)
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

		t, c, d := ParseLine(os.Args[2:], codes, !ToolMode)
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
		t, c, d := ParseLine(os.Args[1:], codes, !ToolMode)
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
	Code     string
	Found    string
	Distance int
}

// FindAllTimecodes finds all possible timecodes in the input line, and returns them ranked by how likely they are.
func FindAllTimecodes(candidates []string, codes []string) (map[string][]FoundCode, int) {
	// Get a list of all possible timecode candidates
	foundcodes := map[string][]FoundCode{}
	for _, candidate := range candidates {
		if strings.HasPrefix(candidate, ":") {
			foundcodes[strings.TrimPrefix(candidate, ":")] = []FoundCode{}
		}
	}

	total := 0

	// Match each candidate against the possible codes.
	for candidate := range foundcodes {
		for _, code := range codes {
			c, hasWildcard := strings.CutSuffix(candidate, ":...")

			found := fuzzy.RankMatchNormalizedFold(c, code)
			if found == -1 {
				continue
			}

			total++
			if hasWildcard {
				foundcodes[candidate] = append(foundcodes[candidate], FoundCode{Code: code + ":...", Found: candidate, Distance: found})
				continue
			}
			foundcodes[candidate] = append(foundcodes[candidate], FoundCode{Code: code, Found: candidate, Distance: found})
		}

		sort.Slice(foundcodes[candidate], func(i, j int) bool {
			return foundcodes[candidate][i].Distance < foundcodes[candidate][j].Distance
		})
	}

	// Clear everything from the candidate map that didn't get any matches.
	maps.DeleteFunc(foundcodes, func(k string, v []FoundCode) bool {
		return len(v) == 0
	})

	return foundcodes, total
}

var DateParser = dateparser.Parser{}

// Returns the first time found, a time code if one is found, and the whole line with minor editing.
func ParseLine(l []string, codes []string, canprompt bool) (time.Time, string, string) {
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
	found, total := FindAllTimecodes(l, codes)
	if total > 1 && canprompt {
		fmt.Fprintln(os.Stdout, "Multiple possible time codes found in input:")

		foundstrings := []string{}
		foundmap := []struct {
			c string
			i int
		}{}
		for c, l := range found {
			for i, v := range l {
				foundstrings = append(foundstrings, v.Code)
				foundmap = append(foundmap, struct {
					c string
					i int
				}{c, i})
			}
		}

		prompt := promptui.Select{
			Label: "Select Code",
			Items: foundstrings,
		}
		i, _, err := prompt.Run()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}

		code = found[foundmap[i].c][foundmap[i].i]
	} else if len(found) > 0 {
		if total > 1 {
			fmt.Fprintln(os.Stderr, "Multiple possible time codes found in input, picking best match.")
		}

		best := FoundCode{Distance: -1}
		for _, item := range found {
			if item[0].Distance < best.Distance || best.Distance == -1 {
				best = item[0]
			}
		}
		code = best
	}

	// If the time code and time prefix the string (in any order), strip them.
	for _, v := range []string{":" + code.Found, times[0].Text, ":" + code.Found} {
		if strings.HasPrefix(whole, v) {
			whole = strings.TrimSpace(strings.TrimPrefix(whole, v))
		}
	}

	// Strip the prefix colon from the first occurrence of the chosen timecode.
	whole = strings.Replace(whole, ":"+code.Found, code.Found, 1)

	return times[0].Date.Time.Round(6 * time.Minute), code.Code, whole
}

// Returns the first two times found and a code if provided.
func ParseReportRequest(l []string, codes []string, reports *template.Template) (*time.Time, *time.Time, []string, *template.Template) {
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
	found, _ := FindAllTimecodes(l, codes)
	var foundcodes []string
	for _, f := range found {
		foundcodes = append(foundcodes, f[0].Code)
	}

	// Find the template
	foundtemplates := []*template.Template{}
	for _, word := range l {
		foundtmpl := reports.Lookup(word)
		if foundtmpl != nil {
			foundtemplates = append(foundtemplates, foundtmpl)
		}
	}

	template := reports.Lookup("default.tmpl")
	if len(foundtemplates) > 1 {
		fmt.Fprintln(os.Stderr, "Multiple templates found in input, using first one found.")
	}

	if len(foundtemplates) != 0 {
		template = foundtemplates[0]
	}

	if len(times) > 1 {
		return &begin, &end, foundcodes, template
	}
	return &begin, nil, foundcodes, template
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

func loadTemplatesFrom(f fs.FS, t *template.Template) {
	err := fs.WalkDir(f, ".", func(path string, d fs.DirEntry, err error) error {
		if d == nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}

		if d.IsDir() {
			return nil
		}

		if strings.HasSuffix(d.Name(), ".tmpl") {
			file, err := f.Open(path)
			if err != nil {
				return err
			}
			content, err := io.ReadAll(file)
			if err != nil {
				return err
			}

			nt := t.Lookup(d.Name())
			if nt == nil {
				_, err = t.New(d.Name()).Parse(string(content))
				return err
			}
			_, err = nt.Parse(string(content))
			return err
		}

		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error reading report templates:")
		fmt.Fprintln(os.Stderr, err)
		os.Exit(9)
	}
	return
}
