/*
Copyright 2021-2023 by Milo Christiansen

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

package timelog

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/milochristiansen/ledger/parse/lex"
)

// The time/date format used in the time log file.
const TimeFormat = "2006/01/02 03:04PM"
const TimeShortFormat = "03:04PM"

// TimeLog is a simple log of events, which collectively divide a period up into smaller periods.
type TimeLog []*Event

// Event marks the end of one time period and the start of another.
type Event struct {
	At   time.Time
	Code string
	Desc string
}

func (e *Event) String() string {
	return fmt.Sprintf("%s [%s] %s", e.At.Format(TimeFormat), e.Code, e.Desc)
}

// Codes returns a list of all the time codes present in the TimeLog.
func (log TimeLog) Codes() []string {
	codes := map[string]bool{}
	for _, item := range log {
		if strings.TrimSpace(item.Code) == "" {
			continue
		}
		codes[item.Code] = true
	}

	out := []string{}
	for code := range codes {
		out = append(out, code)
	}
	return out
}

// CodeLen returns the length of the longest time code in the log.
func (log TimeLog) CodeLen() int {
	codes := log.Codes()
	max := 0
	for _, code := range codes {
		if len(code) > max {
			max = len(code)
		}
	}
	return max
}

// Format dumps a TimeLog to an [io.Writer], one [Event] per line.
func (log TimeLog) Format(w io.Writer) error {
	cl := log.CodeLen()

	for _, item := range log {
		_, err := fmt.Fprintf(w, "%s [%*s] %s\n", item.At.Format(TimeFormat), cl, item.Code, item.Desc)
		if err != nil {
			return err
		}
	}
	return nil
}

// String dumps the TimeLog to a string. It should not be possible for this to fail, but outside of testing please
// use [TimeLog.Format] and handle your errors!
func (log TimeLog) String(w io.Writer) string {
	out := new(bytes.Buffer)
	_ = log.Format(out) // No error *should* be possible here, simple writes to a Buffer are pretty robust.
	return out.String()
}

// ParseTimeLogString parses a TimeLog from the given string.
func ParseTimeLogString(input string) (TimeLog, error) {
	return parseTimeLog(lex.NewCharReader(input, 1))
}

// ParseTimeLog parses a TimeLog from the given [io.RuneReader].
func ParseTimeLog(input io.RuneReader) (TimeLog, error) {
	return parseTimeLog(lex.NewRawCharReader(input, 1))
}

// A lot of this code comes from my Ledger parser.
func parseTimeLog(cr *lex.CharReader) (TimeLog, error) {
	log := []*Event{}
	for !cr.EOF {
		// Eat any leading white space, also lines that are blank.
		cr.Eat(" \t")
		if cr.C == '\n' {
			cr.Next()
			continue
		}

		// Consume comments.
		if cr.C == '#' {
			cr.EatUntil("\n")
			cr.Next()
			continue
		}

		current := &Event{}

		// Parse the date/time
		date, err := parseDate(cr)
		if err != nil {
			return nil, err
		}
		current.At = date

		// Whitespace
		cr.Eat(" \t")
		if cr.EOF {
			return nil, ErrUnexpectedEnd(cr.L)
		}

		// Time code
		if cr.C == '[' {
			cr.Next()
			cr.Eat(" \t")
			desc, err := readUntilTrimmed(cr, "]")
			if err != nil {
				return nil, err
			}
			if cr.C == '\n' {
				return nil, ErrMalformed(cr.L)
			}
			current.Code = desc
			cr.Next()
		}

		// Even more ws
		cr.Eat(" \t")
		if cr.EOF {
			return nil, ErrUnexpectedEnd(cr.L)
		}

		// And, to cap it off, the description.
		desc, err := readUntilTrimmed(cr, "\n")
		if err != nil {
			return nil, err
		}
		current.Desc = desc
		cr.Next()

		log = append(log, current)
	}

	return log, nil
}

// readUntilTrimmed reads characters from the [lex.CharReader] until one of the characters in `chars` is found.
// The result then has all the whitespace trimmed from the ends.
func readUntilTrimmed(cr *lex.CharReader, chars string) (string, error) {
	ln := []rune{}
	ln = cr.ReadUntil(chars, ln)
	if cr.EOF {
		return "", ErrUnexpectedEnd(cr.L)
	}
	// Trim trailing ws
	for i := len(ln) - 1; i > 0; i-- {
		if ln[i] != ' ' && ln[i] != '\t' {
			break
		}
		ln = ln[:i]
	}
	// Trim leading ws
	for i := 0; i < len(ln); i++ {
		if ln[0] != ' ' && ln[0] != '\t' {
			break
		}
		ln = ln[1:]
	}
	return string(ln), nil
}

// parseDate reads a date and time (in yyyy/mm/dd hh:mmPM format) from the [lex.CharReader].
func parseDate(cr *lex.CharReader) (time.Time, error) {
	date := []rune{}
	ok := false
	var t time.Time

	// "2006"
	ok, date = cr.ReadMatchLimit("0123456789", date, 4)
	if !ok {
		return t, ErrBadDate(cr.L)
	}

	// "2006/"
	if !cr.Match("/-.") {
		return t, ErrBadDate(cr.L)
	}
	date = append(date, '/')
	cr.Next()

	// "2006/01"
	ok, date = cr.ReadMatchLimit("0123456789", date, 2)
	if !ok {
		return t, ErrBadDate(cr.L)
	}

	// "2006/01/"
	if !cr.Match("/-.") {
		return t, ErrBadDate(cr.L)
	}
	date = append(date, '/')
	cr.Next()

	// "2006/01/02"
	ok, date = cr.ReadMatchLimit("0123456789", date, 2)
	if !ok {
		return t, ErrBadDate(cr.L)
	}

	// "2006/01/02 "
	if !cr.Match(" ") {
		return t, ErrBadDate(cr.L)
	}
	date = append(date, ' ')
	cr.Next()

	// "2006/01/02 03"
	ok, date = cr.ReadMatchLimit("0123456789", date, 2)
	if !ok {
		return t, ErrBadDate(cr.L)
	}

	// "2006/01/02 03:"
	if !cr.Match(":") {
		return t, ErrBadDate(cr.L)
	}
	date = append(date, ':')
	cr.Next()

	// "2006/01/02 03:04"
	ok, date = cr.ReadMatchLimit("0123456789", date, 2)
	if !ok {
		return t, ErrBadDate(cr.L)
	}

	// "2006/01/02 03:04P"
	ok, date = cr.ReadMatchLimit("apAP", date, 1)
	if !ok {
		return t, ErrBadDate(cr.L)
	}

	// "2006/01/02 03:04PM"
	ok, date = cr.ReadMatchLimit("mM", date, 1)
	if !ok {
		return t, ErrBadDate(cr.L)
	}

	return time.ParseInLocation(TimeFormat, string(date), time.Local)
}

// ErrBadDate is returned by the parser when it attempts to consume an invalid date.
type ErrBadDate lex.Location

func (err ErrBadDate) Error() string {
	return fmt.Sprintf("Malformed event date on line: %v", lex.Location(err))
}

// ErrUnexpectedEnd is returned by the parser when the end of input is found unexpectedly.
type ErrUnexpectedEnd lex.Location

func (err ErrUnexpectedEnd) Error() string {
	return fmt.Sprintf("Unexpected end of input on line: %v", lex.Location(err))
}

// ErrMalformed is returned by the parser when it finds a malformed [Event].
type ErrMalformed lex.Location

func (err ErrMalformed) Error() string {
	return fmt.Sprintf("Malformed event on line: %v", lex.Location(err))
}
