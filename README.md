
# Timeclock

This is a very simple timeclock I built for tracking my working hours. The overriding goal of the program was to keep
the user experience as simple and uncomplicated as possible for the common case of clocking in and out of something.

To this end, the program is very permissive of what it will accept. Times are taken in about any format you can imagine,
from `now` or `in an hour` to `2023-07-06T14:48:58+0000` (the same goes for dates of course). *Very nearly* anything you
feed in will be a valid input, and if it isn't what you meant... Well, there are commands to fix it.


## Configuration

Currently, there isn't any. The timelog is written to `${HOME}/Sync/time.log`.

Yes, I plan to change this somehow, but who knows when, how, or if ever. I like the simplicity of the current system,
but it is a bit fragile and specific to my setup. There isn't anything useful to put in a config file (oh look, another
hardcoded path!), the design of the program doesn't lend itself to a command line argument, and a more generic hardcoded
path would only make the problem less obvious.

For now, edit the path to taste (it is right at the top of `main`) before you compile.


## Building

Due to the total lack of configuration that isn't editing `main.go`, I suggest first downloading the repo somewhere
using any of the normal methods you would use to get a git repo from github.

Once you have edited the timelog path to taste, just

	go build

and move the resulting binary to somewhere that is on your path.

You will, of course, need to have the latest Go complier installed.


## Available Actions

This *should* be a full list of everything you can do with this program.


### Creating a time event

By far the most common thing you do with a timeclock is creating a time event. To do that with this program, all you
need to do is specify a time (most commonly `now`).

	timeclock now

Additionally, you can specify a time code and/or a description, but they are not required.

	timeclock now Customer Did a thing.

This will result in a event with the description "Did a thing." coded to `Customer` that happened at the time the
command was run.

Order is not generally important. You can even mix them together!

	timeclock Did a thing for Customer at 10:00am

This will result in a event with the description "Did a thing for Customer at 10:00am" coded to `Customer` that happened
at 10:00am on the day the command was run.

So, how does this work?

Pretty simply really. First, the program attempts to identify the time. It does this by searching the entire input
string for anything that could possibly be a time or date. If it finds *anything* that it can interpret as a time, it
picks the first such item and uses it as the time for the event. After doing that, it goes through the entire input
looking for any of the timecodes it knows about. Once again, if it finds one it uses the first one it finds. After that,
if the time code or the string it parsed to get the event time prefix the input (in any order) it will strip them off.
Any remaining text will then be used as an event description.


### Creating or setting a timecode

Adding an existing timecode to an event is easy, but what if you need to create a new one? For this you need the `code`
subcommand.

	timeclock code NewCode

This sets the timecode for the last time event to `NewCode`. This code does not have to be a new code, you can also use
this command to fix a case where you forgot to add a time code or specified the wrong one.


### Getting currently known timecodes

Memory like mine? Just need a quick refresher on what the currently known codes are?

	timeclock codes

This simply prints out a list of all known timecodes.


### Setting or changing the description

If you decide to add a description to an event that didn't have one, or you want to change the existing description, you
can use the `desc` subcommand (also has a `note` alias).

	timeclock desc New description.

This simply sets the description of the last event to the new description.


### Printing the current event

Sometimes you forget if you clocked in, or otherwise want to know what the timeclock thinks is going on. To this end you
can use the `status` subcommand.

	timeclock status

This will print the last time event to standard output in the form:

	2023/07/06 09:36AM [TimeCode] Description Text.


### Printing a report

A timeclock isn't any good if you can't print out a report of what you spent time on.

	timeclock report last year

This will print a time report with only events that have happened in the last year. This report will be very noisy,
and full of garbage.

To clean it up, you can specify a timecode.

	timeclock report last year Customer

This will only show events for `Customer`, however that is rarely what you actually want. Generally, what you really
want is all times that are coded to anything at all. For this you can use the special code `clean`. This timecode only
exists for this command, and tells the program to omit any events that do not have a code.

	timeclock report last year clean

If you want to get a report for a specific month, you also need an end time.

	report june 1st july 1st clean

Like the event adding code, the report code simply searches for times in the entire given input, but it will always use
the first *two* it finds. If it only finds one, it will print a report from that time to the current time, if it finds
two it will use them as start and end times. These times can be in any order. Similarly, the timecode used for filtering
is found via a search of the entire given input.


### WTF is this thing doing?

If you ever find yourself wondering how this slightly demented program will parse your input, you can use the `test`
command.

	timeclock test Go last month yourself.

This will act just like the input was being used to specify a new event, but it won't write anything to the timelog.

	2023/06/06 12:36PM [] Go last month yourself.
	No time code found, use 'code' to specify one.

(That output was from a test run on July 6th 2023)


## Timelog Format

The timelog is stored on disk in a plain text format lightly inspired by (Ledger CLI)[https://ledger-cli.org/]. Each
event is stored on a single line in the following format:

	yyy/mm/dd hh:mmPM [timecode] description

The timecode field is left padded with spaces so that every timecode is the same length in the entire file, but that is
purely to make the fields vertically aligned for easier reading should you ever want to look at the file manually. This
is not needed for the file to parse cleanly.
