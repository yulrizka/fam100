---
layout: default
---
# Changelog

## v0.5.24 (2016-08-04)

* Parameterized outbox worker

## v0.5.23 (2016-08-04)

* Post event when starting up & shutting down

## v0.5.22 (2016-07-31)

* update using bot v0.1.1

## v0.5.21 (2016-07-31)

* format join notification

## v0.5.20 (2016-07-31)

* batch join acknowledgement in 5 sec

## v0.5.19 (2016-07-31)

* reduce time-left messages

## v0.5.18 (2016-07-31)

* add retry to important message

## v0.5.17 (2016-07-31)

* lower threshold for unimportant message

## v0.5.16 (2016-07-31)

* remove retry from sending message

## v0.5.15 (2016-07-31)

* discard unimportant message after x sec

## v0.5.14 (2016-07-31)

* remove unused statistics

## v0.5.13 (2016-07-29)

* log error while failed updating last played

## v0.5.12 (2016-07-28)

* log error while failed updating round played

## v0.5.11 (2016-07-27)

* ad ID to game and round
* disable /help command (reduce trafic)

## v0.5.10 (2016-07-25)

* add more metrics and sample query every 1 second

## v0.5.9 (2016-07-25)

* add more metric while handling messages

## v0.5.8 (2016-07-21)

* add database & command handling metrics

## v0.5.7 (2016-07-20)

* add latency metrics

## v0.5.6 (2016-07-20)

* add configurable blocking profiler
* default question limit at 80%

## v0.5.5 (2016-07-20)

* add remote proviling

## v0.5.4 (2016-07-18)

* optimize correct answer message (batch every 10 seconds)
* add metrics to monitor serviceTime

## v0.5.3 (2016-07-17)

* Fix message format when confirming `/join`
* add `/help` command
* update questions

## v0.5.2 (2016-07-08)

* Fix channel ranking bug

## v0.5.1 (2016-07-08)

* limit score to display only 20 for /score and 3 for Final score

## v0.5 (2016-07-08)

* Change format in full score
* Provide link to full score on web

## v0.4.10 (2016-07-04)

* Improve score export tool

## v0.4.9 (2016-07-04)

* Add score export utility
* make broadcast message more efficient
* Fix score did not show properly on certain channel

## v0.4.7, v0.4.8 (2016-06-30)

* Fix bug with regards to active player metrics

## v0.4.6 (2016-06-30)

* Add metrics on active player & message processed

## v0.4.5 (2016-06-29)

* Fix connection leak with DB

## v0.4.4 (2016-06-28)

* Add metrics on logging

## v0.4.3 (2016-06-28)

* Add more metrics on Db and memory usage

## v0.4.2 (2016-06-28)

* Fix connection issue with DB

## v0.4.1 (2016-06-26)

* Fix metrics

## v0.4 (2016-06-18)

* Add global configuration
* Add message of the day (MOTD)

## v0.3.3 (2016-06-18)

* Add internal monitoring

## v0.3.2 (2016-06-16)

* Highlight last correct answer

## v0.3.1 (2016-06-15)

* Fix bug when answer from a channel ends up in another channel

## v0.3 (2016-06-15)

* Internal stability improvement

## v0.2 (2016-06-13)

* Internal Database stability improvement
* play game with cli
* faster answers lookup

## v0.1 (2016-06-12)
* Able to play in telegram.
* Add `/score` command.
* Add game statistics.
* Internal stability improvement.
