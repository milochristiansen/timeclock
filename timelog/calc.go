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

package timelog

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// After returns a TimeLog with only the [Event] items that happen after the given [time.Time].
// Editing [Event] items in the new TimeLog will also edit events in the original!
func (log TimeLog) After(t time.Time) TimeLog {
	out := []*Event{}

	for _, item := range log {
		if item.At.After(t) {
			out = append(out, item)
		}
	}

	return out
}

// Between returns a TimeLog with only the [Event] items that happen between the given [time.Time] values.
// Order of the time values does not matter.
// Editing [Event] items in the new TimeLog will also edit events in the original!
func (log TimeLog) Between(t1, t2 time.Time) TimeLog {
	if t1.After(t2) {
		t2, t1 = t1, t2
	}

	out := []*Event{}

	for _, item := range log {
		if item.At.After(t1) && item.At.Before(t2) {
			out = append(out, item)
		}
	}

	return out
}

// Sort makes sure that all Event items are nicely in order.
func (log TimeLog) Sort() {
	sort.Slice(log, func(i, j int) bool {
		return log[i].At.Before(log[j].At)
	})
}

// Period describes a time period bracketed by two events. By convention the description and time code are take from
// the event that marks the beginning of the period.
type Period struct {
	Begin time.Time
	End   time.Time
	Desc  string
	Code  string
}

func (p *Period) Length() time.Duration {
	return p.End.Sub(p.Begin)
}

func (p *Period) String() string {
	return fmt.Sprintf("%s - %s %5.1fh [%s] %s", p.Begin.Format(TimeFormat), p.End.Format(TimeShortFormat), p.Length().Hours(), p.Code, p.Desc)
}

// FilterOutPeriods removes all [Period] items that match the given time code.
func FilterOutPeriods(p []*Period, code string) []*Period {
	out := []*Period{}

	for _, item := range p {
		if item.Code != code {
			out = append(out, item)
		}
	}

	return out
}

// FilterInPeriods removes all [Period] items that *do not* match the given time code.
func FilterInPeriods(p []*Period, code string) []*Period {
	out := []*Period{}

	for _, item := range p {
		if item.Code == code {
			out = append(out, item)
		}
	}

	return out
}

// Periods takes a TimeLog and assembles the [Event] items into a set of [Period] items. The description and time code
// for each Period is taken from the Event that marks its beginning. If it is not already, the TimeLog will be sorted!
func (log TimeLog) Periods() []*Period {
	out := []*Period{}

	log.Sort()

	var last *Event
	for _, item := range log {
		if last != nil {
			out = append(out, &Period{
				Begin: last.At,
				End: item.At,
				Desc: last.Desc,
				Code: last.Code,
			})
		}
		last = item
	}

	return out
}
type TimecodeTreeNode struct {
	Kids map[string]*TimecodeTreeNode
	Self string
}
	
func GenerateTimecodeTree(codes []string) *TimecodeTreeNode {
	codetree := &TimecodeTreeNode{Kids: map[string]*TimecodeTreeNode{}, Self: "-"}
	for _, code := range codes {
		parts := strings.Split(code, ":")
		n := codetree
		sofar := ""
		for i, part := range parts {
			if i != 0 {
				sofar += ":"
			}
			sofar += part
			kid, ok := n.Kids[part]
			if !ok {
				kid = &TimecodeTreeNode{Kids: map[string]*TimecodeTreeNode{}, Self: sofar}
				n.Kids[part] = kid
			}
			n = kid
		}
	}
	return codetree
}

// FilterOutPeriods removes all [Period] items that match the given time code and its children.
func FilterOutPeriodsChildren(p []*Period, code string, codetree *TimecodeTreeNode) []*Period {
	// Find the node for the given code
	parts := strings.Split(code, ":")
	n := codetree
	for _, part := range parts {
		kid, ok := n.Kids[part]
		if !ok {
			// Code not found in tree.
			return nil
		}
		n = kid
	}

	// n is the node for the current code. Now walk down the remaining children, filtering them in.
	return filterOutPeriodsChildren(p, n)
}

func filterOutPeriodsChildren(p []*Period, n *TimecodeTreeNode) []*Period {
	for _, kid := range n.Kids {
		p = filterOutPeriodsChildren(p, kid)
	}

	return FilterOutPeriods(p, n.Self)
}

// FilterInPeriods removes all [Period] items that *do not* match the given time code and its children.
func FilterInPeriodsChildren(p []*Period, code string, codetree *TimecodeTreeNode) []*Period {
	// Find the node for the given code
	parts := strings.Split(code, ":")
	n := codetree
	for _, part := range parts {
		kid, ok := n.Kids[part]
		if !ok {
			// Code not found in tree.
			return nil
		}
		n = kid
	}

	return filterInPeriodsChildren(p, n)
}

func filterInPeriodsChildren(p []*Period, n *TimecodeTreeNode) []*Period {
	out := []*Period{}
	for _, kid := range n.Kids {
		out = append(out, filterInPeriodsChildren(p, kid)...)
	}
	return append(out, FilterInPeriods(p, n.Self)...)
}
