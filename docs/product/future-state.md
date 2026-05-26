A template artifact for a self-contained self-replicating software company that can clone itself via an API call to `source-code-hosting-service`, which clones the repo with the user's configured company name, owner details, domain name, email provider credential, Stripe API key, etc. and uses all that to configure the repository for the caller, culminating in a download link. The user can then download their white-labelled clone configured for their providers and execute a shell script to bootstrap a replica of Verself for themselves. IOW: technology that converts any bare metal into structured, useful general purpose compute with economically valuable systems already set up for the user like auth, email, billing/payments, CI, observability, and a fully-functioning end-to-end revenue-generating product.


Design Idea:

Provider Claude Code

User enables "Enable Agent" -> Assigns roles
User hits cmd+K
Types "Revoke recent unknown sessions"
Request hits our service with their prompt
Prompt goes to a cloud agent that acts on behalf of the user, running within our services and data ecosystem.
Our agent makes SDK call(s)
User is presented with option to Approve/Deny each SDK call
CloudTrail shows which agent identity did this, can show the initial prompt as well.

Fun Idea:

World's simplest CLI: make-skill, but completely over-engineered (in a good way: for performance/memory-safety/features) and a ridiculously rigorous release process: SLSA signed OCI artifacts.

Core Pivot:

In order to be powerful, we must integrate as much as possible with the outside world: npm, GitHub, Slack, Twitter. We do not depend on anyone else for our core infrastructure. For everything else, it's compartmentalized non-tier-1 features.