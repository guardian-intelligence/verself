---
name: deep-research
description: Perform procedural research on a given topic
---

$TOPIC = $1

Your task is to improve our research documents in order to collect useful non-obvious information about $TOPIC.

Procedure, at a high level:

a. Review existing literature within this repo
b. Crawl the knowledge landscape for $TOPIC, extract novel insights via a fresh eyes perspective, make careful edits to the existing literature within this repo.
c. Git commit, exit.

In more detail:

There may be existing research in docs/research/open-bao. If there is, study the existing corpus first.
                                                                                
Your main focus: collect primary sources about the topic. E.g. case studies, open-source repos using OpenBao, historical and open CVEs, offical docs, manuals and handbooks, and source code for OpenBao itself. Key differences from other libraries solving the same problem from a different angle are helpful as well.

Authoritative econdary sources are helpful as well: blogs from subject matter experts, writing from maintainers and creators, community sentiment and Q&A on forums, and 

Extract from the landscape anything surprising, interesting, or otherwise notewory. Explain via careful additions to the repo with specific terms and citations to specific lines of code or pull quotes. After completing your additions via research, review the totality of the research documents and consolidate repeated information and strike anything that you found to be untrue.

Your output contract is a git commit prefixed [epic:research:self_host_openbao] such that when the diff is viewed in context with the previous commits with the same prefix, it will create a story of deepening technical research into the topic. All changes should be to files within the docs/research/open-bao directory.

Then /loop 1m deep-research $1