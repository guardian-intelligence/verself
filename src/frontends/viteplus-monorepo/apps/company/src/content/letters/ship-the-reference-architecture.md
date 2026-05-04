---
slug: ship-the-reference-architecture
title: Ship the Reference Architecture
publishedAt: 2026-04-19
author: Guardian
flare: Architecture
summary: Every founder spends the first year on the same dozen systems. We ship them, open-source, per subdirectory, so the second founder never has to.
---

We started Guardian to do two things: run our own company with as few people as possible, and open-source the formula for everyone else.

The first year of any company is spent on the same dozen systems. Identity. Billing. Analytics. Email. Infrastructure. Security. The thousand edges where a real company touches the real world. None of it is what a founder started the company to build. All of it has to be right.

The open-source world is rich in primitives and thin in assemblies. There are a hundred identity providers, a hundred billing systems, a hundred metrics pipelines. There is no single codebase that takes all of them, wires them together the way a real company would, and then operates itself on that codebase.

We build that codebase. The repo is one per subdirectory — platform, mailbox-service, billing-service, iam-service, sandbox-rental-service, vm-orchestrator, and the pieces that hold them together. We dogfood every service on the same substrate our customers use. Letters is the place where we talk about why and how.

The first customer of Guardian is Guardian.
