---
slug: errors-as-data
title: Errors as Data
publishedAt: 2026-04-08
author: Guardian
flare: Data
summary: A failure is a row, not an apology. We tag every error with structure so the system can answer questions without a human in the loop.
---

We treat errors as data. Every failure carries a tag, a code, and enough structure to be queried. The error is not a string in a log; it is a row with columns we can group and count.

The shape lets the platform answer questions without a person in the room. Which tenants saw a 429 in the last hour. Which routes failed because a dependency was cold. Which background job failed and how many retries it took to clear. The questions are routine. The answers are SQL.

The discipline is upstream of the dashboard. A function that returns a tagged error is a function whose callers can branch on the tag. A function that returns a sentence is a function whose callers can only log it. The first scales; the second accumulates.

We write the tags before we write the dashboards. The dashboards are what falls out.
