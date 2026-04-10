This is the documentation for TigerBeetle: the financial transactions database designed for mission critical safety and performance to power the next 30 years of OLTP.

This is how the entire documentation is organized:

    Start gets you up and running with a cluster.
    Concepts explains why TigerBeetle exists.
    Coding shows how to integrate TigerBeetle into your application.
    Operating covers deployment and operating a TigerBeetle cluster.
    Reference is a companion to Coding which meticulously documents every detail.

Note that this documentation is aimed at the users of TigerBeetle. If you want to understand how it works under the hood, check out the internals docs.
Edit this page
Start

TigerBeetle is a reliable, fast, and highly available database for financial accounting. It tracks financial transactions or anything else that can be expressed as double-entry bookkeeping, providing three orders of magnitude more performance and guaranteeing durability even in the face of network, machine, and storage faults. You will learn more about why this is an important and hard problem to solve in the Concepts section, but first—let’s make some real transactions!
Install

TigerBeetle is a single, small, statically linked binary.

You can download a pre-built binary from tigerbeetle.com:
Linux

curl -Lo tigerbeetle.zip https://linux.tigerbeetle.com && unzip tigerbeetle.zip
./tigerbeetle version

macOS

Windows

See Installing for other options.
Run a Cluster

Typically, TigerBeetle is deployed as a cluster of 6 replicas, which is described in the Operating section. It is also possible to run a single-replica cluster, which of course doesn’t provide high-availability, but is convenient for experimentation; that’s what we’ll do here.

First, format a data file:

./tigerbeetle format --cluster=0 --replica=0 --replica-count=1 --development ./0_0.tigerbeetle

A TigerBeetle replica stores everything in a single file (./0_0.tigerbeetle in this case). The --cluster, --replica, and --replica-count arguments set the topology of the cluster (a single replica for this tutorial).

Now, start a replica:

./tigerbeetle start --addresses=3000 --development ./0_0.tigerbeetle

It will listen on port 3000 for connections from clients. There’s intentionally no way to gracefully shut down a replica. You can ^C it freely, and the data will be safe as long as the underlying storage functions correctly. Note that with a real cluster of 6 replicas, the data is safe even if the storage misbehaves.
Connecting to a Cluster

Now that the cluster is running, we can connect to it using a client. TigerBeetle has clients for several popular programming languages, including Python, Java, Node.js, .Net, and Go, and more are coming; see the Coding section for details. For this tutorial, we’ll keep it simple and connect to the cluster using the built-in CLI client. In a separate terminal, start a REPL with:

./tigerbeetle repl --cluster=0 --addresses=3000

The --addresses argument is the port the server is listening on. The --cluster argument is required to double-check that the client connects to the correct cluster. While not strictly necessary, it helps prevent operator errors.
Issuing Transactions

TigerBeetle comes with a pre-defined database schema — double-entry bookkeeping. The Concept section explains why this particular schema, and the Reference documents all the bells and whistles. For the purposes of this tutorial, it is enough to understand that there are accounts holding credits and debits balances, and that each transfer moves value between two accounts by incrementing credits on one side and debits on the other.

In the REPL, let’s create two empty accounts:

> create_accounts id=1 code=10 ledger=700, id=2 code=10 ledger=700;
> lookup_accounts id=1, id=2;

{
  "id": "1",
  "user_data": "0",
  "ledger": "700",
  "code": "10",
  "flags": [],
  "debits_pending": "0",
  "debits_posted": "0",
  "credits_pending": "0",
  "credits_posted": "0"
}
{
  "id": "2",
  "user_data": "0",
  "ledger": "700",
  "code": "10",
  "flags": "",
  "debits_pending": "0",
  "debits_posted": "0",
  "credits_pending": "0",
  "credits_posted": "0"
}

Now, create our first transfer and inspect the state of accounts afterwards:

> create_transfers id=1 debit_account_id=1 credit_account_id=2 amount=10 ledger=700 code=10;
> lookup_accounts id=1, id=2;

{
  "id": "1",
  "user_data": "0",
  "ledger": "700",
  "code": "10",
  "flags": [],
  "debits_pending": "0",
  "debits_posted": "10",
  "credits_pending": "0",
  "credits_posted": "0"
}
{
  "id": "2",
  "user_data": "0",
  "ledger": "700",
  "code": "10",
  "flags": "",
  "debits_pending": "0",
  "debits_posted": "0",
  "credits_pending": "0",
  "credits_posted": "10"
}

Note how the transfer amount is added to both the credits and debits. That the sum of debits and credits stays equal, no matter what, is a powerful invariant of a double-entry bookkeeping system.
Conclusion

This is the end of the quick start! You now know how to format a data file, run a single-replica TigerBeetle cluster, and run transactions through it. Here’s where to go from here:

    Concepts explains the “why?” of TigerBeetle; read this to decide if TigerBeetle matches the shape of your problem.
    Coding gives guidance on developing applications which store transactions in a TigerBeetle cluster.
    Operating explains how to deploy a TigerBeetle cluster in a highly-available manner, with replication enabled.
    Reference documents every available feature and flag of the underlying data model.

Community

If you want to keep up to speed with recent TigerBeetle developments:

    Monthly Newsletter covers everything of importance that happened with TigerBeetle. It is a changelog director’s cut!
    YouTube channel has most of the talks about TigerBeetle, as well as talks from the Systems Distributed conference. We also stream on Twitch, with recordings duplicated to YouTube.
    𝕏 is good for smaller updates, and word-of-mouth historical trivia you won’t learn elsewhere! Or Bluesky, if that’s your preference.
    GitHub to stay close to the source!

Edit this page
Concepts

This section is for anyone evaluating TigerBeetle, eager to learn about it, or curious. It focuses on the big picture and problems that TigerBeetle solves. As well as why it looks nothing like a typical SQL database from the outside and from the inside.

    OLTP defines the domain of TigerBeetle — system of record for business transactions.
    Debit-Credit argues that double-entry bookkeeping is the right schema for this domain.
    Performance explains how TigerBeetle achieves state-of-the-art performance.
    Safety shows that safety and performance are not at odds with each other.

Edit this page
Online Transaction Processing (OLTP)

Online Transaction Processing (OLTP) is about recording business transactions in real-time. This could be payments, sales, car sharing rides, game scores, or API usage.
The World is Becoming More Transactional

Historically, general purpose databases like PostgreSQL, MySQL, and SQLite handled OLTP. We refer to these as Online General Purpose (OLGP) databases.

OLTP workloads have increased by 3-4 orders of magnitude in the last 10 years alone. For example:

    The UPI real-time payments switch in India processed 10 billion payments in the year 2019. In January 2025 alone, it processed 16.9 billion payments.
    Cleaner energy and smart metering means energy is being traded by the kilowatt-hour. Customer billing is every 15 or 30 minutes rather than at the end of the month.
    Serverless APIs charge for usage by the second or per-request, rather than per month. (Today, serverless billing at scale is often implemented using MapReduce. This makes it difficult or impossible to offer customers real-time spending caps.)

OLGP databases already struggle to keep up.

But TigerBeetle is built to handle the scale of OLTP workloads today and for the decades to come. It works well alongside OLGP databases, which hold infrequently updated data. TigerBeetle can race ahead, giving your system unparalleled latency and throughput.
Write-Heavy Workloads

A distinguishing characteristic of OLTP is its focus on recording business transactions. In contrast, OLGP databases are often designed for read-heavy or balanced workloads.

TigerBeetle is optimized from the ground up for write-heavy workloads. This means it can handle the increasing scale of OLTP, unlike an OLGP database.
High Contention on Hot Accounts

Business transactions always involve more than one account. One account gets paid but then there are fees, taxes, revenue splits, and other costs to account for.

OLTP systems often have accounts involved in a high percentage of all transactions. This is especially true for accounts that represent the business income or expenses. Locks can be used to ensure that updates to these ‘hot accounts’ are consistent. But the resulting contention can bring the system’s performance to a crawl.

TigerBeetle provides strong consistency guarantees without row locks. This sidesteps the issue of contention on hot accounts. Due to TigerBeetle’s use of the system cache, transactions processing speed even increases.
Business Transactions Don’t Shard Well

One of the most common ways to scale systems is to horizontally scale or shard them. This means different servers process different sets of transactions. Unfortunately, business transactions don’t shard well. Horizontal scaling is a poor fit for OLTP:

    Most accounts cannot be neatly partitioned between shards.
    Transactions between accounts on different shards become more complex and slow.
    Row locks on hot accounts worsen when the transactions must execute across shards.

Another approach to scaling OLTP systems is to use MapReduce for billing. But this makes it hard to provide real-time balance reporting or spending limits. It also creates a poor user experience that’s hard to fix post system design.

TigerBeetle uses a single-core design and unique performance optimizations to deliver high throughput. And this without the downsides of horizontal scaling.
Bottleneck for Your System

You can only do as much business as your database supports. You need a core OLTP database capable of handling your transactions on your busiest days. And for decades to come.

TigerBeetle is designed to handle 1 million transactions per second, to remove the risk of your business outgrowing your database.
Next Up: Debit / Credit is the Schema for OLTP

The world is becoming more transactional. OLTP workloads are increasing and we need a database designed from the ground up to handle them. What is the perfect schema and language for this database? Debit / Credit.
Edit this page
Debit/Credit: The Schema for OLTP

As discussed in the previous section, OLTP is all about processing business transactions. We saw that the nuances of OLTP workloads make them tricky to handle at scale.

Now, we’ll turn to the data model and see how the specifics of business transactions actually lend themselves to an incredibly simple schema that’s been in use for centuries.
The “Who, What, When, Where, Why, and How Much” of OLTP

OLTP and business transactions tend to record the same types of information:

    Who: which accounts are transacting?
    What: what type of asset or value is moving?
    When: when was the transaction initiated or when was it finalized?
    Where: where in the world did the transaction take place?
    Why: what type of transaction is this or why is it happening?
    How Much: what quantity of the asset or items was moved?

The Language of Business for Centuries

Debit/Credit, or double-entry bookkeeping, has been the lingua franca of business and accounting since at least the 13th century.

The key insight underpinning Debit/Credit systems is that every transfer records a movement of value from one or more accounts to one or more accounts. Money never appears from nowhere or disappears. This simple principle helps ensure that all of a business’s money is accounted for.

Debit/Credit perfectly captures the who, what, when, where, why, and how much of OLTP while ensuring financial consistency. It is minimal and complete: two entities (accounts, transfers) and one invariant (every debit has an equal and opposite credit) model any exchange of value, in any domain.

(For a deeper dive on debits and credits, see our primer on Financial Accounting.)
SQL vs Debit/Credit

While SQL is a great query language for getting data out of a database, OLTP is primarily about getting data into the database and this is where SQL falls short.

Often, a single business transaction requires multiple SQL queries (on the order of 10 SQL queries per transaction) and potentially even multiple round-trips from the application to the database.

By designing a database specifically for the schema and needs of OLTP, we can ensure our accounting logic is enforced correctly while massively increasing performance.
TigerBeetle Enforces Debit/Credit in the Database

The schema of OLTP is built into TigerBeetle’s data model, and is ready for you to use:

    Who: the debit_account_id and credit_account_id indicate which accounts are transacting.
    What: each asset or type of value in TigerBeetle is tracked on a separate ledger. The ledger field indicates what is being transferred.
    When: each transfer has a unique timestamp for when it is processed by the cluster, but you can add another timestamp representing when the transaction happened in the real world in the user_data_64 field.
    Where: the user_data_32 can be used to store the locale where the transfer occurred.
    Why: the code field stores the reason a transfer occurred and should map to an enum or table of all the possible business events.
    How Much: the amount indicates how much of the asset or item is being transferred.

TigerBeetle also supports two-phase transfers out of the box, and can express complex atomic chains of transfers using linked events. These powerful built-in primitives allow for a large vocabulary of patterns and recipes for data modeling.

Crucially, accounting invariants such as balance limits are enforced within the database, avoiding round-trips between your database and application logic.
Immutability is Essential

Another critical element of Debit/Credit systems is immutability: once transfers are recorded, they cannot be erased. Reversals are implemented with separate transfers to provide a full and auditable log of business events.

Even the strongest durability doesn’t prevent logical data loss. Where SQL allows destructive UPDATE and DELETE, TigerBeetle enforces append-only immutability — ensuring effortless reconciliation and audit success. Transfers in TigerBeetle are always immutable, out of the box. There is no possibility of a malformed query unintentionally deleting data.

Accidentally dropping rows or tables is bad in any database, but it is unacceptable when it comes to accounting. Legal compliance and good business practices require that all funds be fully accounted for, and all history be maintained.
Don’t Roll Your Own Ledger

Many companies start out building their own system for recording business transactions. Then, once their business scales, they realize they need a proper ledger and end up coming back to debits and credits.

A number of prime examples of this are:

    Uber: In 2018, Uber started a 2-year, 40-engineer effort to migrate their collection and disbursement payment platform to one based on the principles of double-entry accounting and debits and credits.1
    Airbnb: From 2012 to 2016, Airbnb used a MySQL-based data pipeline to record all of its transactions in an immutable store suitable for reporting. The pipeline became too complex, hard to scale, and slow. They ended up building a new financial reporting system based on double-entry accounting.2
    Stripe: While we don’t know when this system initially went into service, Stripe relies on an internal system based on double-entry accounting and an immutable log of events to record all of the payments they process.3

    Singla, A., & Wu, S. (2020, October 2). Revolutionizing Money Movements at Scale with Strong Data Consistency. Uber Blog. [https://www.uber.com/blog/money-scale-strong-data](https://www.uber.com/blog/money-scale-strong-data).↩︎

    Liang, A. (2017, March 16). Tracking the Money — Scaling Financial Reporting at Airbnb. The Airbnb Tech Blog. [https://medium.com/airbnb-engineering/tracking-the-money-scaling-financial-reporting-at-airbnb-6d742b80f040](https://medium.com/airbnb-engineering/tracking-the-money-scaling-financial-reporting-at-airbnb-6d742b80f040).↩︎

    Ganelin, I. (2024, February 16). Ledger: Stripe’s system for tracking and validating money movement. Stripe Blog. [https://stripe.com/blog/ledger-stripe-system-for-tracking-and-validating-money-movement](https://stripe.com/blog/ledger-stripe-system-for-tracking-and-validating-money-movement).↩︎

Standardized, Simple, and Scalable

From one perspective, Debit/Credit may seem like a limited data model. However, it is incredibly flexible and scalable. Any business event can be recorded as debits and credits – indeed, accountants have been doing precisely this for centuries!

Instead of modeling business transactions as a set of ad-hoc tables and relationships, debits and credits provide a simple and standardized schema that can be used across all product lines, now and in the future. This avoids the need to add columns, tables, and complex relations between them as new features are added – and avoids complex schema migrations.

Debit/Credit is a universal schema, the foundation of business for hundreds of years, and you can leverage TigerBeetle’s high-performance implementation of it, built for OLTP in the 21st century.
Next: Performance

So far, we’ve seen why we need a new database designed for OLTP and how Debit/Credit provides the perfect data model for it. Next, we look at the performance of a database designed for OLTP.
Edit this page
Performance

How, exactly, is TigerBeetle so fast?
It’s All About The Interface

TigerBeetle is designed specifically for OLTP workloads.

The prevailing paradigm for OLGP is interactive transactions, where business-logic lives in the application, and the job of the database is to send the data to the application, holding the locks while the data is being processed. This works for mixed read-write workload with low contention, but fails for highly-contended OLTP workloads — locks over the network are very expensive!

With TigerBeetle, all the logic lives inside the database, obviating the need for locking. Not only is this very fast, it is also more convenient — the application can speak Debit/Credit directly, it doesn’t need to translate the language of business to SQL. This is the power of an interface for performance!
Batching, Batching, Batching

On a busy day in a busy city, taking the subway is faster than using a car. On empty streets, a personal sports car gives you the best latency, but when the load and contention increase, due to Little’s law, both latency and throughput become abysmal.

TigerBeetle works like a high-speed train — its interface always deals with batches of transfers, up to 8,190 transfers per query. Although TigerBeetle is a replicated database using a consensus algorithm, the cost of replication is paid only once per batch, which means that TigerBeetle runs almost as fast as an in-memory hash map, all the while providing extreme durability and availability.

What’s more, under light load, the batches automatically become smaller, trading unnecessary throughput for better latency.
Extreme Engineering

Debit/Credit fixes inefficiency in the interface, pervasive batching amortizes costs, but, to really hit performance targets, solid engineering is required at every level of the stack:

    TigerBeetle is built fully from scratch, without using any dependencies, to make sure that all the layers are co-designed for OLTP.
    TigerBeetle is written in Zig, a systems programming language which doesn’t use garbage collection and is designed for writing fast code.
    Every data structure is hand-crafted with the CPU in mind: a transfer object is 128 bytes in size, cache-line aligned. Executing a batch of transfers is just one tight CPU loop!
    TigerBeetle allocates all the memory statically: it never runs out of memory, it never stalls due to a GC pause or mutex contention, and it never fragments the memory.
    TigerBeetle is designed for io_uring, a Linux kernel interface for zero syscall networking and storage I/O.

These and other performance rules are captured in TigerStyle — the secret recipe that keeps TigerBeetle fast and safe.
Single Threaded By Design

TigerBeetle uses a single core by design and uses a single leader node to process events. Adding more nodes can therefore increase reliability, but not throughput.

For a high-performance database, this may seem like an unusual choice. However, sharding in financial databases is notoriously difficult, and contention issues often negate the would-be benefits. Specifically, a small number of hot accounts are often involved in a large proportion of the transactions, so the shards responsible for those accounts become bottlenecks.

For more details on when single-threaded implementations of algorithms outperform multi-threaded implementations, see “Scalability! But at what COST?.
Performance = Flexibility

Is it really necessary to go to such great lengths in the name of performance?

It depends on the use-case (worth keeping in mind is that higher performance can unlock new use-cases). An OLGP database might be enough for nightly settlement; for real-time settlement, OLTP is a no-brainer.

If a transaction system just hits its throughput target, every unexpected delay or ops accident will lead to missed transactions. If a system operates at one tenth of capacity, there is headroom for the unexpected.

Last but not least, it is prudent to think about the future. The future is hard to predict (even the present is hard to wrap one’s head around!); the option to handle significantly more load on short notice greatly expands optionality and sleep quality.
Next: Safety

Performance can get you very far very fast, but it is useless if the result is wrong. Business transaction processing also requires strong safety guarantees, to ensure that data cannot be lost, and high availability to ensure that money is not lost due to database downtime.

Next, how TigerBeetle ensures safety.
Edit this page
Safety

The purpose of a database is to store data: if the database accepts new data, it should be able to retrieve it later. Surprisingly, many databases don’t provide guaranteed durability – usually the data is there, but, under certain edge case conditions, it can get lost!

As the purpose of TigerBeetle is to be the system of record for business transaction, associated with real-world value transfers, it is paramount that the data stored in TigerBeetle is safe.

TigerBeetle is therefore designed, engineered, and tested to deliver unbreakable durability – even under the most extreme failure scenarios.
Strict Serializability

The easiest way to lose data is by incorrectly using the database, by misconfiguring (or just misunderstanding) its isolation level. For this reason, TigerBeetle intentionally supports only the strictest possible isolation level – strict serializability. All transfers are executed one-by-one, on a single core.

Furthermore, TigerBeetle’s state machine is designed according to the end-to-end idempotency principle – each transfer has a unique client-generated u128 id, and each transfer is processed at most once, even in the presence of intermediate retry loops.
High Availability

Some databases rely on a single central server, which puts the data at risk as any single server might fail catastrophically (e.g. due to a fire in the data center). Primary/backup systems with ad-hoc failover can lose data due to split-brain.

To avoid these pitfalls, TigerBeetle implements pioneering Viewstamped Replication and consensus algorithm, that guarantees correct, automatic failover. It’s worth emphasizing that consensus proper needs only be engaged during actual failover. During the normal operation, the cost of consensus is just the cost of replication, which is further minimized because of batching, tail latency tolerance, and pipelining.

TigerBeetle does not depend on synchronized system clocks, does not use leader leases, and performs leader-based timestamping so that your application can deal only with safe relative quantities of time with respect to transfer timeouts. To ensure that the leader’s clock is within safe bounds of “true time”, TigerBeetle combines all the clocks in the cluster to create a fault-tolerant clock that we call “cluster time”.

For the highest availability, TigerBeetle should be deployed as a cluster of six replicas across three different cloud providers (two replicas per provider). Because TigerBeetle uses Heidi Howard’s flexible quorums, this deployment is guaranteed to tolerate a complete outage of any cloud provider and will likely survive even if one extra replica fails. Multi-cloud eliminates lock-in, meets regulatory requirements, and protects availability – even through provider slowdowns and disruptions.

TigerBeetle detects and overcomes Gray Failure automatically. If a replica’s disk becomes slow or the network interface starts dropping packets, TigerBeetle automatically adjusts replication topology to ensure that the slow replica doesn’t affect user-visible latencies, while still guaranteeing cluster-wide durability.
Storage Fault Tolerance

Traditionally, databases assume that disks do not fail, or at least fail politely with a clear error code. This is usually a reasonable assumption, but edge cases matter.

HDD and SSD hardware can fail. Disks can silently return corrupt data ( 0.031% of SSD disks per year, 1.4% of Enterprise HDD disks per year), misdirect IO ( 0.023% of SSD disks per year, 0.466% of Nearline HDD disks per year), or just suddenly become extremely slow, without returning an error code (the so called gray failure).

On top of hardware, software might be buggy or just tricky to use correctly. Handling fsync failures correctly is particularly hard.

TigerBeetle assumes that its disk will fail and takes advantage of replication to proactively repair replica’s local disks:

    All data in TigerBeetle is immutable, checksummed, and hash-chained, providing a strong guarantee that no corruption or tampering happened. In case of a latent sector error, the error is detected and repaired without any operator involvement.
    Most consensus implementations lose data or become unavailable if the write-ahead log gets corrupted. TigerBeetle uses Protocol Aware Recovery to remain available unless the data gets corrupted on every single replica.
    To minimize the impact of software bugs, TigerBeetle puts as little software as possible between itself and the disk – TigerBeetle manages its own page cache, writes data to disk with O_DIRECT and can work with a block device directly, no file system is necessary.
    TigerBeetle also tolerates Gray Failure – if a disk on a replica becomes very slow, the cluster falls back on other replicas for durability.

Software Reliability

Even the advanced algorithm with a formally proved correctness theorem is useless if the implementation is buggy. TigerBeetle uses the oldest and the newest software engineering practices to ensure correctness.

TigerBeetle is written in Zig – a modern systems programming language that removes many instances of undefined behavior, provides spatial memory safety and encourages simple, straightforward code.

TigerBeetle adheres to a strict code style, TigerStyle, inspired by NASA’s power of ten. For example, TigerBeetle uses static memory allocation, which designs away memory fragmentation, out-of-memory errors and use-after-frees.

TigerBeetle is tested in the VOPR – a simulated environment where an entire cluster, running real code, is subjected to all kinds of network, storage and process faults, at 1000x speed. This simulation can find both logical errors in the algorithms and coding bugs in the source. This simulator is running 24/7 on 1024 cores, fuzzing the latest version of the database. You can also play it as a game.
Human Fallibility

While, with a lot of care, software can be perfected to become virtually bug-free, humans will always make mistakes. TigerBeetle takes this into account and tries to protect from operator errors:

    The surface area is intentionally minimized, with little configurability.
    In particular, there’s only one isolation level – strict serializability.
    Upgrades are automatic and atomic, guaranteeing that each transfer is applied by only a single version of code.
    TigerBeetle always runs with online verification on, to detect any discrepancies in the data.

Is TigerBeetle ACID-compliant?

Yes. Let’s discuss each part:
Atomicity

As part of replication, each operation is durably stored in at least a quorum of replicas’ Write-Ahead Logs (WAL) before the primary will acknowledge the operation as committed. WAL entries are executed through the state machine business logic and the resulting state changes are stored in TigerBeetle’s LSM-Forest local storage engine.

The WAL is what allows TigerBeetle to achieve atomicity and durability since the WAL is the source of truth. If TigerBeetle crashes, the WAL is replayed at startup from the last checkpoint on disk.

However, financial atomicity goes further than this: events and transfers can be linked when created so they all succeed or fail together.
Consistency

TigerBeetle guarantees strict serializability. And at the cluster level, stale reads are not possible since all operations (not only writes, but also reads) go through the global consensus protocol.

However, financial consistency requires more than this. TigerBeetle exposes a double-entry accounting API to guarantee that money cannot be created or destroyed, but only transferred from one account to another. And transfer history is immutable.
Isolation

All client requests (and all events within a client request batch) are executed with the highest level of isolation, serially through the state machine, one after another, before the next operation begins. Counterintuitively, the use of batching and serial execution means that TigerBeetle can also provide this level of isolation optimally, without the cost of locks for all the individual events within a batch.
Durability

Without Durability, the guarantees of Atomicity, Consistency, and Isolation collapse – the only letter in ACID whose loss undoes the others.

Up until 2018, traditional DBMS durability has focused on the Crash Consistency Model, however, Fsyncgate and Protocol Aware Recovery have shown that this model can lead to real data loss for users in the wild. TigerBeetle therefore adopts an explicit storage fault model, which we then verify and test with incredible levels of corruption, something which few distributed systems historically were designed to handle. Our emphasis on protecting Durability sets TigerBeetle apart.

While absolute durability is impossible – all hardware can ultimately fail; data we write today might not be available tomorrow – TigerBeetle embraces limited disk reliability and maximizes data durability in spite of imperfect disks. We actively work against such entropy by taking advantage of cluster-wide storage. A record would need to get corrupted on all replicas in a cluster to get lost, and even in that case the system would safely halt.
Security

As a financial system of record, TigerBeetle is a trusted component and must be running in a trusted environment. While TigerBeetle is extensively fuzzed, deals only with fixed-sized integer data structures, has no (de)serialization and doesn’t take user-generated strings, TigerBeetle doesn’t provide any permission system. The application must implement its own access controls.

Note on io_uring: it is a relatively recent (Linux 5.1, 2019) part of the Linux kernel. It had some kernel exploits, which made it problematic for sandboxed applications, and lead to io_uring being disabled for systems which deal with untrusted data. Because TigerBeetle, by design, only deals with trusted integer data, its usage of io_uring is secure, and is the safest and most performant way to handle asynchronous disk I/O.
Next: Coding

This concludes the discussion of the concepts behind TigerBeetle — an OLTP database for recording business transactions in real time, using a double-entry bookkeeping schema, which is orders of magnitude faster and keeps the data safe even when the underlying hardware inevitably fails.

We will now learn how to build applications on top of TigerBeetle.
Edit this page
Coding

This section is aimed at programmers building applications on top of TigerBeetle. It is organized as a series of loosely connected guides which can be read in any order.

    System Architecture paints the big picture.
    Data Modeling shows how to map business-level entities to the primitives provided by TigerBeetle.
    Financial Accounting, a deep dive into double-entry bookkeeping.
    Requests outlines the database interface.
    Reliable Transaction Submission explains the end-to-end principle and how it helps to avoid double spending.
    Two-Phase Transfers introduces pending transfers, one of the most powerful primitives built into TigerBeetle.
    Linked Events shows how several transfers can be chained together into a larger transaction, which succeeds or fails atomically.
    Time lists the guarantees provided by the TigerBeetle cluster clock.
    Recipes is a library of ready-made solutions for common business requirements such as a currency exchange.
    Clients shows how to use TigerBeetle from the comfort of .NET, Go, Java, Node.js, or Python.

Subscribe to the tracking issue #2231 to receive notifications about breaking changes!
Edit this page
TigerBeetle in Your System Architecture

TigerBeetle is an Online Transaction Processing (OLTP) database built for safety and performance. It is not a general purpose database like PostgreSQL or MySQL. Instead, TigerBeetle works alongside your general purpose database, which we refer to as an Online General Purpose (OLGP) database.

TigerBeetle should be used in the data plane, or hot path of transaction processing, while your general purpose database is used in the control plane and may be used for storing information or metadata that is updated less frequently.

TigerBeetle in Your System Architecture
Division of Responsibilities

App or Website

    Initiate transactions
    Generate Transfer and Account IDs

Stateless API Service

    Handle authentication and authorization
    Create account records in both the general purpose database and TigerBeetle when users sign up
    Cache ledger metadata
    Batch transfers
    Apply exchange rates for currency exchange transactions

General Purpose (OLGP) Database

    Store metadata about ledgers and accounts (such as string names or descriptions)
    Store mappings between integer type identifiers used in TigerBeetle and string representations used by the app and API

TigerBeetle (OLTP) Database

    Record transfers between accounts
    Track balances for accounts
    Enforce balance limits
    Enforce financial consistency through double-entry bookkeeping
    Enforce strict serializability of events
    Optionally store pointers to records or entities in the general purpose database in the user_data fields

Ledger, Account, and Transfer Types

For performance reasons, TigerBeetle stores the ledger, account, and transfer types as simple integers. Most likely, you will want these integers to map to enums of type names or strings, along with other associated metadata.

The mapping from the string representation of these types to the integers used within TigerBeetle may be hard-coded into your application logic or stored in a general purpose (OLGP) database and cached by your application. (These mappings should be immutable and append-only, so there is no concern about cache invalidation.)

⚠️ Importantly, initiating a transfer should not require fetching metadata from the general purpose database. If it does, that database will become the bottleneck and will negate the performance gains from using TigerBeetle.

Specifically, the types of information that fit into this category include:
Hard-coded in app or cached 	In TigerBeetle
Currency or asset code’s string representation (for example, “USD”) 	ledger and asset scale
Account type’s string representation (for example, “cash”) 	code
Transfer type’s string representation (for example, “refund”) 	code
Authentication

TigerBeetle does not support authentication. You should never allow untrusted users or services to interact with it directly.

Also, untrusted processes must not be able to access or modify TigerBeetle’s on-disk data file.
Edit this page
Data Modeling

This section describes various aspects of the TigerBeetle data model and provides some suggestions for how you can map your application’s requirements onto the data model.
Accounts, Transfers, and Ledgers

The TigerBeetle data model consists of Accounts, Transfers, and ledgers.
Ledgers

Ledgers partition accounts into groups that may represent a currency or asset type or any other logical grouping. Only accounts on the same ledger can transact directly, but you can use atomically linked transfers to implement currency exchange.

Ledgers are only stored in TigerBeetle as a numeric identifier on the account and transfer data structures. You may want to store additional metadata about each ledger in a control plane database.

You can also use different ledgers to further partition accounts, beyond asset type. For example, if you have a multi-tenant setup where you are tracking balances for your customers’ end-users, you might have a ledger for each of your customers. If customers have end-user accounts in multiple currencies, each of your customers would have multiple ledgers.
Debits vs Credits

TigerBeetle tracks each account’s cumulative posted debits and cumulative posted credits. In double-entry accounting, an account balance is the difference between the two – computed as either debits - credits or credits - debits, depending on the type of account. It is up to the application to compute the balance from the cumulative debits/credits.

From the database’s perspective the distinction is arbitrary, but accounting conventions recommend using a certain balance type for certain types of accounts.

If you are new to thinking in terms of debits and credits, read the deep dive on financial accounting to get a better understanding of double-entry bookkeeping and the different types of accounts.
Debit Balances

balance = debits - credits

By convention, debit balances are used to represent:

    Operator’s Assets
    Operator’s Expenses

To enforce a positive (non-negative) debit balance, use flags.credits_must_not_exceed_debits.

To keep an account’s balance between an upper and lower bound, see the Balance Bounds recipe.
Credit Balances

balance = credits - debits

By convention, credit balances are used to represent:

    Operator’s Liabilities
    Equity in the Operator’s Business
    Operator’s Income

To enforce a positive (non-negative) credit balance, use flags.debits_must_not_exceed_credits. For example, a customer account that is represented as an Operator’s Liability would use this flag to ensure that the balance cannot go negative.

To keep an account’s balance between an upper and lower bound, see the Balance Bounds recipe.
Compound Transfers

Transfers in TigerBeetle debit a single account and credit a single account. You can read more about implementing compound transfers in Multi-Debit, Multi-Credit Transfers.
Fractional Amounts and Asset Scale

To maximize precision and efficiency, Account debits/credits and Transfer amounts are unsigned 128-bit integers. However, currencies are often denominated in fractional amounts.

To represent a fractional amount in TigerBeetle, map the smallest useful unit of the fractional currency to 1. Consider all amounts in TigerBeetle as a multiple of that unit.

Applications may rescale the integer amounts as necessary when rendering or interfacing with other systems. But when working with fractional amounts, calculations should be performed on the integers to avoid loss of precision due to floating-point approximations.
Asset Scale

When the multiplier is a power of 10 (e.g. 10 ^ n), then the exponent n is referred to as an asset scale. For example, representing USD in cents uses an asset scale of 2.
Examples

    1 USD = 100 cents. Using an asset scale of 2,
        The fractional amount 0.45 USD is represented as the integer 45.
        The fractional amount 123.00 USD is represented as the integer 12300.
        The fractional amount 123.45 USD is represented as the integer 12345.

    1 JPY = 1 yen. Using an asset scale of 0,
        The fractional amount 123 JPY is represented as the integer 123.

    1 KWD = 1000 fils. Using an asset scale of 3,
        The fractional amount 0.450 KWD is represented as the integer 450.
        The fractional amount 123.000 KWD is represented as the integer 123000.
        The fractional amount 123.450 KWD is represented as the integer 123450.

The other direction works as well. If the smallest useful unit of an asset is 10, 000, 000 units, then it can be scaled down to the integer 1 using an asset scale of -7.
⚠️ Asset Scales Cannot Be Easily Changed

When setting your asset scales, we recommend thinking about whether your application may ever require a larger asset scale. If so, we would recommend using that larger scale from the start.

For example, it might seem natural to use an asset scale of 2 for many currencies. However, it may be wise to use a higher scale in case you ever need to represent smaller fractions of that asset.

Accounts and transfers are immutable once created. In order to change the asset scale of a ledger, you would need to use a different ledger number and duplicate all the accounts on that ledger over to the new one.
user_data

user_data_128, user_data_64 and user_data_32 are the most flexible fields in the schema (for both accounts and transfers). Each user_data field’s contents are arbitrary, interpreted only by the application.

Each user_data field is indexed for efficient point and range queries.

While the usage of each field is entirely up to you, one way of thinking about each of the fields is:

    user_data_128 - this might store the “who” and/or “what” of a transfer. For example, it could be a pointer to a business entity stored within the control plane database.
    user_data_64 - this might store a second timestamp for “when” the transaction originated in the real world, rather than when the transfer was timestamped by TigerBeetle. This can be used if you need to model bitemporality. Alternatively, if you do not need this to be used for a timestamp, you could use this field in place of the user_data_128 to store the “who”/“what”.
    user_data_32 - this might store the “where” of a transfer. For example, it could store the jurisdiction where the transaction originated in the real world. In certain cases, such as for cross-border remittances, it might not be enough to have the UTC timestamp and you may want to know the transfer’s locale.

(Note that the code can be used to encode the “why” of a transfer.)

Any of the user_data fields can be used as a group identifier for objects that will be queried together. For example, for multiple transfers used for currency exchange.
id

The id field uniquely identifies each Account and Transfer within the cluster.

The primary purpose of an id is to serve as an “idempotency key” — to avoid executing an event twice. For example, if a client creates a transfer but the server’s reply is lost, the client (or application) will retry — the database must not transfer the money twice.

Note that ids are unique per cluster – not per ledger. You should attach a separate identifier in the user_data field if you want to store a connection between multiple Accounts or multiple Transfers that are related to one another. For example, different currency Accounts belonging to the same user or multiple Transfers that are part of a currency exchange.

TigerBeetle Time-Based Identifiers are recommended for most applications.

When selecting an id scheme:

    Idempotency is particularly important (and difficult) in the context of application crash recovery.
    Be careful to avoid id collisions.
    An account and a transfer may share the same id (they belong to different “namespaces”), but this is not recommended because other systems (that you may later connect to TigerBeetle) may use a single “namespace” for all objects.
    Avoid requiring a central oracle to generate each unique id (e.g. an auto-increment field in SQL). A central oracle may become a performance bottleneck when creating accounts/transfers.
    Sequences of identifiers with long runs of strictly increasing (or strictly decreasing) values are amenable to optimization, leading to higher database throughput.
    Random identifiers are not recommended – they can’t take advantage of all of the LSM optimizations. (Random identifiers have significantly lower throughput than strictly-increasing ULIDs).

TigerBeetle Time-Based Identifiers (Recommended)

TigerBeetle recommends using a specific ID scheme for most applications. It is time-based and lexicographically sortable. The scheme is inspired by ULIDs and UUIDv7s but is better able to take advantage of LSM optimizations, which leads to higher database throughput.

TigerBeetle clients include an id() function to generate IDs using the recommended scheme.

TigerBeetle ID is a 128-bit number where:

    the high 48 bits are a millisecond timestamp
    the low 80 bits are random.

id = (timestamp << 80) | random

When creating multiple objects during the same millisecond, we increment the random bytes rather than generating new random bytes. These details ensure that a sequence of objects have strictly increasing IDs according to the server, which improves database optimization.

Similar to ULIDs and UUIDv7s, these IDs have the following benefits:

    they have an insignificant risk of collision.
    they do not require a central oracle to generate.

Reuse Foreign Identifier

This technique is most appropriate when integrating TigerBeetle with an existing application where TigerBeetle accounts or transfers map one-to-one with an entity in the foreign database.

Set id to a “foreign key” – that is, reuse an identifier of a corresponding object from another database. For example, if every user (within the application’s database) has a single account, then the identifier within the foreign database can be used as the Account.id within TigerBeetle.

To reuse the foreign identifier, it must conform to TigerBeetle’s id constraints.
code

The code identifier represents the “why” for an Account or Transfer.

On an Account, the code indicates the account type, such as assets, liabilities, equity, income, or expenses, and subcategories within those classification.

On a Transfer, the code indicates why a given transfer is happening, such as a purchase, refund, currency exchange, etc.

When you start building out your application on top of TigerBeetle, you may find it helpful to list out all of the known types of accounts and movements of funds and mapping each of these to code numbers or ranges.
Edit this page
Financial Accounting

For developers with non-financial backgrounds, TigerBeetle’s use of accounting concepts like debits and credits may be one of the trickier parts to understand. However, these concepts have been the language of business for hundreds of years, and it will be worth it!

This page goes a bit deeper into debits and credits, double-entry bookkeeping, and how to think about your accounts as part of a type system.
Building Intuition with Two Simple Examples

If you have an outstanding loan and owe a bank 100, is your balance 100 or -100? Conversely, if you have 200 in your bank account, is the balance 200 or -200?

Thinking about these two examples, we can start to build an intuition that the positive or negative sign of the balance depends on whose perspective we’re looking from. That 100 you owe the bank represents a “bad” thing for you, but a “good” thing for the bank. We might think about that same debt differently if we’re doing your accounting or the bank’s.

These examples also hint at the different types of accounts. We probably want to think about a debt as having the opposite “sign” as the funds in your bank account. At the same time, the types of these accounts look different depending on whether you are considering them from the perspective of you or the bank.

Now, back to our original questions: is the loan balance 100 or -100 and is the bank account balance 200 or -200? On some level, this feels a bit arbitrary, because it is. Fortunately, there are some commonly agreed-upon standards! This is exactly what debits and credits and the financial accounting type system provide.
Types of Accounts

In financial accounting, there are 5 main types of accounts:

    Asset - what you own, which could produce income or which you could sell.
    Liability - what you owe to other people.
    Equity - value of the business owned by the owners or shareholders, or “the residual interest in the assets of the entity after deducting all its liabilities.”1
    Income - money or things of value you receive for selling products or services, or “increases in assets, or decreases in liabilities, that result in increases in equity, other than those relating to contributions from holders of equity claims.”2
    Expense - money you spend to pay for products or services, or “decreases in assets, or increases in liabilities, that result in decreases in equity, other than those relating to distributions to holders of equity claims.”3

As mentioned above, the type of account depends on whose perspective you are doing the accounting from. In those examples, the loan you have from the bank is liability for you, because you owe the amount to the bank. However, that same loan is an asset from the bank’s perspective. In contrast, the money in your bank account is an asset for you but it is a liability for the bank.

Each of these major categories are further subdivided into more specific types of accounts. For example, in your personal accounting you would separately track the cash in your physical wallet from the funds in your checking account, even though both of those are assets. The bank would split out mortgages from car loans, even though both of those are also assets for the bank.

    IFRS. Conceptual Framework for Financial Reporting. IFRS Foundation, 2018. https://www.ifrs.org/content/dam/ifrs/publications/pdf-standards/english/2021/issued/part-a/conceptual-framework-for-financial-reporting.pdf↩︎

    IFRS. Conceptual Framework for Financial Reporting. IFRS Foundation, 2018. https://www.ifrs.org/content/dam/ifrs/publications/pdf-standards/english/2021/issued/part-a/conceptual-framework-for-financial-reporting.pdf↩︎

    IFRS. Conceptual Framework for Financial Reporting. IFRS Foundation, 2018. https://www.ifrs.org/content/dam/ifrs/publications/pdf-standards/english/2021/issued/part-a/conceptual-framework-for-financial-reporting.pdf↩︎

Double-Entry Bookkeeping

Categorizing accounts into different types is useful for organizational purposes, but it also provides a key error-correcting mechanism.

Every record in our accounting is not only recorded in one place, but in two. This is double-entry bookkeeping. Why would we do that?

Let’s think about the bank loan in our example above. When you took out the loan, two things actually happened at the same time. On the one hand, you now owe the bank 100. At the same time, the bank gave you 100. These are the two entries that comprise the loan transaction.

From your perspective, your liability to the bank increased by 100 while your assets also increased by 100. From the bank’s perspective, their assets (the loan to you) increased by 100 while their liabilities (the money in your bank account) also increased by 100.

Double-entry bookkeeping ensures that funds are always accounted for. Money never just appears. Funds always go from somewhere to somewhere.
Keeping Accounts in Balance

Now we understand that there are different types of accounts and every transaction will be recorded in two (or more) accounts – but which accounts?

The Fundamental Accounting Equation stipulates that:

Assets - Liabilities = Equity

Using our loan example, it’s no accident that the loan increases assets and liabilities at the same time. Assets and liabilities are on the opposite sides of the equation, and both sides must be exactly equal. Loans increase assets and liabilities equally.

Here are some other types of transactions that would affect assets, liabilities, and equity, while maintaining this balance:

    If you withdraw 100 in cash from your bank account, your total assets stay the same. Your bank account balance (an asset) would decrease while your physical cash (another asset) would increase.
    From the perspective of the bank, you withdrawing 100 in cash decreases their assets in the form of the cash they give you, while also decreasing their liabilities because your bank balance decreases as well.
    If a shareholder invests 1000 in the bank, that increases both the bank’s assets and equity.

Assets, liabilities, and equity represent a point in time. The other two main categories, income and expenses, represent flows of money in and out.

Income and expenses impact the position of the business over time. The expanded accounting equation can be written as:

Assets - Liabilities = Equity + Income − Expenses

You don’t need to memorize these equations (unless you’re training as an accountant!). However, it is useful to understand that those main account types lie on different sides of this equation.
Debits and Credits vs Signed Integers

Instead of using a positive or negative integer to track a balance, TigerBeetle and double-entry bookkeeping systems use debits and credits.

The two entries that give “double-entry bookkeeping” its name are the debit and the credit: every transaction has at least one debit and at least one credit. (Note that for efficiency’s sake, TigerBeetle Transfers consist of exactly one debit and one credit. These can be composed into more complex multi-debit, multi-credit transfers.) Which entry is the debit and which is the credit? The answer is easy once you understand that accounting is a type system. An account increases with a debit or credit according to its type.

When our example loan increases the assets and liabilities, we need to assign each of these entries to either be a debit or a credit. At some level, this is completely arbitrary. For clarity, accountants have used the same standards for hundreds of years:
How Debits and Credits Increase or Decrease Account Balances

    Assets and expenses are increased with debits, decreased with credits
    Liabilities, equity, and income are increased with credits, decreased with debits

Or, in a table form:
	Debit 	Credit
Asset 	+ 	-
Liability 	- 	+
Equity 	- 	+
Income 	- 	+
Expense 	+ 	-

From the perspective of our example bank:

    You taking out a loan debits (increases) their loan assets and credits (increases) their bank account balance liabilities.
    You paying off the loan debits (decreases) their bank account balance liabilities and credits (decreases) their loan assets.
    You depositing cash debits (increases) their cash assets and credits (increases) their bank account balance liabilities.
    You withdrawing cash debits (decreases) their bank account balance liabilities and credits (decreases) their cash assets.

Note that accounting conventions also always write the debits first, to represent that something is received (debit) before it is given up (credit). This is also consistent with the visual representation of T-Accounts, with a “debit” column on the left and a “credit” column on the right.

If this seems arbitrary and confusing, we understand! It’s a convention, just like how most programmers need to learn zero-based array indexing and then at some point it becomes second nature.
Account Types and the “Normal Balance”

Some other accounting systems have the concept of a “normal balance”, which would indicate whether a given account’s balance is increased by debits or credits.

When designing for TigerBeetle, we recommend thinking about account types instead of “normal balances”. This is because the type of balance follows from the type of account, but the type of balance doesn’t tell you the type of account. For example, an account might have a normal balance on the debit side but that doesn’t tell you whether it is an asset or expense.
Takeaways

    Accounts are categorized into types. The 5 main types are asset, liability, equity, income, and expense.
    Depending on the type of account, an increase is recorded as either a debit or a credit.
    All transfers consist of two entries, a debit and a credit. Double-entry bookkeeping ensures that all funds come from somewhere and go somewhere.

When you get started using TigerBeetle, we would recommend writing a list of all the types of accounts in your system that you can think of. Then, think about whether, from the perspective of your business, each account represents an asset, liability, equity, income, or expense. That determines whether the given type of account is increased with a debit or a credit.
Want More Help Understanding Debits and Credits?

The TigerBeetle team can support you to design your chart of accounts, and leverage the power of fully managed TigerBeetle in your architecture. Contact us at sales@tigerbeetle.com to set up a call. There is also a Startup Program for early-stage businesses.
Edit this page
Requests

A request queries or updates the database state.

A request consists of one or more events of the same type sent to the cluster in a single message. For example, a single request can create multiple transfers but it cannot create both accounts and transfers.

The cluster commits an entire request at once. Events are applied in series, such that successive events observe the effects of previous ones and event timestamps are totally ordered.

Each request receives one reply message from the cluster. The reply contains one result for each event in the request.
Request Types

    create_accounts: create Accounts
    create_transfers: create Transfers
    lookup_accounts: fetch Accounts by id
    lookup_transfers: fetch Transfers by id
    get_account_transfers: fetch Transfers by debit_account_id or credit_account_id
    get_account_balances: fetch the historical account balance by the Account’s id.
    query_accounts: query Accounts
    query_transfers: query Transfers

Events and Results

Each request has a corresponding event and result type:
Request Type 	Event 	Result
create_accounts 	Account 	CreateAccountResult
create_transfers 	Transfer 	CreateTransferResult
lookup_accounts 	Account.id 	Account or nothing
lookup_transfers 	Transfer.id 	Transfer or nothing
get_account_transfers 	AccountFilter 	Transfer or nothing
get_account_balances 	AccountFilter 	AccountBalance or nothing
query_accounts 	QueryFilter 	Account or nothing
query_transfers 	QueryFilter 	Transfer or nothing
Idempotency

Events that create objects are idempotent. The first event to create an object with a given id will receive the ok result. Subsequent events that attempt to create the same object will receive the exists result.
Batching Events

To achieve high throughput, TigerBeetle amortizes the overhead of consensus and I/O by batching many events in each request.

In the default configuration, the maximum batch sizes for each request type are:
Request Type 	Request Batch Size (Events) 	Reply Batch Size (Results)
lookup_accounts 	8189 	8189
lookup_transfers 	8189 	8189
create_accounts 	8189 	8189
create_transfers 	8189 	8189
get_account_transfers 	1† 	8189
get_account_balances 	1† 	8189
query_accounts 	1† 	8189
query_transfers 	1† 	8189

    Node.js
    Go
    Java
    .NET
    Python

Automatic Batching

TigerBeetle clients automatically batch operations. There may be instances where your application logic makes it hard to fill up the batches that you send to TigerBeetle, for example a multi-threaded web server where each HTTP request is handled on a different thread.

The TigerBeetle client should be shared across threads (or tasks, depending on your paradigm), since it automatically groups together batches of small sizes into one request. Since TigerBeetle clients can have at most one in-flight request, the client accumulates smaller batches together while waiting for a reply to the last request.

†: For queries (e.g. get_account_transfers, etc) TigerBeetle clients use the query limit to automatically batch queries of the same type together into requests when it knows for sure that all of their results will fit in a single reply.
Guarantees

    A request executes within the cluster at most once.
    Requests do not time out. Clients will continuously retry requests until they receive a reply from the cluster. This is because in the case of a network partition, a lack of response from the cluster could either indicate that the request was dropped before it was processed or that the reply was dropped after the request was processed. Note that individual pending transfers within a request may have timeouts.
    Requests retried by their original client session receive identical replies.
    Requests retried by a different client (same request body, different session) may receive different replies.
    Events within a request are executed in sequence. The effects of a given event are observable when the next event within that request is applied.
    Events within a request do not interleave with events from other requests.
    All events within a request batch are committed, or none are. Note that this does not mean that all of the events in a batch will succeed, or that all will fail. Events succeed or fail independently unless they are explicitly linked.
    Once committed, an event will always be committed – the cluster’s state never backtracks.
    Within a cluster, object timestamps are unique and strictly increasing. No two objects within the same cluster will have the same timestamp. Furthermore, the order of the timestamps indicates the order in which the objects were committed.

Edit this page
Reliable Transaction Submission

When making payments or recording transfers, it is important to ensure that they are recorded once and only once – even if some parts of the system fail during the transaction.

There are some subtle gotchas to avoid, so this page describes how to submit events – and especially transfers – reliably.
The App or Browser Should Generate the ID

Transfers and Accounts carry an id field that is used as an idempotency key to ensure the same object is not created twice.

The client software, such as your app or web page, that the user interacts with should generate the id (not your API). This id should be persisted locally before submission, and the same id should be used for subsequent retries.

    User initiates a transfer.
    Client software (app, web page, etc) generates the transfer id.
    Client software persists the id in the app or browser local storage.
    Client software submits the transfer to your API service.
    API service includes the transfer in a request.
    TigerBeetle creates the transfer with the given id once and only once.
    TigerBeetle responds to the API service.
    The API service responds to the client software.

Handling Network Failures

The method described above handles various potential network failures. The request may be lost before it reaches the API service or before it reaches TigerBeetle. Or, the response may be lost on the way back from TigerBeetle.

Generating the id on the client side ensures that transfers can be safely retried. The app must use the same id each time the transfer is resent.

If the transfer was already created before and then retried, TigerBeetle will return the exists response code. If the transfer had not already been created, it will be created and return the ok.
Handling Client Software Restarts

The method described above also handles potential restarts of the app or browser while the request is in flight.

It is important to persist the id to local storage on the client’s device before submitting the transfer. When the app or web page reloads, it should resubmit the transfer using the same id.

This ensures that the operation can be safely retried even if the client app or browser restarts before receiving the response to the operation. Similar to the case of a network failure, TigerBeetle will respond with the ok if a transfer is newly created and exists if an object with the same id was already created.
Edit this page
Two-Phase Transfers

A two-phase transfer moves funds in stages:

    Reserve funds (pending)
    Resolve funds (post, void, or expire)

The name “two-phase transfer” is a reference to the two-phase commit protocol for distributed transactions.
Reserve Funds (Pending Transfer)

A pending transfer, denoted by flags.pending, reserves its amount in the debit/credit accounts’ debits_pending/credits_pending fields, respectively. Pending transfers leave the debits_posted/credits_posted unmodified.
Resolve Funds

Pending transfers can be posted, voided, or they may time out.
Post-Pending Transfer

A post-pending transfer, denoted by flags.post_pending_transfer, causes a pending transfer to “post”, transferring some or all of the pending transfer’s reserved amount to its destination.

    If the posted amount is less than the pending transfer’s amount, then only this amount is posted, and the remainder is restored to its original accounts.
    If the posted amount is equal to the pending transfer’s amount or equal to AMOUNT_MAX (2^128 - 1), the full pending transfer’s amount is posted.
    If the posted amount is greater than the pending transfer’s amount (but less than AMOUNT_MAX), exceeds_pending_transfer_amount is returned.

Client < 0.16.0

Additionally, when flags.post_pending_transfer is set:

    pending_id must reference a pending transfer
    flags.void_pending_transfer must not be set.

The following fields may either be zero or they must match the value of the pending transfer’s field:

    debit_account_id
    credit_account_id
    ledger
    code

Void-Pending Transfer

A void-pending transfer, denoted by flags.void_pending_transfer, restores the pending amount its original accounts. Additionally, when this field is set:

    pending_id must reference a pending transfer
    flags.post_pending_transfer must not be set.

The following fields may either be zero or they must match the value of the pending transfer’s field:

    debit_account_id
    credit_account_id
    ledger
    code

Expire Pending Transfer

A pending transfer may optionally be created with a timeout. If the timeout interval passes before the transfer is either posted or voided, the transfer expires and the full amount is returned to the original account.

Note that timeouts are given as intervals, specified in seconds, rather than as absolute timestamps. For more details on why, read the page about Time in TigerBeetle.
Errors

A pending transfer can only be posted or voided once. It cannot be posted twice or voided then posted, etc.

Attempting to resolve a pending transfer more than once will return the applicable error result:

    pending_transfer_already_posted
    pending_transfer_already_voided
    pending_transfer_expired

Interaction with Account Invariants

The pending transfer’s amount is reserved in a way that the second step in a two-phase transfer will never cause the accounts’ configured balance invariants (credits_must_not_exceed_debits or debits_must_not_exceed_credits) to be broken, whether the second step is a post or void.
Pessimistic Pending Transfers

If an account with debits_must_not_exceed_credits has credits_posted = 100 and debits_posted = 70 and a pending transfer is started causing the account to have debits_pending = 50, the pending transfer will fail. It will not wait to get to posted status to fail.
All Transfers Are Immutable

To reiterate, completing a two-phase transfer (by either marking it void or posted) does not involve modifying the pending transfer. Instead you create a new transfer.

The first transfer that is marked pending will always have its pending flag set.

The second transfer will have a post_pending_transfer or void_pending_transfer flag set and a pending_id field set to the id of the first transfer. The id of the second transfer will be unique, not the same id as the initial pending transfer.
Examples

The following examples show the state of two accounts in three steps:

    Initially, before any transfers
    After a pending transfer
    And after the pending transfer is posted or voided

Post Full Pending Amount
Account A 		Account B 		Transfers 			
debits 		credits 					
pending 	posted 	pending 	posted 	debit_account_id 	credit_account_id 	amount 	flags
w 	x 	y 	z 	- 	- 	- 	-
w + 123 	x 	y + 123 	z 	A 	B 	123 	pending
w 	x+ 123 	y 	z + 123 	A 	B 	123 	post_pending_transfer
Post Partial Pending Amount
Account A 		Account B 		Transfers 			
debits 		credits 					
pending 	posted 	pending 	posted 	debit_account_id 	credit_account_id 	amount 	flags
w 	x 	y 	z 	- 	- 	- 	-
w + 123 	x 	y + 123 	z 	A 	B 	123 	pending
w 	x + 100 	y 	z + 100 	A 	B 	100 	post_pending_transfer
Void Pending Transfer
Account A 		Account B 		Transfers 			
debits 		credits 					
pending 	posted 	pending 	posted 	debit_account_id 	credit_account_id 	amount 	flags
w 	x 	y 	z 	- 	- 	- 	-
w + 123 	x 	y + 123 	z 	A 	B 	123 	pending
w 	x 	y 	z 	A 	B 	123 	void_pending_transfer
Edit this page
Linked Events

Events within a request succeed or fail independently unless they are explicitly linked using flags.linked (Account.flags.linked or Transfer.flags.linked).

When the linked flag is specified, it links the outcome of a Transfer or Account creation with the outcome of the next one in the request. These chains of events will all succeed or fail together.

The last event in a chain is denoted by the first Transfer or Account without this flag.

The last Transfer or Account in a request may never have the flags.linked set, as it would leave a chain open-ended. Attempting to do so will result in the linked_event_chain_open error.

Multiple chains of events may coexist within a request to succeed or fail independently.

Events within a chain are executed in order, or are rolled back on error, so that the effect of each event in the chain is visible to the next. Each chain is either visible or invisible as a unit to subsequent transfers after the chain. The event that was the first to fail within a chain will have a unique error result. Other events in the chain will have their error result set to linked_event_failed.
Linked Transfers Example

Consider this set of Transfers as part of a request:
Transfer 	Index in Request 	flags.linked
A 	0 	false
B 	1 	true
C 	2 	true
D 	3 	false
E 	4 	false

If any of transfers B, C, or D fail (for example, due to exceeds_credits), then B, C, and D will all fail. They are linked.

Transfers A and E fail or succeed independently of B, C, D, and each other.

After the chain of linked events has executed, the fact that they were linked will not be saved. To save the association between Transfers or Accounts, it must be encoded into the data model, for example by adding an ID to one of the user data fields.
Edit this page
Time

Time is a critical component of all distributed systems and databases. Within TigerBeetle, we keep track of two types of time: logical time and physical time. Logical time is about ordering events relative to each other, and physical time is the everyday time, a numeric timestamp.
Logical Time

TigerBeetle uses a consensus protocol (Viewstamped Replication) to guarantee strict serializability for all operations.

In other words, to an external observer, TigerBeetle cluster behaves as if it is just a single machine which processes the incoming requests in order. If an application submits a batch of transfers with transfer T1, receives a reply, and then submits a batch with another transfer T2, it is guaranteed that T2 will observe the effects of T1. Note, however, that there could be concurrent requests from multiple applications, so, unless T1 and T2 are in the same batch of transfers, some other transfer could happen in between them. See the reference for precise guarantees.
Physical Time

TigerBeetle uses physical time in addition to the logical time provided by the consensus algorithm. Financial transactions require physical time for multiple reasons, including:

    Liquidity - TigerBeetle supports Two-Phase Transfers that reserve funds and hold them in a pending state until they are posted, voided, or the transfer times out. A timeout is useful to ensure that the reserved funds are not held in limbo indefinitely.
    Compliance and Auditing - For regulatory and security purposes, it is useful to have a specific idea of when (in terms of wall clock time) transfers took place.

TigerBeetle uses two-layered approach to physical time. On the basic layer, each replica asks the underling operating system about the current time. Then, timing information from several replicas is aggregated to make sure that the replicas roughly agree on the time, to prevent a replica with a bad clock from issuing incorrect timestamps. Additionally, this “cluster time” is made strictly monotonic, for end user’s convenience.
Why TigerBeetle Manages Timestamps

An important invariant is that the TigerBeetle cluster assigns all timestamps. In particular, timestamps on Transfers and Accounts are set by the cluster when the corresponding event arrives at the primary. This is why the timestamp field must be set to 0 when operations are submitted by the client.

Similarly, the Transfer.timeout is given as an interval in seconds, rather than as an absolute timestamp, because it is also managed by the primary. The timeout is calculated relative to the timestamp when the operation arrives at the primary.

This restriction is needed to make sure that any two timestamps always refer to the same underlying clock (cluster’s physical time) and are directly comparable. This in turn provides a set of powerful guarantees.
Timestamps are Totally Ordered

All timestamps within TigerBeetle are unique, immutable and totally ordered. A transfer that is created before another transfer is guaranteed to have an earlier timestamp (even if they were created in the same request).

In other systems this is also called a “physical” timestamp, “ingestion” timestamp, “record” timestamp, or “system” timestamp.
Further Reading

If you are curious how exactly it is that TigerBeetle achieves strictly monotonic physical time, we have a talk and a blog post with details:

    Detecting Clock Sync Failure in Highly Available Systems (YouTube)
    Three Clocks are Better than One (TigerBeetle Blog)

Edit this page
Recipes

A collection of solutions for common use-cases. Want to exchange some currency? Or made a wrong transfer and want to undo that? We have a recipe for that!

    Currency Exchange
    Multi-Debit, Multi-Credit Transfers
    Closing Accounts
    Balance-Conditional Transfers
    Balance-Invariant Transfers
    Balance Bounds
    Correcting Transfers
    Rate Limiting

Edit this page
Currency Exchange

Some applications require multiple currencies. For example, a bank may hold balances in many different currencies. If a single logical entity holds multiple currencies, each currency must be held in a separate TigerBeetle Account. (Normalizing to a single currency at the application level should be avoided because exchange rates fluctuate).

Currency exchange is a trade of one type of currency (denoted by the ledger) for another, facilitated by an entity called the liquidity provider.
Data Modeling

Distinct ledger values denote different currencies (or other asset types). Transfers between pairs of accounts with different ledgers are not permitted.

Instead, currency exchange is implemented by creating two atomically linked different-ledger transfers between two pairs of same-ledger accounts.

A simple currency exchange involves four accounts:

    A source account A₁, on ledger 1.
    A destination account A₂, on ledger 2.
    A source liquidity account L₁, on ledger 1.
    A destination liquidity account L₂, on ledger 2.

and two linked transfers:

    A transfer T₁ from the source account to the source liquidity account.
    A transfer T₂ from the destination liquidity account to the destination account.

The transfer amounts vary according to the exchange rate.

    Both liquidity accounts belong to the liquidity provider (e.g. a bank or exchange).
    The source and destination accounts may belong to the same entity as one another, or different entities, depending on the use case.

Example

Consider sending 100.00 USD from account A₁ (denominated in USD) to account A₂ (denominated in INR). Assuming an exchange rate of 1.00 USD = 82.42135 INR, 100.00 USD = 8242.14 INR:
Ledger 	Debit Account 	Credit Account 	Amount 	flags.linked
USD 	A₁ 	L₁ 	10000 	true
INR 	L₂ 	A₂ 	824214 	false

    Amounts are represented as integers.
    Because both liquidity accounts belong to the same entity, the entity does not lose money on the transaction.
        If the exchange rate is precise, the entity breaks even.
        If the exchange rate is not precise, the application should round in favor of the liquidity account to deter arbitrage.
    Because the two transfers are linked together, they will either both succeed or both fail.

Spread

In the prior example, the liquidity provider breaks even. A fee (i.e. spread) can be included in the linked chain as a separate transfer from the source account to the source liquidity account (A₁ to L₁).

This is preferable to simply modifying the exchange rate in the liquidity provider’s favor because it implicitly records the exchange rate and spread at the time of the exchange — information that cannot be derived if the two are combined.
Example

This depicts the same scenario as the prior example, except the liquidity provider charges a 0.10 USD fee for the transaction.
Ledger 	Debit Account 	Credit Account 	Amount 	flags.linked
USD 	A₁ 	L₁ 	10000 	true
USD 	A₁ 	L₁ 	10 	true
INR 	L₂ 	A₂ 	824214 	false
Edit this page
Multi-Debit, Multi-Credit Transfers

TigerBeetle is designed for maximum performance. In order to keep it lean, the database only supports simple transfers with a single debit and a single credit.

However, you’ll probably run into cases where you want transactions with multiple debits and/or credits. For example, you might have a transfer where you want to extract fees and/or taxes.

Read on to see how to implement one-to-many and many-to-many transfers!

    Note that all of these examples use the Linked Transfers flag (flags.linked) to ensure that all of the transfers succeed or fail together.

One-to-Many Transfers

Transactions that involve multiple debits and a single credit OR a single debit and multiple credits are relatively straightforward.

You can use multiple linked transfers as depicted below.
Single Debit, Multiple Credits

This example debits a single account and credits multiple accounts. It uses the following accounts:

    A source account A, on the USD ledger.
    Three destination accounts X, Y, and Z, on the USD ledger.

Ledger 	Debit Account 	Credit Account 	Amount 	flags.linked
USD 	A 	X 	10000 	true
USD 	A 	Y 	50 	true
USD 	A 	Z 	10 	false
Multiple Debits, Single Credit

This example debits multiple accounts and credits a single account. It uses the following accounts:

    Three source accounts A, B, and C on the USD ledger.
    A destination account X on the USD ledger.

Ledger 	Debit Account 	Credit Account 	Amount 	flags.linked
USD 	A 	X 	10000 	true
USD 	B 	X 	50 	true
USD 	C 	X 	10 	false
Multiple Debits, Single Credit, Balancing debits

This example debits multiple accounts and credits a single account. The total amount to transfer to the credit account is known (in this case, 100), but the balances of the individual debit accounts are not known. That is, each debit account should contribute as much as possible (in order of precedence) up to the target, cumulative transfer amount.

It uses the following accounts:

    Three source accounts A, B, and C, with flags.debits_must_not_exceed_credits.
    A destination account X.
    A control account LIMIT, with flags.debits_must_not_exceed_credits.
    A control account SETUP, for setting up the LIMIT account.

Id 	Ledger 	Debit Account 	Credit Account 	Amount 	Flags
1 	USD 	SETUP 	LIMIT 	100 	linked
2 	USD 	A 	SETUP 	100 	linked, balancing_debit, balancing_credit
3 	USD 	B 	SETUP 	100 	linked, balancing_debit, balancing_credit
4 	USD 	C 	SETUP 	100 	linked, balancing_debit, balancing_credit
5 	USD 	SETUP 	X 	100 	linked
6 	USD 	LIMIT 	SETUP 	AMOUNT_MAX 	balancing_credit

If the cumulative credit balance of A + B + C is less than 100, the chain will fail (transfer 6 will return exceeds_credits).
Many-to-Many Transfers

Transactions with multiple debits and multiple credits are a bit more involved (but you got this!).

This is where the accounting concept of a Control Account comes in handy. We can use this as an intermediary account, as illustrated below.

In this example, we’ll use the following accounts:

    Two source accounts A and B on the USD ledger.
    Three destination accounts X, Y, and Z, on the USD ledger.
    A compound entry control account Control on the USD ledger.

Ledger 	Debit Account 	Credit Account 	Amount 	flags.linked
USD 	A 	Control 	10000 	true
USD 	B 	Control 	50 	true
USD 	Control 	X 	9000 	true
USD 	Control 	Y 	1000 	true
USD 	Control 	Z 	50 	false

Here, we use two transfers to debit accounts A and B and credit the Control account, and another three transfers to credit accounts X, Y, and Z.

If you looked closely at this example, you may have noticed that we could have debited B and credited Z directly because the amounts happened to line up. That is true!

For a little more extreme performance, you might consider implementing logic to circumvent the control account where possible, to reduce the number of transfers to implement a compound journal entry.

However, if you’re just getting started, you can avoid premature optimizations (we’ve all been there!). You may find it easier to program these compound journal entries always using a control account – and you can then come back to squeeze this performance out later!
Edit this page
Close Account

In accounting, a closing entry calculates the net debit or credit balance for an account and then credits or debits this balance respectively, to zero the account’s balance and move the balance to another account.

Additionally, it may be desirable to forbid further transfers on this account (i.e. at the end of an accounting period, upon account termination, or even temporarily freezing the account for audit purposes). This doesn’t affect existing pending transfers, which can still time out but can’t be posted or voided.
Example

Given a set of accounts:
Account 	Debits Pending 	Debits Posted 	Credits Pending 	Credits Posted 	Flags
A 	0 	10 	0 	20 	debits_must_not_exceed_credits
B 	0 	30 	0 	5 	credits_must_not_exceed_debits
C 	0 	0 	0 	0 	

The “closing entries” for accounts A and B are expressed as linked chains, so they either succeed or fail atomically.

    Account A: the linked transfers are T1 and T2.

    Account B: the linked transfers are T3 and T4.

    Account C: is the control account and will not be closed.

Transfer 	Debit Account 	Credit Account 	Amount 	Amount (recorded) 	Flags
T1 	A 	C 	AMOUNT_MAX 	10 	balancing_debit,linked
T2 	A 	C 	0 	0 	closing_debit, pending
T3 	C 	B 	AMOUNT_MAX 	25 	balancing_credit,linked
T4 	C 	B 	0 	0 	closing_credit, pending

    T1 and T3 are balancing transfers with AMOUNT_MAX as the Transfer.amount so that the application does not need to know (or query) the balance prior to closing the account.

    The stored transfer’s amount will be set to the actual amount transferred.

    T2 and T4 are closing transfers that will cause the respective account to be closed.

    The closing transfer must be also a pending transfer so the action can be reversible.

After committing these transfers, A and B are closed with net balance zero, and will reject any further transfers.
Account 	Debits Pending 	Debits Posted 	Credits Pending 	Credits Posted 	Flags
A 	0 	20 	0 	20 	debits_must_not_exceed_credits, closed
B 	0 	30 	0 	30 	credits_must_not_exceed_debits, closed
C 	0 	25 	0 	10 	

To re-open the closed account, the pending closing transfer can be voided, reverting the closing action (but not reverting the net balance):
Transfer 	Debit Account 	Credit Account 	Amount 	Pending Transfer 	Flags
T5 	A 	C 	0 	T2 	void_pending_transfer
T6 	C 	B 	0 	T4 	void_pending_transfer

After committing these transfers, A and B are re-opened and can accept transfers again:
Account 	Debits Pending 	Debits Posted 	Credits Pending 	Credits Posted 	Flags
A 	0 	20 	0 	20 	debits_must_not_exceed_credits
B 	0 	30 	0 	30 	credits_must_not_exceed_debits
C 	0 	25 	0 	10 	
Edit this page
Balance-Conditional Transfers

In some use cases, you may want to execute a transfer if and only if an account has at least a certain balance.

It would be unsafe to check an account’s balance using the lookup_accounts and then perform the transfer, because these requests are not be atomic and the account’s balance may change between the lookup and the transfer.

You can atomically run a check against an account’s balance before executing a transfer by using a control or temporary account and linked transfers.
Preconditions
1. Target Account Must Have a Limited Balance

The account for whom you want to do the balance check must have one of these flags set:

    flags.debits_must_not_exceed_credits for accounts with credit balances
    flags.credits_must_not_exceed_debits for accounts with debit balances

2. Create a Control Account

There must also be a designated control account. As you can see below, this account will never actually take control of the target account’s funds, but we will set up simultaneous transfers in and out of the control account.
Executing a Balance-Conditional Transfer

The balance-conditional transfer consists of 3 linked transfers.

We will refer to two amounts:

    The threshold amount is the minimum amount the target account should have in order to execute the transfer.
    The transfer amount is the amount we want to transfer if and only if the target account’s balance meets the threshold.

If the Source Account Has a Credit Balance
Transfer 	Debit Account 	Credit Account 	Amount 	Pending Id 	Flags
1 	Source 	Control 	Threshold 	- 	flags.linked, pending
2 	- 	- 	- 	1 	flags.linked, void_pending_transfer
3 	Source 	Destination 	Transfer 	- 	N/A
If the Source Account Has a Debit Balance
Transfer 	Debit Account 	Credit Account 	Amount 	Pending Id 	Flags
1 	Control 	Source 	Threshold 	- 	flags.linked, pending
2 	- 	- 	- 	1 	flags.linked, void_pending_transfer
3 	Destination 	Source 	Transfer 	- 	N/A
Understanding the Mechanism

Each of the 3 transfers is linked, meaning they will all succeed or fail together.

The first transfer attempts to transfer the threshold amount to the control account. If this transfer would cause the source account’s net balance to go below zero, the account’s balance limit flag would ensure that the first transfer fails. If the first transfer fails, the other two linked transfers would also fail.

If the first transfer succeeds, it means that the source account did have the threshold balance. In this case, the second transfer cancels the first transfer (returning the threshold amount to the source account). Then, the third transfer would execute the desired transfer to the ultimate destination account.

Note that in the tables above, we do the balance check on the source account. The balance check could also be applied to the destination account instead.
Edit this page
Balance-invariant Transfers

For some accounts, it may be useful to enforce flags.debits_must_not_exceed_credits or flags.credits_must_not_exceed_debits balance invariants for only a subset of all transfers, rather than all transfers.

This can be achieved by having a control account used to test the balance invariants at the desired points in time. The control account will have a 0 balance and the balance invariant that we want to test on the destination account. At the point where we want to test the destination account balance invariant, we can initiate a pending balancing transfer for the opposite side to the control account. If the invariant is violated on the destination account, the balancing transfer has non-zero amount, violates the control account invariant, and fails the entire chain. The following example will make this clearer.
Per-transfer credits_must_not_exceed_debits

Let’s test a credits_must_not_exceed_debits balance invariant on a destination account after a particular transfer.

This recipe requires three accounts:

    The source account, to debit.
    The destination account, to credit. (With neither flags.credits_must_not_exceed_debits nor flags.debits_must_not_exceed_credits set, since in this recipe we are only enforcing the invariant on a per-transfer basis.)
    The control account, to test the balance invariant. The control account should have flags.credits_must_not_exceed_debits set.

Id 	Debit Account 	Credit Account 	Amount 	Pending Id 	Flags
1 	Source 	Destination 	123 	- 	linked
2 	Destination 	Control 	1 	- 	linked, pending, balancing_debit
3 	- 	- 	0 	2 	void_pending_transfer

When the destination account’s credits after transfer 1 do not exceed its debits, the chain will succeed. When the destination account’s credits after transfer 1 exceed its debits, transfer 2 will fail with exceeds_debits.
Per-transfer debits_must_not_exceed_credits

This case is symmetric:

    The source is account to credit.
    The destination is account to debit. Neither flags.credits_must_not_exceed_debits nor flags.debits_must_not_exceed_credits are set.
    The control account has flags.credits_must_not_exceed_debits set.

Id 	Debit Account 	Credit Account 	Amount 	Pending Id 	Flags
1 	Destination 	Source 	123 	- 	linked
2 	Control 	Destination 	1 	- 	linked, pending, balancing_credit
3 	- 	- 	0 	2 	void_pending_transfer
Edit this page
Balance Bounds

It is easy to limit an account’s balance using either flags.debits_must_not_exceed_credits or flags.credits_must_not_exceed_debits.

What if you want an account’s balance to stay between an upper and a lower bound?

This is possible to check atomically using a set of linked transfers. (Note: with the must_not_exceed flag invariants, an account is guaranteed to never violate those invariants. This maximum balance approach must be enforced per-transfer – it is possible to exceed the limit simply by not enforcing it for a particular transfer.)
Preconditions

    Target Account Should Have a Limited Balance

The account whose balance you want to bound should have one of these flags set:

    flags.debits_must_not_exceed_credits for accounts with credit balances
    flags.credits_must_not_exceed_debits for accounts with debit balances

    Create a Control Account with the Opposite Limit

There must also be a designated control account.

As you can see below, this account will never actually take control of the target account’s funds, but we will set up simultaneous transfers in and out of the control account to apply the limit.

This account must have the opposite limit applied as the target account:

    flags.credits_must_not_exceed_debits if the target account has a credit balance
    flags.debits_must_not_exceed_credits if the target account has a debit balance

    Create an Operator Account

The operator account will be used to fund the Control Account.
Executing a Transfer with a Balance Bounds Check

This consists of 5 linked transfers.

We will refer to two amounts:

    The limit amount is upper bound we want to maintain on the target account’s balance.
    The transfer amount is the amount we want to transfer if and only if the target account’s balance after a successful transfer would be within the bounds.

If the Target Account Has a Credit Balance

In this case, we are keeping the Destination Account’s balance between the bounds.
Transfer 	Debit Account 	Credit Account 	Amount 	Pending ID 	Flags (Note: | sets multiple flags)
1 	Source 	Destination 	Transfer 	- 	flags.linked
2 	Control 	Operator 	Limit 	- 	flags.linked
3 	Destination 	Control 	AMOUNT_MAX 	- 	flags.linked | flags.balancing_debit | flags.pending
4 	- 	- 	- 	3* 	flags.linked | flags.void_pending_transfer
5 	Operator 	Control 	Limit 	- 	-

*This must be set to the transfer ID of the pending transfer (in this example, it is transfer 3).
If the Target Account Has a Debit Balance

In this case, we are keeping the Destination Account’s balance between the bounds.
Transfer 	Debit Account 	Credit Account 	Amount 	Pending ID 	Flags (Note | sets multiple flags)
1 	Destination 	Source 	Transfer 	- 	flags.linked
2 	Operator 	Control 	Limit 	- 	flags.linked
3 	Control 	Destination 	AMOUNT_MAX 	- 	flags.balancing_credit | flags.pending | flags.linked
4 	- 	- 	- 	3* 	flags.void_pending_transfer | flags.linked
5 	Control 	Operator 	Limit 	- 	-

*This must be set to the transfer ID of the pending transfer (in this example, it is transfer 3).
Understanding the Mechanism

Each of the 5 transfers is linked so that all of them will succeed or all of them will fail.

The first transfer is the one we actually want to send.

The second transfer sets the Control Account’s balance to the upper bound we want to impose.

The third transfer uses a balancing_debit or balancing_credit to transfer the Destination Account’s net credit balance or net debit balance, respectively, to the Control Account. This transfer will fail if the first transfer would put the Destination Account’s balance above the upper bound.

The third transfer is also a pending transfer, so it won’t actually transfer the Destination Account’s funds, even if it succeeds.

If everything to this point succeeds, the fourth and fifth transfers simply undo the effects of the second and third transfers. The fourth transfer voids the pending transfer. And the fifth transfer resets the Control Account’s net balance to zero.
Edit this page
Correcting Transfers

Transfers in TigerBeetle are immutable, so once they are created they cannot be modified or deleted.

Immutability is useful for creating an auditable log of all of the business events, but it does raise the question of what to do when a transfer was made in error or some detail such as the amount was incorrect.
Always Add More Transfers

Correcting transfers or entries in TigerBeetle are handled with more transfers to reverse or adjust the effects of the previous transfer(s).

This is important because adding transfers as opposed to deleting or modifying incorrect ones adds more information to the history. The log of events includes the original error, when it took place, as well as any attempts to correct the record and when they took place. A correcting entry might even be wrong, in which case it itself can be corrected with yet another transfer. All of these events form a timeline of the particular business event, which is stored permanently.

Another way to put this is that TigerBeetle is the lowest layer of the accounting stack and represents the finest-resolution data that is stored. At a higher-level reporting layer, you can “downsample” the data to show only the corrected transfer event. However, it would not be possible to go back if the original record were modified or deleted.

Two specific recommendations for correcting transfers are:

    You may want to have a Transfer.code that indicates a given transfer is a correction, or you may want multiple codes where each one represents a different reason why the correction has taken place.
    If you use the Transfer.user_data_128 to store an ID that links multiple transfers within TigerBeetle or points to a record in an external database, you may want to use the same user_data_128 field on the correction transfer(s), even if they happen at a later point.

Example

Let’s say you had a couple of transfers, from account A to accounts X and Y:
Ledger 	Debit Account 	Credit Account 	Amount 	code 	user_data_128 	flags.linked
USD 	A 	X 	10000 	600 	123456 	true
USD 	A 	Y 	50 	9000 	123456 	false

Now, let’s say we realized the amount was wrong and we need to adjust both of the amounts by 10%. We would submit two additional transfers going in the opposite direction:
Ledger 	Debit Account 	Credit Account 	Amount 	code 	user_data_128 	flags.linked
USD 	X 	A 	1000 	10000 	123456 	true
USD 	Y 	A 	5 	10000 	123456 	false

Note that the codes used here don’t have any actual meaning, but you would want to enumerate your business events and map each to a numeric code value, including the initial reasons for transfers and the reasons they might be corrected.
Edit this page
Rate Limiting

TigerBeetle can be used to account for non-financial resources.

In this recipe, we will show you how to use it to implement rate limiting using the leaky bucket algorithm based on the user request rate, bandwidth, and money.
Mechanism

For each type of resource we want to limit, we will have a ledger specifically for that resource. On that ledger, we have an operator account and an account for each user. Each user’s account will have a balance limit applied.

To set up the rate limiting system, we will first credit the resource limit amount to each of the users. For each user request, we will then create a pending transfer with a timeout. We will never post or void these transfers, but will instead let them expire.

Since each account’s credit “balance” is limited, requesting a pending transfer that would exceed the rate limit will fail. However, when each pending transfer expires, the pending amounts are automatically restored to the available balance.
Request Rate Limiting

Let’s say we want to limit each user to 10 requests per minute.

We need our user account to have a limited balance.
Ledger 	Account 	Flags
Request Rate 	Operator 	0
Request Rate 	User 	debits_must_not_exceed_credits

We’ll first transfer 10 units from the operator to the user.
Transfer 	Ledger 	Debit Account 	Credit Account 	Amount
1 	Request Rate 	Operator 	User 	10

Then, for each incoming request, we will create a pending transfer for 1 unit back to the operator from the user:
Transfer 	Ledger 	Debit Account 	Credit Account 	Amount 	Timeout 	Flags
2…N 	Request Rate 	User 	Operator 	1 	60 	pending

Note that we use a timeout of 60 (seconds), because we wanted to limit each user to 10 requests per minute.

That’s it! Each of these transfers will “reserve” some of the user’s balance and then replenish the balance after they expire.
Bandwidth Limiting

To limit user requests based on bandwidth as opposed to request rate, we can apply the same technique but use amounts that represent the request size.

Let’s say we wanted to limit each user to 10 MB (10,000,000 bytes) per minute.

Our account setup is the same as before:
Ledger 	Account 	Flags
Bandwidth 	Operator 	0
Bandwidth 	User 	debits_must_not_exceed_credits

Now, we’ll transfer 10,000,000 units (bytes in this case) from the operator to the user:
Transfer 	Ledger 	Debit Account 	Credit Account 	Amount
1 	Bandwidth 	Operator 	User 	10000000

For each incoming request, we’ll create a pending transfer where the amount is equal to the request size:
Transfer 	Ledger 	Debit Account 	Credit Account 	Amount 	Timeout 	Flags
2…N 	Bandwidth 	User 	Operator 	Request Size 	60 	pending

We’re again using a timeout of 60 seconds, but you could adjust this to be whatever time window you want to use to limit requests.
Transfer Amount Limiting

Now, let’s say you wanted to limit each account to transferring no more than a certain amount of money per time window. We can do that using 2 ledgers and linked transfers.
Ledger 	Account 	Flags
Rate Limiting 	Operator 	0
Rate Limiting 	User 	debits_must_not_exceed_credits
USD 	Operator 	0
USD 	User 	debits_must_not_exceed_credits

Let’s say we wanted to limit each account to sending no more than 1000 USD per day.

To set up, we transfer 1000 from the Operator to the User on the Rate Limiting ledger:
Transfer 	Ledger 	Debit Account 	Credit Account 	Amount
1 	Rate Limiting 	Operator 	User 	1000

For each transfer the user wants to do, we will create 2 transfers that are linked:
Transfer 	Ledger 	Debit Account 	Credit Account 	Amount 	Timeout 	Flags (Note | sets multiple flags)
2N 	Rate Limiting 	User 	Operator 	Transfer Amount 	86400 	pending | linked
2N + 1 	USD 	User 	Destination 	Transfer Amount 	0 	0

Note that we are using a timeout of 86400 seconds, because this is the number of seconds in a day.

These are linked such that if the first transfer fails, because the user has already transferred too much money in the past day, the second transfer will also fail.
Edit this page
Clients

TigerBeetle has official client libraries for the following languages:

    .NET (nuget package).
    Go (package, API docs).
    Java (maven central package, API docs).
    Node.js (npm package).
    Python (PyPi package).
    Rust (Cargo package).

Subscribe to the tracking issue #2231 to receive notifications about breaking changes!

Please report any client bugs to the main issue tracker.
Edit this page
tigerbeetle-dotnet

The TigerBeetle client for .NET.
Prerequisites

Linux >= 5.6 is the only production environment we support. But for ease of development we also support macOS and Windows.

    .NET >= 8.0.

And if you do not already have NuGet.org as a package source, make sure to add it:

dotnet nuget add source https://api.nuget.org/v3/index.json -n nuget.org

Setup

First, create a directory for your project and cd into the directory.

Then, install the TigerBeetle client:

dotnet new console
dotnet add package tigerbeetle

Now, create Program.cs and copy this into it:

using System;

using TigerBeetle;

// Validate import works.
Console.WriteLine("SUCCESS");

Finally, build and run:

dotnet run

Now that all prerequisites and dependencies are correctly set up, let’s dig into using TigerBeetle.
Sample projects

This document is primarily a reference guide to the client. Below are various sample projects demonstrating features of TigerBeetle.

    Basic: Create two accounts and transfer an amount between them.
    Two-Phase Transfer: Create two accounts and start a pending transfer between them, then post the transfer.
    Many Two-Phase Transfers: Create two accounts and start a number of pending transfers between them, posting and voiding alternating transfers.

Creating a Client

A client is created with a cluster ID and replica addresses for all replicas in the cluster. The cluster ID and replica addresses are both chosen by the system that starts the TigerBeetle cluster.

Clients are thread-safe and a single instance should be shared between multiple concurrent tasks. This allows events to be automatically batched.

Multiple clients are useful when connecting to more than one TigerBeetle cluster.

In this example the cluster ID is 0 and there is one replica. The address is read from the TB_ADDRESS environment variable and defaults to port 3000.

var tbAddress = Environment.GetEnvironmentVariable("TB_ADDRESS");
var clusterID = UInt128.Zero;
var addresses = new[] { tbAddress != null ? tbAddress : "3000" };
using (var client = new Client(clusterID, addresses))
{
    // Use client
}

The following are valid addresses:

    3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1:3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1 (interpreted as 127.0.0.1:3001, 3001 is the default port)

Creating Accounts

See details for account fields in the Accounts reference.

var accounts = new[] {
    new Account
    {
        Id = ID.Create(), // TigerBeetle time-based ID.
        UserData128 = 0,
        UserData64 = 0,
        UserData32 = 0,
        Ledger = 1,
        Code = 718,
        Flags = AccountFlags.None,
        Timestamp = 0,
    },
};

var accountErrors = client.CreateAccounts(accounts);
// Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.

The UInt128 fields like ID, UserData128, Amount and account balances have a few extension methods to make it easier to convert 128-bit little-endian unsigned integers between BigInteger, byte[], and Guid.

See the class UInt128Extensions for more details.
Account Flags

The account flags value is a bitfield. See details for these flags in the Accounts reference.

To toggle behavior for an account, combine enum values stored in the AccountFlags object with bitwise-or:

    AccountFlags.None
    AccountFlags.Linked
    AccountFlags.DebitsMustNotExceedCredits
    AccountFlags.CreditsMustNotExceedDebits
    AccountFlags.History

For example, to link two accounts where the first account additionally has the debits_must_not_exceed_credits constraint:

var account0 = new Account
{
    Id = 100,
    Ledger = 1,
    Code = 1,
    Flags = AccountFlags.Linked | AccountFlags.DebitsMustNotExceedCredits,
};
var account1 = new Account
{
    Id = 101,
    Ledger = 1,
    Code = 1,
    Flags = AccountFlags.History,
};

var accountErrors = client.CreateAccounts(new[] { account0, account1 });
// Error handling omitted.

Response and Errors

The response is an empty array if all accounts were created successfully. If the response is non-empty, each object in the response array contains error information for an account that failed. The error object contains an error code and the index of the account in the request batch.

See all error conditions in the create_accounts reference.

var account0 = new Account
{
    Id = 102,
    Ledger = 1,
    Code = 1,
    Flags = AccountFlags.None,
};
var account1 = new Account
{
    Id = 103,
    Ledger = 1,
    Code = 1,
    Flags = AccountFlags.None,
};
var account2 = new Account
{
    Id = 104,
    Ledger = 1,
    Code = 1,
    Flags = AccountFlags.None,
};

var accountErrors = client.CreateAccounts(new[] { account0, account1, account2 });
foreach (var error in accountErrors)
{
    switch (error.Result)
    {
        case CreateAccountResult.Exists:
            Console.WriteLine($"Batch account at ${error.Index} already exists.");
            break;
        default:
            Console.WriteLine($"Batch account at ${error.Index} failed to create ${error.Result}");
            break;
    }
    return;
}

Account Lookup

Account lookup is batched, like account creation. Pass in all IDs to fetch. The account for each matched ID is returned.

If no account matches an ID, no object is returned for that account. So the order of accounts in the response is not necessarily the same as the order of IDs in the request. You can refer to the ID field in the response to distinguish accounts.

Account[] accounts = client.LookupAccounts(new UInt128[] { 100, 101 });

Create Transfers

This creates a journal entry between two accounts.

See details for transfer fields in the Transfers reference.

var transfers = new[] {
    new Transfer
    {
        Id = ID.Create(), // TigerBeetle time-based ID.
        DebitAccountId = 102,
        CreditAccountId = 103,
        Amount = 10,
        UserData128 = 0,
        UserData64 = 0,
        UserData32 = 0,
        Timeout = 0,
        Ledger = 1,
        Code = 1,
        Flags = TransferFlags.None,
        Timestamp = 0,
    }
};

var transferErrors = client.CreateTransfers(transfers);
// Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.
Response and Errors

The response is an empty array if all transfers were created successfully. If the response is non-empty, each object in the response array contains error information for a transfer that failed. The error object contains an error code and the index of the transfer in the request batch.

See all error conditions in the create_transfers reference.

var transfers = new[] {
    new Transfer
    {
        Id = 1,
        DebitAccountId = 102,
        CreditAccountId = 103,
        Amount = 10,
        Ledger = 1,
        Code = 1,
        Flags = TransferFlags.None,
    },
    new Transfer
    {
        Id = 2,
        DebitAccountId = 102,
        CreditAccountId = 103,
        Amount = 10,
        Ledger = 1,
        Code = 1,
        Flags = TransferFlags.None,
    },
    new Transfer
    {
        Id = 3,
        DebitAccountId = 102,
        CreditAccountId = 103,
        Amount = 10,
        Ledger = 1,
        Code = 1,
        Flags = TransferFlags.None,
    },
};

var transferErrors = client.CreateTransfers(transfers);
foreach (var error in transferErrors)
{
    switch (error.Result)
    {
        case CreateTransferResult.Exists:
            Console.WriteLine($"Batch transfer at ${error.Index} already exists.");
            break;
        default:
            Console.WriteLine($"Batch transfer at ${error.Index} failed to create: ${error.Result}");
            break;
    }
}

Batching

TigerBeetle performance is maximized when you batch API requests. A client instance shared across multiple threads/tasks can automatically batch concurrent requests, but the application must still send as many events as possible in a single call. For example, if you insert 1 million transfers sequentially, one at a time, the insert rate will be a fraction of the potential, because the client will wait for a reply between each one.

var batch = new Transfer[] { }; // Array of transfer to create.
foreach (var t in batch)
{
    var transferErrors = client.CreateTransfer(t);
    // Error handling omitted.
}

Instead, always batch as much as you can. The maximum batch size is set in the TigerBeetle server. The default is 8189.

var batch = new Transfer[] { }; // Array of transfer to create.
var BATCH_SIZE = 8189;
for (int firstIndex = 0; firstIndex < batch.Length; firstIndex += BATCH_SIZE)
{
    var lastIndex = firstIndex + BATCH_SIZE;
    if (lastIndex > batch.Length)
    {
        lastIndex = batch.Length;
    }
    var transferErrors = client.CreateTransfers(batch[firstIndex..lastIndex]);
    // Error handling omitted.
}

Queues and Workers

If you are making requests to TigerBeetle from workers pulling jobs from a queue, you can batch requests to TigerBeetle by having the worker act on multiple jobs from the queue at once rather than one at a time. i.e. pulling multiple jobs from the queue rather than just one.
Transfer Flags

The transfer flags value is a bitfield. See details for these flags in the Transfers reference.

To toggle behavior for an account, combine enum values stored in the TransferFlags object with bitwise-or:

    TransferFlags.None
    TransferFlags.Linked
    TransferFlags.Pending
    TransferFlags.PostPendingTransfer
    TransferFlags.VoidPendingTransfer

For example, to link transfer0 and transfer1:

var transfer0 = new Transfer
{
    Id = 4,
    DebitAccountId = 102,
    CreditAccountId = 103,
    Amount = 10,
    Ledger = 1,
    Code = 1,
    Flags = TransferFlags.Linked,
};
var transfer1 = new Transfer
{
    Id = 5,
    DebitAccountId = 102,
    CreditAccountId = 103,
    Amount = 10,
    Ledger = 1,
    Code = 1,
    Flags = TransferFlags.None,
};

var transferErrors = client.CreateTransfers(new[] { transfer0, transfer1 });
// Error handling omitted.

Two-Phase Transfers

Two-phase transfers are supported natively by toggling the appropriate flag. TigerBeetle will then adjust the credits_pending and debits_pending fields of the appropriate accounts. A corresponding post pending transfer then needs to be sent to post or void the transfer.
Post a Pending Transfer

With flags set to post_pending_transfer, TigerBeetle will post the transfer. TigerBeetle will atomically roll back the changes to debits_pending and credits_pending of the appropriate accounts and apply them to the debits_posted and credits_posted balances.

var transfer0 = new Transfer
{
    Id = 6,
    DebitAccountId = 102,
    CreditAccountId = 103,
    Amount = 10,
    Ledger = 1,
    Code = 1,
    Flags = TransferFlags.Pending,
};

var transferErrors = client.CreateTransfers(new[] { transfer0 });
// Error handling omitted.

var transfer1 = new Transfer
{
    Id = 7,
    // Post the entire pending amount.
    Amount = Transfer.AmountMax,
    PendingId = 6,
    Flags = TransferFlags.PostPendingTransfer,
};

transferErrors = client.CreateTransfers(new[] { transfer1 });
// Error handling omitted.

Void a Pending Transfer

In contrast, with flags set to void_pending_transfer, TigerBeetle will void the transfer. TigerBeetle will roll back the changes to debits_pending and credits_pending of the appropriate accounts and not apply them to the debits_posted and credits_posted balances.

var transfer0 = new Transfer
{
    Id = 8,
    DebitAccountId = 102,
    CreditAccountId = 103,
    Amount = 10,
    Ledger = 1,
    Code = 1,
    Flags = TransferFlags.Pending,
};

var transferErrors = client.CreateTransfers(new[] { transfer0 });
// Error handling omitted.

var transfer1 = new Transfer
{
    Id = 9,
    Amount = 0,
    PendingId = 8,
    Flags = TransferFlags.VoidPendingTransfer,
};

transferErrors = client.CreateTransfers(new[] { transfer1 });
// Error handling omitted.

Transfer Lookup

NOTE: While transfer lookup exists, it is not a flexible query API. We are developing query APIs and there will be new methods for querying transfers in the future.

Transfer lookup is batched, like transfer creation. Pass in all ids to fetch, and matched transfers are returned.

If no transfer matches an id, no object is returned for that transfer. So the order of transfers in the response is not necessarily the same as the order of ids in the request. You can refer to the id field in the response to distinguish transfers.

Transfer[] transfers = client.LookupTransfers(new UInt128[] { 1, 2 });

Get Account Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the transfers involving a given account, allowing basic filter and pagination capabilities.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

var filter = new AccountFilter
{
    AccountId = 101,
    UserData128 = 0, // No filter by UserData.
    UserData64 = 0,
    UserData32 = 0,
    Code = 0, // No filter by Code.
    TimestampMin = 0, // No filter by Timestamp.
    TimestampMax = 0, // No filter by Timestamp.
    Limit = 10, // Limit to ten transfers at most.
    Flags = AccountFilterFlags.Debits | // Include transfer from the debit side.
        AccountFilterFlags.Credits | // Include transfer from the credit side.
        AccountFilterFlags.Reversed, // Sort by timestamp in reverse-chronological order.
};

Transfer[] transfers = client.GetAccountTransfers(filter);

Get Account Balances

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the point-in-time balances of a given account, allowing basic filter and pagination capabilities.

Only accounts created with the flag history set retain historical balances.

The balances in the response are sorted by timestamp in chronological or reverse-chronological order.

var filter = new AccountFilter
{
    AccountId = 101,
    UserData128 = 0, // No filter by UserData.
    UserData64 = 0,
    UserData32 = 0,
    Code = 0, // No filter by Code.
    TimestampMin = 0, // No filter by Timestamp.
    TimestampMax = 0, // No filter by Timestamp.
    Limit = 10, // Limit to ten balances at most.
    Flags = AccountFilterFlags.Debits | // Include transfer from the debit side.
        AccountFilterFlags.Credits | // Include transfer from the credit side.
        AccountFilterFlags.Reversed, // Sort by timestamp in reverse-chronological order.
};

AccountBalance[] accountBalances = client.GetAccountBalances(filter);

Query Accounts

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query accounts by the intersection of some fields and by timestamp range.

The accounts in the response are sorted by timestamp in chronological or reverse-chronological order.

var filter = new QueryFilter
{
    UserData128 = 1000, // Filter by UserData.
    UserData64 = 100,
    UserData32 = 10,
    Code = 1, // Filter by Code.
    Ledger = 0, // No filter by Ledger.
    TimestampMin = 0, // No filter by Timestamp.
    TimestampMax = 0, // No filter by Timestamp.
    Limit = 10, // Limit to ten accounts at most.
    Flags = QueryFilterFlags.Reversed, // Sort by timestamp in reverse-chronological order.
};

Account[] accounts = client.QueryAccounts(filter);

Query Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query transfers by the intersection of some fields and by timestamp range.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

var filter = new QueryFilter
{
    UserData128 = 1000, // Filter by UserData
    UserData64 = 100,
    UserData32 = 10,
    Code = 1, // Filter by Code
    Ledger = 0, // No filter by Ledger
    TimestampMin = 0, // No filter by Timestamp.
    TimestampMax = 0, // No filter by Timestamp.
    Limit = 10, // Limit to ten transfers at most.
    Flags = QueryFilterFlags.Reversed, // Sort by timestamp in reverse-chronological order.
};

Transfer[] transfers = client.QueryTransfers(filter);

Linked Events

When the linked flag is specified for an account when creating accounts or a transfer when creating transfers, it links that event with the next event in the batch, to create a chain of events, of arbitrary length, which all succeed or fail together. The tail of a chain is denoted by the first event without this flag. The last event in a batch may therefore never have the linked flag set as this would leave a chain open-ended. Multiple chains or individual events may coexist within a batch to succeed or fail independently.

Events within a chain are executed within order, or are rolled back on error, so that the effect of each event in the chain is visible to the next, and so that the chain is either visible or invisible as a unit to subsequent events after the chain. The event that was the first to break the chain will have a unique error result. Other events in the chain will have their error result set to linked_event_failed.

var batch = new System.Collections.Generic.List<Transfer>();

// An individual transfer (successful):
batch.Add(new Transfer { Id = 1, /* ... rest of transfer ... */ });

// A chain of 4 transfers (the last transfer in the chain closes the chain with linked=false):
batch.Add(new Transfer { Id = 2, /* ... rest of transfer ... */ Flags = TransferFlags.Linked }); // Commit/rollback.
batch.Add(new Transfer { Id = 3, /* ... rest of transfer ... */ Flags = TransferFlags.Linked }); // Commit/rollback.
batch.Add(new Transfer { Id = 2, /* ... rest of transfer ... */ Flags = TransferFlags.Linked }); // Fail with exists
batch.Add(new Transfer { Id = 4, /* ... rest of transfer ... */ }); // Fail without committing

// An individual transfer (successful):
// This should not see any effect from the failed chain above.
batch.Add(new Transfer { Id = 2, /* ... rest of transfer ... */ });

// A chain of 2 transfers (the first transfer fails the chain):
batch.Add(new Transfer { Id = 2, /* ... rest of transfer ... */ Flags = TransferFlags.Linked });
batch.Add(new Transfer { Id = 3, /* ... rest of transfer ... */ });

// A chain of 2 transfers (successful):
batch.Add(new Transfer { Id = 3, /* ... rest of transfer ... */ Flags = TransferFlags.Linked });
batch.Add(new Transfer { Id = 4, /* ... rest of transfer ... */ });

var transferErrors = client.CreateTransfers(batch.ToArray());
// Error handling omitted.

Imported Events

When the imported flag is specified for an account when creating accounts or a transfer when creating transfers, it allows importing historical events with a user-defined timestamp.

The entire batch of events must be set with the flag imported.

It’s recommended to submit the whole batch as a linked chain of events, ensuring that if any event fails, none of them are committed, preserving the last timestamp unchanged. This approach gives the application a chance to correct failed imported events, re-submitting the batch again with the same user-defined timestamps.

// External source of time
ulong historicalTimestamp = 0UL;
var historicalAccounts = new Account[] { /* Loaded from an external source */ };
var historicalTransfers = new Transfer[] { /* Loaded from an external source */ };

// First, load and import all accounts with their timestamps from the historical source.
var accountsBatch = new System.Collections.Generic.List<Account>();
for (var index = 0; index < historicalAccounts.Length; index++)
{
    var account = historicalAccounts[index];

    // Set a unique and strictly increasing timestamp.
    historicalTimestamp += 1;
    account.Timestamp = historicalTimestamp;
    // Set the account as `imported`.
    account.Flags = AccountFlags.Imported;
    // To ensure atomicity, the entire batch (except the last event in the chain)
    // must be `linked`.
    if (index < historicalAccounts.Length - 1)
    {
        account.Flags |= AccountFlags.Linked;
    }

    accountsBatch.Add(account);
}

var accountErrors = client.CreateAccounts(accountsBatch.ToArray());
// Error handling omitted.

// Then, load and import all transfers with their timestamps from the historical source.
var transfersBatch = new System.Collections.Generic.List<Transfer>();
for (var index = 0; index < historicalTransfers.Length; index++)
{
    var transfer = historicalTransfers[index];

    // Set a unique and strictly increasing timestamp.
    historicalTimestamp += 1;
    transfer.Timestamp = historicalTimestamp;
    // Set the account as `imported`.
    transfer.Flags = TransferFlags.Imported;
    // To ensure atomicity, the entire batch (except the last event in the chain)
    // must be `linked`.
    if (index < historicalTransfers.Length - 1)
    {
        transfer.Flags |= TransferFlags.Linked;
    }

    transfersBatch.Add(transfer);
}

var transferErrors = client.CreateTransfers(transfersBatch.ToArray());
// Error handling omitted.
// Since it is a linked chain, in case of any error the entire batch is rolled back and can be retried
// with the same historical timestamps without regressing the cluster timestamp.

Timeouts And Cancellation

The Client retries indefinitely and doesn’t impose any per-request timeout. Cancellation is provided as a mechanism, and the specific cancellation policy is left to the application. A Client instance can be closed at any time. On close, all in-flight requests are canceled and return an error to the caller. Even if an error is returned, a request might still be processed by the TigerBeetle server. Reliable transaction submission explains how to make transfers retry-proof using IDs for end-to-end idempotency.
Edit this page
tigerbeetle-go

The TigerBeetle client for Go.

Go Reference

Make sure to import github.com/tigerbeetle/tigerbeetle-go, not this repo and subdirectory.
Prerequisites

Linux >= 5.6 is the only production environment we support. But for ease of development we also support macOS and Windows.

    Go >= 1.21

Additionally on Windows: you must install Zig 0.14.1 and set the CC environment variable to zig.exe cc. Use the full path for zig.exe.
Setup

First, create a directory for your project and cd into the directory.

Then, install the TigerBeetle client:

go mod init tbtest
go get github.com/tigerbeetle/tigerbeetle-go

Now, create main.go and copy this into it:

package main

import (
    "fmt"
    "log"
    "os"

    . "github.com/tigerbeetle/tigerbeetle-go"
    . "github.com/tigerbeetle/tigerbeetle-go/pkg/types"
)

func main() {
    fmt.Println("Import ok!")
}

Finally, build and run:

go run main.go

Now that all prerequisites and dependencies are correctly set up, let’s dig into using TigerBeetle.
Sample projects

This document is primarily a reference guide to the client. Below are various sample projects demonstrating features of TigerBeetle.

    Basic: Create two accounts and transfer an amount between them.
    Two-Phase Transfer: Create two accounts and start a pending transfer between them, then post the transfer.
    Many Two-Phase Transfers: Create two accounts and start a number of pending transfers between them, posting and voiding alternating transfers.

Creating a Client

A client is created with a cluster ID and replica addresses for all replicas in the cluster. The cluster ID and replica addresses are both chosen by the system that starts the TigerBeetle cluster.

Clients are thread-safe and a single instance should be shared between multiple concurrent tasks. This allows events to be automatically batched.

Multiple clients are useful when connecting to more than one TigerBeetle cluster.

In this example the cluster ID is 0 and there is one replica. The address is read from the TB_ADDRESS environment variable and defaults to port 3000.

tbAddress := os.Getenv("TB_ADDRESS")
if len(tbAddress) == 0 {
    tbAddress = "3000"
}
client, err := NewClient(ToUint128(0), []string{tbAddress})
if err != nil {
    log.Printf("Error creating client: %s", err)
    return
}
defer client.Close()

The following are valid addresses:

    3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1:3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1 (interpreted as 127.0.0.1:3001, 3001 is the default port)

Creating Accounts

See details for account fields in the Accounts reference.

accountErrors, err := client.CreateAccounts([]Account{
    {
        ID:          ID(), // TigerBeetle time-based ID.
        UserData128: ToUint128(0),
        UserData64:  0,
        UserData32:  0,
        Ledger:      1,
        Code:        718,
        Flags:       0,
        Timestamp:   0,
    },
})
// Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.

The Uint128 fields like ID, UserData128, Amount and account balances have a few helper functions to make it easier to convert 128-bit little-endian unsigned integers between string, math/big.Int, and []byte.

See the type Uint128 for more details.
Account Flags

The account flags value is a bitfield. See details for these flags in the Accounts reference.

To toggle behavior for an account, use the types.AccountFlags struct to combine enum values and generate a uint16. Here are a few examples:

    AccountFlags{Linked: true}.ToUint16()
    AccountFlags{DebitsMustNotExceedCredits: true}.ToUint16()
    AccountFlags{CreditsMustNotExceedDebits: true}.ToUint16()
    AccountFlags{History: true}.ToUint16()

For example, to link two accounts where the first account additionally has the debits_must_not_exceed_credits constraint:

account0 := Account{
    ID:     ToUint128(100),
    Ledger: 1,
    Code:   718,
    Flags: AccountFlags{
        DebitsMustNotExceedCredits: true,
        Linked:                     true,
    }.ToUint16(),
}
account1 := Account{
    ID:     ToUint128(101),
    Ledger: 1,
    Code:   718,
    Flags: AccountFlags{
        History: true,
    }.ToUint16(),
}

accountErrors, err := client.CreateAccounts([]Account{account0, account1})
// Error handling omitted.

Response and Errors

The response is an empty array if all accounts were created successfully. If the response is non-empty, each object in the response array contains error information for an account that failed. The error object contains an error code and the index of the account in the request batch.

See all error conditions in the create_accounts reference.

account0 := Account{
    ID:     ToUint128(102),
    Ledger: 1,
    Code:   718,
    Flags:  0,
}
account1 := Account{
    ID:     ToUint128(103),
    Ledger: 1,
    Code:   718,
    Flags:  0,
}
account2 := Account{
    ID:     ToUint128(104),
    Ledger: 1,
    Code:   718,
    Flags:  0,
}

accountErrors, err := client.CreateAccounts([]Account{account0, account1, account2})
if err != nil {
    log.Printf("Error creating accounts: %s", err)
    return
}

for _, err := range accountErrors {
    switch err.Index {
    case uint32(AccountExists):
        log.Printf("Batch account at %d already exists.", err.Index)
    default:
        log.Printf("Batch account at %d failed to create: %s", err.Index, err.Result)
    }
}

To handle errors you can either 1) exactly match error codes returned from client.createAccounts with enum values in the CreateAccountError object, or you can 2) look up the error code in the CreateAccountError object for a human-readable string.
Account Lookup

Account lookup is batched, like account creation. Pass in all IDs to fetch. The account for each matched ID is returned.

If no account matches an ID, no object is returned for that account. So the order of accounts in the response is not necessarily the same as the order of IDs in the request. You can refer to the ID field in the response to distinguish accounts.

accounts, err := client.LookupAccounts([]Uint128{ToUint128(100), ToUint128(101)})

Create Transfers

This creates a journal entry between two accounts.

See details for transfer fields in the Transfers reference.

transfers := []Transfer{{
    ID:              ID(), // TigerBeetle time-based ID.
    DebitAccountID:  ToUint128(101),
    CreditAccountID: ToUint128(102),
    Amount:          ToUint128(10),
    Ledger:          1,
    Code:            1,
    Flags:           0,
    Timestamp:       0,
}}

transferErrors, err := client.CreateTransfers(transfers)
// Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.
Response and Errors

The response is an empty array if all transfers were created successfully. If the response is non-empty, each object in the response array contains error information for a transfer that failed. The error object contains an error code and the index of the transfer in the request batch.

See all error conditions in the create_transfers reference.

transfers := []Transfer{{
    ID:              ToUint128(1),
    DebitAccountID:  ToUint128(101),
    CreditAccountID: ToUint128(102),
    Amount:          ToUint128(10),
    Ledger:          1,
    Code:            1,
    Flags:           0,
}, {
    ID:              ToUint128(2),
    DebitAccountID:  ToUint128(101),
    CreditAccountID: ToUint128(102),
    Amount:          ToUint128(10),
    Ledger:          1,
    Code:            1,
    Flags:           0,
}, {
    ID:              ToUint128(3),
    DebitAccountID:  ToUint128(101),
    CreditAccountID: ToUint128(102),
    Amount:          ToUint128(10),
    Ledger:          1,
    Code:            1,
    Flags:           0,
}}

transferErrors, err := client.CreateTransfers(transfers)
if err != nil {
    log.Printf("Error creating transfers: %s", err)
    return
}

for _, err := range transferErrors {
    switch err.Index {
    case uint32(TransferExists):
        log.Printf("Batch transfer at %d already exists.", err.Index)
    default:
        log.Printf("Batch transfer at %d failed to create: %s", err.Index, err.Result)
    }
}

Batching

TigerBeetle performance is maximized when you batch API requests. A client instance shared across multiple threads/tasks can automatically batch concurrent requests, but the application must still send as many events as possible in a single call. For example, if you insert 1 million transfers sequentially, one at a time, the insert rate will be a fraction of the potential, because the client will wait for a reply between each one.

batch := []Transfer{}
for i := 0; i < len(batch); i++ {
    transferErrors, err := client.CreateTransfers([]Transfer{batch[i]})
    _, _ = transferErrors, err // Error handling omitted.
}

Instead, always batch as much as you can. The maximum batch size is set in the TigerBeetle server. The default is 8189.

batch := []Transfer{}
BATCH_SIZE := 8189
for i := 0; i < len(batch); i += BATCH_SIZE {
    size := BATCH_SIZE
    if i+BATCH_SIZE > len(batch) {
        size = len(batch) - i
    }
    transferErrors, err := client.CreateTransfers(batch[i : i+size])
    _, _ = transferErrors, err // Error handling omitted.
}

Queues and Workers

If you are making requests to TigerBeetle from workers pulling jobs from a queue, you can batch requests to TigerBeetle by having the worker act on multiple jobs from the queue at once rather than one at a time. i.e. pulling multiple jobs from the queue rather than just one.
Transfer Flags

The transfer flags value is a bitfield. See details for these flags in the Transfers reference.

To toggle behavior for an account, use the types.TransferFlags struct to combine enum values and generate a uint16. Here are a few examples:

    TransferFlags{Linked: true}.ToUint16()
    TransferFlags{Pending: true}.ToUint16()
    TransferFlags{PostPendingTransfer: true}.ToUint16()
    TransferFlags{VoidPendingTransfer: true}.ToUint16()

For example, to link transfer0 and transfer1:

transfer0 := Transfer{
    ID:              ToUint128(4),
    DebitAccountID:  ToUint128(101),
    CreditAccountID: ToUint128(102),
    Amount:          ToUint128(10),
    Ledger:          1,
    Code:            1,
    Flags:           TransferFlags{Linked: true}.ToUint16(),
}
transfer1 := Transfer{
    ID:              ToUint128(5),
    DebitAccountID:  ToUint128(101),
    CreditAccountID: ToUint128(102),
    Amount:          ToUint128(10),
    Ledger:          1,
    Code:            1,
    Flags:           0,
}

transferErrors, err := client.CreateTransfers([]Transfer{transfer0, transfer1})
// Error handling omitted.

Two-Phase Transfers

Two-phase transfers are supported natively by toggling the appropriate flag. TigerBeetle will then adjust the credits_pending and debits_pending fields of the appropriate accounts. A corresponding post pending transfer then needs to be sent to post or void the transfer.
Post a Pending Transfer

With flags set to post_pending_transfer, TigerBeetle will post the transfer. TigerBeetle will atomically roll back the changes to debits_pending and credits_pending of the appropriate accounts and apply them to the debits_posted and credits_posted balances.

transfer0 := Transfer{
    ID:              ToUint128(6),
    DebitAccountID:  ToUint128(101),
    CreditAccountID: ToUint128(102),
    Amount:          ToUint128(10),
    Ledger:          1,
    Code:            1,
    Flags:           0,
}

transferErrors, err := client.CreateTransfers([]Transfer{transfer0})
// Error handling omitted.

transfer1 := Transfer{
    ID: ToUint128(7),
    // Post the entire pending amount.
    Amount:    AmountMax,
    PendingID: ToUint128(6),
    Flags:     TransferFlags{PostPendingTransfer: true}.ToUint16(),
}

transferErrors, err = client.CreateTransfers([]Transfer{transfer1})
// Error handling omitted.

Void a Pending Transfer

In contrast, with flags set to void_pending_transfer, TigerBeetle will void the transfer. TigerBeetle will roll back the changes to debits_pending and credits_pending of the appropriate accounts and not apply them to the debits_posted and credits_posted balances.

transfer0 := Transfer{
    ID:              ToUint128(8),
    DebitAccountID:  ToUint128(101),
    CreditAccountID: ToUint128(102),
    Amount:          ToUint128(10),
    Timeout:         0,
    Ledger:          1,
    Code:            1,
    Flags:           0,
}

transferErrors, err := client.CreateTransfers([]Transfer{transfer0})
// Error handling omitted.

transfer1 := Transfer{
    ID:        ToUint128(9),
    Amount:    ToUint128(0),
    PendingID: ToUint128(8),
    Flags:     TransferFlags{VoidPendingTransfer: true}.ToUint16(),
}

transferErrors, err = client.CreateTransfers([]Transfer{transfer1})
// Error handling omitted.

Transfer Lookup

NOTE: While transfer lookup exists, it is not a flexible query API. We are developing query APIs and there will be new methods for querying transfers in the future.

Transfer lookup is batched, like transfer creation. Pass in all ids to fetch, and matched transfers are returned.

If no transfer matches an id, no object is returned for that transfer. So the order of transfers in the response is not necessarily the same as the order of ids in the request. You can refer to the id field in the response to distinguish transfers.

transfers, err := client.LookupTransfers([]Uint128{ToUint128(1), ToUint128(2)})

Get Account Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the transfers involving a given account, allowing basic filter and pagination capabilities.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

filter := AccountFilter{
    AccountID:    ToUint128(2),
    UserData128:  ToUint128(0), // No filter by UserData.
    UserData64:   0,
    UserData32:   0,
    Code:         0,  // No filter by Code.
    TimestampMin: 0,  // No filter by Timestamp.
    TimestampMax: 0,  // No filter by Timestamp.
    Limit:        10, // Limit to ten transfers at most.
    Flags: AccountFilterFlags{
        Debits:   true, // Include transfer from the debit side.
        Credits:  true, // Include transfer from the credit side.
        Reversed: true, // Sort by timestamp in reverse-chronological order.
    }.ToUint32(),
}

transfers, err := client.GetAccountTransfers(filter)

Get Account Balances

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the point-in-time balances of a given account, allowing basic filter and pagination capabilities.

Only accounts created with the flag history set retain historical balances.

The balances in the response are sorted by timestamp in chronological or reverse-chronological order.

filter := AccountFilter{
    AccountID:    ToUint128(2),
    UserData128:  ToUint128(0), // No filter by UserData.
    UserData64:   0,
    UserData32:   0,
    Code:         0,  // No filter by Code.
    TimestampMin: 0,  // No filter by Timestamp.
    TimestampMax: 0,  // No filter by Timestamp.
    Limit:        10, // Limit to ten balances at most.
    Flags: AccountFilterFlags{
        Debits:   true, // Include transfer from the debit side.
        Credits:  true, // Include transfer from the credit side.
        Reversed: true, // Sort by timestamp in reverse-chronological order.
    }.ToUint32(),
}

account_balances, err := client.GetAccountBalances(filter)

Query Accounts

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query accounts by the intersection of some fields and by timestamp range.

The accounts in the response are sorted by timestamp in chronological or reverse-chronological order.

filter := QueryFilter{
    UserData128:  ToUint128(1000), // Filter by UserData
    UserData64:   100,
    UserData32:   10,
    Code:         1,  // Filter by Code
    Ledger:       0,  // No filter by Ledger
    TimestampMin: 0,  // No filter by Timestamp.
    TimestampMax: 0,  // No filter by Timestamp.
    Limit:        10, // Limit to ten accounts at most.
    Flags: QueryFilterFlags{
        Reversed: true, // Sort by timestamp in reverse-chronological order.
    }.ToUint32(),
}

accounts, err := client.QueryAccounts(filter)

Query Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query transfers by the intersection of some fields and by timestamp range.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

filter := QueryFilter{
    UserData128:  ToUint128(1000), // Filter by UserData.
    UserData64:   100,
    UserData32:   10,
    Code:         1,  // Filter by Code.
    Ledger:       0,  // No filter by Ledger.
    TimestampMin: 0,  // No filter by Timestamp.
    TimestampMax: 0,  // No filter by Timestamp.
    Limit:        10, // Limit to ten transfers at most.
    Flags: QueryFilterFlags{
        Reversed: true, // Sort by timestamp in reverse-chronological order.
    }.ToUint32(),
}

transfers, err := client.QueryTransfers(filter)

Linked Events

When the linked flag is specified for an account when creating accounts or a transfer when creating transfers, it links that event with the next event in the batch, to create a chain of events, of arbitrary length, which all succeed or fail together. The tail of a chain is denoted by the first event without this flag. The last event in a batch may therefore never have the linked flag set as this would leave a chain open-ended. Multiple chains or individual events may coexist within a batch to succeed or fail independently.

Events within a chain are executed within order, or are rolled back on error, so that the effect of each event in the chain is visible to the next, and so that the chain is either visible or invisible as a unit to subsequent events after the chain. The event that was the first to break the chain will have a unique error result. Other events in the chain will have their error result set to linked_event_failed.

batch := []Transfer{}
linkedFlag := TransferFlags{Linked: true}.ToUint16()

// An individual transfer (successful):
batch = append(batch, Transfer{ID: ToUint128(1) /* ... rest of transfer ... */})

// A chain of 4 transfers (the last transfer in the chain closes the chain with linked=false):
batch = append(batch, Transfer{ID: ToUint128(2) /* ... , */, Flags: linkedFlag}) // Commit/rollback.
batch = append(batch, Transfer{ID: ToUint128(3) /* ... , */, Flags: linkedFlag}) // Commit/rollback.
batch = append(batch, Transfer{ID: ToUint128(2) /* ... , */, Flags: linkedFlag}) // Fail with exists
batch = append(batch, Transfer{ID: ToUint128(4) /* ... , */})                    // Fail without committing

// An individual transfer (successful):
// This should not see any effect from the failed chain above.
batch = append(batch, Transfer{ID: ToUint128(2) /* ... rest of transfer ... */})

// A chain of 2 transfers (the first transfer fails the chain):
batch = append(batch, Transfer{ID: ToUint128(2) /* ... rest of transfer ... */, Flags: linkedFlag})
batch = append(batch, Transfer{ID: ToUint128(3) /* ... rest of transfer ... */})

// A chain of 2 transfers (successful):
batch = append(batch, Transfer{ID: ToUint128(3) /* ... rest of transfer ... */, Flags: linkedFlag})
batch = append(batch, Transfer{ID: ToUint128(4) /* ... rest of transfer ... */})

transferErrors, err := client.CreateTransfers(batch)
// Error handling omitted.

Imported Events

When the imported flag is specified for an account when creating accounts or a transfer when creating transfers, it allows importing historical events with a user-defined timestamp.

The entire batch of events must be set with the flag imported.

It’s recommended to submit the whole batch as a linked chain of events, ensuring that if any event fails, none of them are committed, preserving the last timestamp unchanged. This approach gives the application a chance to correct failed imported events, re-submitting the batch again with the same user-defined timestamps.

// External source of time.
var historicalTimestamp uint64 = 0
historicalAccounts := []Account{ /* Loaded from an external source. */ }
historicalTransfers := []Transfer{ /* Loaded from an external source. */ }

// First, load and import all accounts with their timestamps from the historical source.
accountsBatch := []Account{}
for index, account := range historicalAccounts {
    // Set a unique and strictly increasing timestamp.
    historicalTimestamp += 1
    account.Timestamp = historicalTimestamp

    account.Flags = AccountFlags{
        // Set the account as `imported`.
        Imported: true,
        // To ensure atomicity, the entire batch (except the last event in the chain)
        // must be `linked`.
        Linked: index < len(historicalAccounts)-1,
    }.ToUint16()

    accountsBatch = append(accountsBatch, account)
}

accountErrors, err := client.CreateAccounts(accountsBatch)
// Error handling omitted.

// Then, load and import all transfers with their timestamps from the historical source.
transfersBatch := []Transfer{}
for index, transfer := range historicalTransfers {
    // Set a unique and strictly increasing timestamp.
    historicalTimestamp += 1
    transfer.Timestamp = historicalTimestamp

    transfer.Flags = TransferFlags{
        // Set the transfer as `imported`.
        Imported: true,
        // To ensure atomicity, the entire batch (except the last event in the chain)
        // must be `linked`.
        Linked: index < len(historicalAccounts)-1,
    }.ToUint16()

    transfersBatch = append(transfersBatch, transfer)
}

transferErrors, err := client.CreateTransfers(transfersBatch)
// Error handling omitted..
// Since it is a linked chain, in case of any error the entire batch is rolled back and can be retried
// with the same historical timestamps without regressing the cluster timestamp.

Timeouts And Cancellation

The Client retries indefinitely and doesn’t impose any per-request timeout. Cancellation is provided as a mechanism, and the specific cancellation policy is left to the application. A Client instance can be closed at any time. On close, all in-flight requests are canceled and return an error to the caller. Even if an error is returned, a request might still be processed by the TigerBeetle server. Reliable transaction submission explains how to make transfers retry-proof using IDs for end-to-end idempotency.
Edit this page
tigerbeetle-java

The TigerBeetle client for Java.

javadoc

maven-central
Prerequisites

Linux >= 5.6 is the only production environment we support. But for ease of development we also support macOS and Windows.

    Java >= 11
    Maven >= 3.6 (not strictly necessary but it’s what our guides assume)

Setup

First, create a directory for your project and cd into the directory.

Then create pom.xml and copy this into it:

<project xmlns="http://maven.apache.org/POM/4.0.0"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xsi:schemaLocation="http://maven.apache.org/POM/4.0.0 https://maven.apache.org/xsd/maven-4.0.0.xsd">
  <modelVersion>4.0.0</modelVersion>

  <groupId>com.tigerbeetle</groupId>
  <artifactId>samples</artifactId>
  <version>1.0-SNAPSHOT</version>

  <properties>
    <maven.compiler.source>11</maven.compiler.source>
    <maven.compiler.target>11</maven.compiler.target>
  </properties>

  <build>
    <plugins>
      <plugin>
        <groupId>org.apache.maven.plugins</groupId>
        <artifactId>maven-compiler-plugin</artifactId>
        <version>3.8.1</version>
        <configuration>
          <compilerArgs>
            <arg>-Xlint:all,-options,-path</arg>
          </compilerArgs>
        </configuration>
      </plugin>

      <plugin>
        <groupId>org.codehaus.mojo</groupId>
        <artifactId>exec-maven-plugin</artifactId>
        <version>1.6.0</version>
        <configuration>
          <mainClass>com.tigerbeetle.samples.Main</mainClass>
        </configuration>
      </plugin>
    </plugins>
  </build>

  <dependencies>
    <dependency>
      <groupId>com.tigerbeetle</groupId>
      <artifactId>tigerbeetle-java</artifactId>
      <!-- Grab the latest commit from: https://repo1.maven.org/maven2/com/tigerbeetle/tigerbeetle-java/maven-metadata.xml -->
      <version>0.0.1-3431</version>
    </dependency>
  </dependencies>
</project>

Then, install the TigerBeetle client:

mvn install

Now, create src/main/java/Main.java and copy this into it:

import com.tigerbeetle.*;

public final class Main {
    public static void main(String[] args) throws Exception {
        System.out.println("Import ok!");
    }
}

Finally, build and run:

mvn exec:java

Now that all prerequisites and dependencies are correctly set up, let’s dig into using TigerBeetle.
Sample projects

This document is primarily a reference guide to the client. Below are various sample projects demonstrating features of TigerBeetle.

    Basic: Create two accounts and transfer an amount between them.
    Two-Phase Transfer: Create two accounts and start a pending transfer between them, then post the transfer.
    Many Two-Phase Transfers: Create two accounts and start a number of pending transfers between them, posting and voiding alternating transfers.

Creating a Client

A client is created with a cluster ID and replica addresses for all replicas in the cluster. The cluster ID and replica addresses are both chosen by the system that starts the TigerBeetle cluster.

Clients are thread-safe and a single instance should be shared between multiple concurrent tasks. This allows events to be automatically batched.

Multiple clients are useful when connecting to more than one TigerBeetle cluster.

In this example the cluster ID is 0 and there is one replica. The address is read from the TB_ADDRESS environment variable and defaults to port 3000.

String replicaAddress = System.getenv("TB_ADDRESS");
byte[] clusterID = UInt128.asBytes(0);
String[] replicaAddresses = new String[] {replicaAddress == null ? "3000" : replicaAddress};
try (var client = new Client(clusterID, replicaAddresses)) {
    // Use client
}

The following are valid addresses:

    3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1:3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1 (interpreted as 127.0.0.1:3001, 3001 is the default port)

Creating Accounts

See details for account fields in the Accounts reference.

AccountBatch accounts = new AccountBatch(1);
accounts.add();
accounts.setId(UInt128.id()); // TigerBeetle time-based ID.
accounts.setUserData128(0, 0);
accounts.setUserData64(0);
accounts.setUserData32(0);
accounts.setLedger(1);
accounts.setCode(718);
accounts.setFlags(AccountFlags.NONE);
accounts.setTimestamp(0);

CreateAccountResultBatch accountErrors = client.createAccounts(accounts);
// Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.

The 128-bit fields like id and user_data_128 have a few overrides to make it easier to integrate. You can either pass in a long, a pair of longs (least and most significant bits), or a byte[].

There is also a com.tigerbeetle.UInt128 helper with static methods for converting 128-bit little-endian unsigned integers between instances of long, java.util.UUID, java.math.BigInteger and byte[].

The fields for transfer amounts and account balances are also 128-bit, but they are always represented as a java.math.BigInteger.
Account Flags

The account flags value is a bitfield. See details for these flags in the Accounts reference.

To toggle behavior for an account, combine enum values stored in the AccountFlags object with bitwise-or:

    AccountFlags.LINKED
    AccountFlags.DEBITS_MUST_NOT_EXCEED_CREDITS
    AccountFlags.CREDITS_MUST_NOT_EXCEED_CREDITS
    AccountFlags.HISTORY

For example, to link two accounts where the first account additionally has the debits_must_not_exceed_credits constraint:

AccountBatch accounts = new AccountBatch(2);

accounts.add();
accounts.setId(100);
accounts.setLedger(1);
accounts.setCode(718);
accounts.setFlags(AccountFlags.LINKED | AccountFlags.DEBITS_MUST_NOT_EXCEED_CREDITS);

accounts.add();
accounts.setId(101);
accounts.setLedger(1);
accounts.setCode(718);
accounts.setFlags(AccountFlags.HISTORY);

CreateAccountResultBatch accountErrors = client.createAccounts(accounts);
// Error handling omitted.

Response and Errors

The response is an empty array if all accounts were created successfully. If the response is non-empty, each object in the response array contains error information for an account that failed. The error object contains an error code and the index of the account in the request batch.

See all error conditions in the create_accounts reference.

AccountBatch accounts = new AccountBatch(3);

accounts.add();
accounts.setId(102);
accounts.setLedger(1);
accounts.setCode(718);
accounts.setFlags(AccountFlags.NONE);

accounts.add();
accounts.setId(103);
accounts.setLedger(1);
accounts.setCode(718);
accounts.setFlags(AccountFlags.NONE);

accounts.add();
accounts.setId(104);
accounts.setLedger(1);
accounts.setCode(718);
accounts.setFlags(AccountFlags.NONE);

CreateAccountResultBatch accountErrors = client.createAccounts(accounts);
while (accountErrors.next()) {
    switch (accountErrors.getResult()) {
        case Exists:
            System.err.printf("Batch account at %d already exists.\n",
                    accountErrors.getIndex());
            break;

        default:
            System.err.printf("Batch account at %d failed to create %s.\n",
                    accountErrors.getIndex(), accountErrors.getResult());
            break;
    }
}

Account Lookup

Account lookup is batched, like account creation. Pass in all IDs to fetch. The account for each matched ID is returned.

If no account matches an ID, no object is returned for that account. So the order of accounts in the response is not necessarily the same as the order of IDs in the request. You can refer to the ID field in the response to distinguish accounts.

IdBatch ids = new IdBatch(2);
ids.add(100);
ids.add(101);

AccountBatch accounts = client.lookupAccounts(ids);

Create Transfers

This creates a journal entry between two accounts.

See details for transfer fields in the Transfers reference.

TransferBatch transfers = new TransferBatch(1);

transfers.add();
transfers.setId(UInt128.id());
transfers.setDebitAccountId(102);
transfers.setCreditAccountId(103);
transfers.setAmount(10);
transfers.setUserData128(0, 0);
transfers.setUserData64(0);
transfers.setUserData32(0);
transfers.setTimeout(0);
transfers.setLedger(1);
transfers.setCode(1);
transfers.setFlags(TransferFlags.NONE);
transfers.setTimeout(0);

CreateTransferResultBatch transferErrors = client.createTransfers(transfers);
// Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.
Response and Errors

The response is an empty array if all transfers were created successfully. If the response is non-empty, each object in the response array contains error information for a transfer that failed. The error object contains an error code and the index of the transfer in the request batch.

See all error conditions in the create_transfers reference.

TransferBatch transfers = new TransferBatch(3);

transfers.add();
transfers.setId(1);
transfers.setDebitAccountId(102);
transfers.setCreditAccountId(103);
transfers.setAmount(10);
transfers.setLedger(1);
transfers.setCode(1);

transfers.add();
transfers.setId(2);
transfers.setDebitAccountId(102);
transfers.setCreditAccountId(103);
transfers.setAmount(10);
transfers.setLedger(1);
transfers.setCode(1);

transfers.add();
transfers.setId(3);
transfers.setDebitAccountId(102);
transfers.setCreditAccountId(103);
transfers.setAmount(10);
transfers.setLedger(1);
transfers.setCode(1);

CreateTransferResultBatch transferErrors = client.createTransfers(transfers);
while (transferErrors.next()) {
    switch (transferErrors.getResult()) {
        case ExceedsCredits:
            System.err.printf("Batch transfer at %d already exists.\n",
                    transferErrors.getIndex());
            break;

        default:
            System.err.printf("Batch transfer at %d failed to create: %s\n",
                    transferErrors.getIndex(), transferErrors.getResult());
            break;
    }
}

Batching

TigerBeetle performance is maximized when you batch API requests. A client instance shared across multiple threads/tasks can automatically batch concurrent requests, but the application must still send as many events as possible in a single call. For example, if you insert 1 million transfers sequentially, one at a time, the insert rate will be a fraction of the potential, because the client will wait for a reply between each one.

ResultSet dataSource = null; /* Loaded from an external source. */;
while(dataSource.next()) {
    TransferBatch batch = new TransferBatch(1);

    batch.add();
    batch.setId(dataSource.getBytes("id"));
    batch.setDebitAccountId(dataSource.getBytes("debit_account_id"));
    batch.setCreditAccountId(dataSource.getBytes("credit_account_id"));
    batch.setAmount(dataSource.getBigDecimal("amount").toBigInteger());
    batch.setLedger(dataSource.getInt("ledger"));
    batch.setCode(dataSource.getInt("code"));

    CreateTransferResultBatch transferErrors = client.createTransfers(batch);
    // Error handling omitted.
}

Instead, always batch as much as you can. The maximum batch size is set in the TigerBeetle server. The default is 8189.

ResultSet dataSource = null; /* Loaded from an external source. */;

var BATCH_SIZE = 8189;
TransferBatch batch = new TransferBatch(BATCH_SIZE);
while(dataSource.next()) {
    batch.add();
    batch.setId(dataSource.getBytes("id"));
    batch.setDebitAccountId(dataSource.getBytes("debit_account_id"));
    batch.setCreditAccountId(dataSource.getBytes("credit_account_id"));
    batch.setAmount(dataSource.getBigDecimal("amount").toBigInteger());
    batch.setLedger(dataSource.getInt("ledger"));
    batch.setCode(dataSource.getInt("code"));

    if (batch.getLength() == BATCH_SIZE) {
        CreateTransferResultBatch transferErrors = client.createTransfers(batch);
        // Error handling omitted.

        // Reset the batch for the next iteration.
        batch.beforeFirst();
    }
}

if (batch.getLength() > 0) {
    // Send the remaining items.
    CreateTransferResultBatch transferErrors = client.createTransfers(batch);
    // Error handling omitted.
}

Queues and Workers

If you are making requests to TigerBeetle from workers pulling jobs from a queue, you can batch requests to TigerBeetle by having the worker act on multiple jobs from the queue at once rather than one at a time. i.e. pulling multiple jobs from the queue rather than just one.
Transfer Flags

The transfer flags value is a bitfield. See details for these flags in the Transfers reference.

To toggle behavior for an account, combine enum values stored in the TransferFlags object with bitwise-or:

    TransferFlags.NONE
    TransferFlags.LINKED
    TransferFlags.PENDING
    TransferFlags.POST_PENDING_TRANSFER
    TransferFlags.VOID_PENDING_TRANSFER

For example, to link transfer0 and transfer1:

TransferBatch transfers = new TransferBatch(2);

// First transfer
transfers.add();
transfers.setId(4);
transfers.setDebitAccountId(102);
transfers.setCreditAccountId(103);
transfers.setAmount(10);
transfers.setLedger(1);
transfers.setCode(1);
transfers.setFlags(TransferFlags.LINKED);

transfers.add();
transfers.setId(5);
transfers.setDebitAccountId(102);
transfers.setCreditAccountId(103);
transfers.setAmount(10);
transfers.setLedger(1);
transfers.setCode(1);
transfers.setFlags(TransferFlags.NONE);

CreateTransferResultBatch transferErrors = client.createTransfers(transfers);
// Error handling omitted.

Two-Phase Transfers

Two-phase transfers are supported natively by toggling the appropriate flag. TigerBeetle will then adjust the credits_pending and debits_pending fields of the appropriate accounts. A corresponding post pending transfer then needs to be sent to post or void the transfer.
Post a Pending Transfer

With flags set to post_pending_transfer, TigerBeetle will post the transfer. TigerBeetle will atomically roll back the changes to debits_pending and credits_pending of the appropriate accounts and apply them to the debits_posted and credits_posted balances.

TransferBatch transfers = new TransferBatch(1);

transfers.add();
transfers.setId(6);
transfers.setDebitAccountId(102);
transfers.setCreditAccountId(103);
transfers.setAmount(10);
transfers.setLedger(1);
transfers.setCode(1);
transfers.setFlags(TransferFlags.PENDING);

CreateTransferResultBatch transferErrors = client.createTransfers(transfers);
// Error handling omitted.

transfers = new TransferBatch(1);

transfers.add();
transfers.setId(7);
transfers.setAmount(TransferBatch.AMOUNT_MAX);
transfers.setPendingId(6);
transfers.setFlags(TransferFlags.POST_PENDING_TRANSFER);

transferErrors = client.createTransfers(transfers);
// Error handling omitted.

Void a Pending Transfer

In contrast, with flags set to void_pending_transfer, TigerBeetle will void the transfer. TigerBeetle will roll back the changes to debits_pending and credits_pending of the appropriate accounts and not apply them to the debits_posted and credits_posted balances.

TransferBatch transfers = new TransferBatch(1);

transfers.add();
transfers.setId(8);
transfers.setDebitAccountId(102);
transfers.setCreditAccountId(103);
transfers.setAmount(10);
transfers.setLedger(1);
transfers.setCode(1);
transfers.setFlags(TransferFlags.PENDING);

CreateTransferResultBatch transferErrors = client.createTransfers(transfers);
// Error handling omitted.

transfers = new TransferBatch(1);

transfers.add();
transfers.setId(9);
transfers.setAmount(0);
transfers.setPendingId(8);
transfers.setFlags(TransferFlags.VOID_PENDING_TRANSFER);

transferErrors = client.createTransfers(transfers);
// Error handling omitted.

Transfer Lookup

NOTE: While transfer lookup exists, it is not a flexible query API. We are developing query APIs and there will be new methods for querying transfers in the future.

Transfer lookup is batched, like transfer creation. Pass in all ids to fetch, and matched transfers are returned.

If no transfer matches an id, no object is returned for that transfer. So the order of transfers in the response is not necessarily the same as the order of ids in the request. You can refer to the id field in the response to distinguish transfers.

IdBatch ids = new IdBatch(2);
ids.add(1);
ids.add(2);

TransferBatch transfers = client.lookupTransfers(ids);

Get Account Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the transfers involving a given account, allowing basic filter and pagination capabilities.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

AccountFilter filter = new AccountFilter();
filter.setAccountId(2);
filter.setUserData128(0); // No filter by UserData.
filter.setUserData64(0);
filter.setUserData32(0);
filter.setCode(0); // No filter by Code.
filter.setTimestampMin(0); // No filter by Timestamp.
filter.setTimestampMax(0); // No filter by Timestamp.
filter.setLimit(10); // Limit to ten transfers at most.
filter.setDebits(true); // Include transfer from the debit side.
filter.setCredits(true); // Include transfer from the credit side.
filter.setReversed(true); // Sort by timestamp in reverse-chronological order.

TransferBatch transfers = client.getAccountTransfers(filter);

Get Account Balances

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the point-in-time balances of a given account, allowing basic filter and pagination capabilities.

Only accounts created with the flag history set retain historical balances.

The balances in the response are sorted by timestamp in chronological or reverse-chronological order.

AccountFilter filter = new AccountFilter();
filter.setAccountId(2);
filter.setUserData128(0); // No filter by UserData.
filter.setUserData64(0);
filter.setUserData32(0);
filter.setCode(0); // No filter by Code.
filter.setTimestampMin(0); // No filter by Timestamp.
filter.setTimestampMax(0); // No filter by Timestamp.
filter.setLimit(10); // Limit to ten balances at most.
filter.setDebits(true); // Include transfer from the debit side.
filter.setCredits(true); // Include transfer from the credit side.
filter.setReversed(true); // Sort by timestamp in reverse-chronological order.

AccountBalanceBatch account_balances = client.getAccountBalances(filter);

Query Accounts

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query accounts by the intersection of some fields and by timestamp range.

The accounts in the response are sorted by timestamp in chronological or reverse-chronological order.

QueryFilter filter = new QueryFilter();
filter.setUserData128(1000); // Filter by UserData.
filter.setUserData64(100);
filter.setUserData32(10);
filter.setCode(1); // Filter by Code.
filter.setLedger(0); // No filter by Ledger.
filter.setTimestampMin(0); // No filter by Timestamp.
filter.setTimestampMax(0); // No filter by Timestamp.
filter.setLimit(10); // Limit to ten accounts at most.
filter.setReversed(true); // Sort by timestamp in reverse-chronological order.

AccountBatch accounts = client.queryAccounts(filter);

Query Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query transfers by the intersection of some fields and by timestamp range.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

QueryFilter filter = new QueryFilter();
filter.setUserData128(1000); // Filter by UserData.
filter.setUserData64(100);
filter.setUserData32(10);
filter.setCode(1); // Filter by Code.
filter.setLedger(0); // No filter by Ledger.
filter.setTimestampMin(0); // No filter by Timestamp.
filter.setTimestampMax(0); // No filter by Timestamp.
filter.setLimit(10); // Limit to ten transfers at most.
filter.setReversed(true); // Sort by timestamp in reverse-chronological order.

TransferBatch transfers = client.queryTransfers(filter);

Linked Events

When the linked flag is specified for an account when creating accounts or a transfer when creating transfers, it links that event with the next event in the batch, to create a chain of events, of arbitrary length, which all succeed or fail together. The tail of a chain is denoted by the first event without this flag. The last event in a batch may therefore never have the linked flag set as this would leave a chain open-ended. Multiple chains or individual events may coexist within a batch to succeed or fail independently.

Events within a chain are executed within order, or are rolled back on error, so that the effect of each event in the chain is visible to the next, and so that the chain is either visible or invisible as a unit to subsequent events after the chain. The event that was the first to break the chain will have a unique error result. Other events in the chain will have their error result set to linked_event_failed.

TransferBatch transfers = new TransferBatch(10);

// An individual transfer (successful):
transfers.add();
transfers.setId(1);
// ... rest of transfer ...
transfers.setFlags(TransferFlags.NONE);

// A chain of 4 transfers (the last transfer in the chain closes the chain with
// linked=false):
transfers.add();
transfers.setId(2); // Commit/rollback.
// ... rest of transfer ...
transfers.setFlags(TransferFlags.LINKED);
transfers.add();
transfers.setId(3); // Commit/rollback.
// ... rest of transfer ...
transfers.setFlags(TransferFlags.LINKED);
transfers.add();
transfers.setId(2); // Fail with exists
// ... rest of transfer ...
transfers.setFlags(TransferFlags.LINKED);
transfers.add();
transfers.setId(4); // Fail without committing
// ... rest of transfer ...
transfers.setFlags(TransferFlags.NONE);

// An individual transfer (successful):
// This should not see any effect from the failed chain above.
transfers.add();
transfers.setId(2);
// ... rest of transfer ...
transfers.setFlags(TransferFlags.NONE);

// A chain of 2 transfers (the first transfer fails the chain):
transfers.add();
transfers.setId(2);
// ... rest of transfer ...
transfers.setFlags(TransferFlags.LINKED);
transfers.add();
transfers.setId(3);
// ... rest of transfer ...
transfers.setFlags(TransferFlags.NONE);
// A chain of 2 transfers (successful):
transfers.add();
transfers.setId(3);
// ... rest of transfer ...
transfers.setFlags(TransferFlags.LINKED);
transfers.add();
transfers.setId(4);
// ... rest of transfer ...
transfers.setFlags(TransferFlags.NONE);

CreateTransferResultBatch transferErrors = client.createTransfers(transfers);
// Error handling omitted.

Imported Events

When the imported flag is specified for an account when creating accounts or a transfer when creating transfers, it allows importing historical events with a user-defined timestamp.

The entire batch of events must be set with the flag imported.

It’s recommended to submit the whole batch as a linked chain of events, ensuring that if any event fails, none of them are committed, preserving the last timestamp unchanged. This approach gives the application a chance to correct failed imported events, re-submitting the batch again with the same user-defined timestamps.

// External source of time
long historicalTimestamp = 0L;
ResultSet historicalAccounts = null; // Loaded from an external source;
ResultSet historicalTransfers = null ; // Loaded from an external source.

var BATCH_SIZE = 8189;

// First, load and import all accounts with their timestamps from the historical source.
AccountBatch accounts = new AccountBatch(BATCH_SIZE);
while (historicalAccounts.next()) {
    // Set a unique and strictly increasing timestamp.
    historicalTimestamp += 1;

    accounts.add();
    accounts.setId(historicalAccounts.getBytes("id"));
    accounts.setLedger(historicalAccounts.getInt("ledger"));
    accounts.setCode(historicalAccounts.getInt("code"));
    accounts.setTimestamp(historicalTimestamp);

    // Set the account as `imported`.
    // To ensure atomicity, the entire batch (except the last event in the chain)
    // must be `linked`.
    if (accounts.getLength() < BATCH_SIZE) {
        accounts.setFlags(AccountFlags.IMPORTED | AccountFlags.LINKED);
    } else {
        accounts.setFlags(AccountFlags.IMPORTED);

        CreateAccountResultBatch accountsErrors = client.createAccounts(accounts);
        // Error handling omitted.

        // Reset the batch for the next iteration.
        accounts.beforeFirst();
    }
}

if (accounts.getLength() > 0) {
    // Send the remaining items.
    CreateAccountResultBatch accountsErrors = client.createAccounts(accounts);
    // Error handling omitted.
}

// Then, load and import all transfers with their timestamps from the historical source.
TransferBatch transfers = new TransferBatch(BATCH_SIZE);
while (historicalTransfers.next()) {
    // Set a unique and strictly increasing timestamp.
    historicalTimestamp += 1;

    transfers.add();
    transfers.setId(historicalTransfers.getBytes("id"));
    transfers.setDebitAccountId(historicalTransfers.getBytes("debit_account_id"));
    transfers.setCreditAccountId(historicalTransfers.getBytes("credit_account_id"));
    transfers.setAmount(historicalTransfers.getBigDecimal("amount").toBigInteger());
    transfers.setLedger(historicalTransfers.getInt("ledger"));
    transfers.setCode(historicalTransfers.getInt("code"));
    transfers.setTimestamp(historicalTimestamp);

    // Set the transfer as `imported`.
    // To ensure atomicity, the entire batch (except the last event in the chain)
    // must be `linked`.
    if (transfers.getLength() < BATCH_SIZE) {
        transfers.setFlags(TransferFlags.IMPORTED | TransferFlags.LINKED);
    } else {
        transfers.setFlags(TransferFlags.IMPORTED);

        CreateTransferResultBatch transferErrors = client.createTransfers(transfers);
        // Error handling omitted.

        // Reset the batch for the next iteration.
        transfers.beforeFirst();
    }
}

if (transfers.getLength() > 0) {
    // Send the remaining items.
    CreateTransferResultBatch transferErrors = client.createTransfers(transfers);
    // Error handling omitted.
}

// Since it is a linked chain, in case of any error the entire batch is rolled back and can be retried
// with the same historical timestamps without regressing the cluster timestamp.

Timeouts And Cancellation

The Client retries indefinitely and doesn’t impose any per-request timeout. Cancellation is provided as a mechanism, and the specific cancellation policy is left to the application. A Client instance can be closed at any time. On close, all in-flight requests are canceled and return an error to the caller. Even if an error is returned, a request might still be processed by the TigerBeetle server. Reliable transaction submission explains how to make transfers retry-proof using IDs for end-to-end idempotency.
Edit this page
tigerbeetle-node

The TigerBeetle client for Node.js.
Prerequisites

Linux >= 5.6 is the only production environment we support. But for ease of development we also support macOS and Windows.

    Node.js >= 18

Setup

First, create a directory for your project and cd into the directory.

Then, install the TigerBeetle client:

npm install --save-exact tigerbeetle-node

Now, create main.js and copy this into it:

const { id } = require("tigerbeetle-node");
const { createClient } = require("tigerbeetle-node");
const process = require("process");

console.log("Import ok!");

Finally, build and run:

node main.js

Now that all prerequisites and dependencies are correctly set up, let’s dig into using TigerBeetle.
Sample projects

This document is primarily a reference guide to the client. Below are various sample projects demonstrating features of TigerBeetle.

    Basic: Create two accounts and transfer an amount between them.
    Two-Phase Transfer: Create two accounts and start a pending transfer between them, then post the transfer.
    Many Two-Phase Transfers: Create two accounts and start a number of pending transfers between them, posting and voiding alternating transfers.

Sidenote: BigInt

TigerBeetle uses 64-bit integers for many fields while JavaScript’s builtin Number maximum value is 2^53-1. The n suffix in JavaScript means the value is a BigInt. This is useful for literal numbers. If you already have a Number variable though, you can call the BigInt constructor to get a BigInt from it. For example, 1n is the same as BigInt(1).
Creating a Client

A client is created with a cluster ID and replica addresses for all replicas in the cluster. The cluster ID and replica addresses are both chosen by the system that starts the TigerBeetle cluster.

Clients are thread-safe and a single instance should be shared between multiple concurrent tasks. This allows events to be automatically batched.

Multiple clients are useful when connecting to more than one TigerBeetle cluster.

In this example the cluster ID is 0 and there is one replica. The address is read from the TB_ADDRESS environment variable and defaults to port 3000.

const client = createClient({
  cluster_id: 0n,
  replica_addresses: [process.env.TB_ADDRESS || "3000"],
});

The following are valid addresses:

    3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1:3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1 (interpreted as 127.0.0.1:3001, 3001 is the default port)

Creating Accounts

See details for account fields in the Accounts reference.

const account = {
  id: id(), // TigerBeetle time-based ID.
  debits_pending: 0n,
  debits_posted: 0n,
  credits_pending: 0n,
  credits_posted: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  reserved: 0,
  ledger: 1,
  code: 718,
  flags: 0,
  timestamp: 0n,
};

const account_errors = await client.createAccounts([account]);
// Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.
Account Flags

The account flags value is a bitfield. See details for these flags in the Accounts reference.

To toggle behavior for an account, combine enum values stored in the AccountFlags object (in TypeScript it is an actual enum) with bitwise-or:

    AccountFlags.linked
    AccountFlags.debits_must_not_exceed_credits
    AccountFlags.credits_must_not_exceed_credits
    AccountFlags.history

For example, to link two accounts where the first account additionally has the debits_must_not_exceed_credits constraint:

const account0 = {
  id: 100n,
  debits_pending: 0n,
  debits_posted: 0n,
  credits_pending: 0n,
  credits_posted: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  reserved: 0,
  ledger: 1,
  code: 1,
  timestamp: 0n,
  flags: AccountFlags.linked | AccountFlags.debits_must_not_exceed_credits,
};
const account1 = {
  id: 101n,
  debits_pending: 0n,
  debits_posted: 0n,
  credits_pending: 0n,
  credits_posted: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  reserved: 0,
  ledger: 1,
  code: 1,
  timestamp: 0n,
  flags: AccountFlags.history,
};

const account_errors = await client.createAccounts([account0, account1]);
// Error handling omitted.

Response and Errors

The response is an empty array if all accounts were created successfully. If the response is non-empty, each object in the response array contains error information for an account that failed. The error object contains an error code and the index of the account in the request batch.

See all error conditions in the create_accounts reference.

const account0 = {
  id: 102n,
  debits_pending: 0n,
  debits_posted: 0n,
  credits_pending: 0n,
  credits_posted: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  reserved: 0,
  ledger: 1,
  code: 1,
  timestamp: 0n,
  flags: 0,
};
const account1 = {
  id: 103n,
  debits_pending: 0n,
  debits_posted: 0n,
  credits_pending: 0n,
  credits_posted: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  reserved: 0,
  ledger: 1,
  code: 1,
  timestamp: 0n,
  flags: 0,
};
const account2 = {
  id: 104n,
  debits_pending: 0n,
  debits_posted: 0n,
  credits_pending: 0n,
  credits_posted: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  reserved: 0,
  ledger: 1,
  code: 1,
  timestamp: 0n,
  flags: 0,
};

const account_errors = await client.createAccounts([account0, account1, account2]);
for (const error of account_errors) {
  switch (error.result) {
    case CreateAccountError.exists:
      console.error(`Batch account at ${error.index} already exists.`);
      break;
    default:
      console.error(
        `Batch account at ${error.index} failed to create: ${
          CreateAccountError[error.result]
        }.`,
      );
  }
}

To handle errors you can either 1) exactly match error codes returned from client.createAccounts with enum values in the CreateAccountError object, or you can 2) look up the error code in the CreateAccountError object for a human-readable string.
Account Lookup

Account lookup is batched, like account creation. Pass in all IDs to fetch. The account for each matched ID is returned.

If no account matches an ID, no object is returned for that account. So the order of accounts in the response is not necessarily the same as the order of IDs in the request. You can refer to the ID field in the response to distinguish accounts.

const accounts = await client.lookupAccounts([100n, 101n]);

Create Transfers

This creates a journal entry between two accounts.

See details for transfer fields in the Transfers reference.

const transfers = [{
  id: id(), // TigerBeetle time-based ID.
  debit_account_id: 102n,
  credit_account_id: 103n,
  amount: 10n,
  pending_id: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  timeout: 0,
  ledger: 1,
  code: 720,
  flags: 0,
  timestamp: 0n,
}];

const transfer_errors = await client.createTransfers(transfers);
// Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.
Response and Errors

The response is an empty array if all transfers were created successfully. If the response is non-empty, each object in the response array contains error information for a transfer that failed. The error object contains an error code and the index of the transfer in the request batch.

See all error conditions in the create_transfers reference.

const transfers = [{
  id: 1n,
  debit_account_id: 102n,
  credit_account_id: 103n,
  amount: 10n,
  pending_id: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  timeout: 0,
  ledger: 1,
  code: 720,
  flags: 0,
  timestamp: 0n,
},
{
  id: 2n,
  debit_account_id: 102n,
  credit_account_id: 103n,
  amount: 10n,
  pending_id: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  timeout: 0,
  ledger: 1,
  code: 720,
  flags: 0,
  timestamp: 0n,
},
{
  id: 3n,
  debit_account_id: 102n,
  credit_account_id: 103n,
  amount: 10n,
  pending_id: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  timeout: 0,
  ledger: 1,
  code: 720,
  flags: 0,
  timestamp: 0n,
}];

const transfer_errors = await client.createTransfers(batch);
for (const error of transfer_errors) {
  switch (error.result) {
    case CreateTransferError.exists:
      console.error(`Batch transfer at ${error.index} already exists.`);
      break;
    default:
      console.error(
        `Batch transfer at ${error.index} failed to create: ${
          CreateTransferError[error.result]
        }.`,
      );
  }
}

To handle errors you can either 1) exactly match error codes returned from client.createTransfers with enum values in the CreateTransferError object, or you can 2) look up the error code in the CreateTransferError object for a human-readable string.
Batching

TigerBeetle performance is maximized when you batch API requests. A client instance shared across multiple threads/tasks can automatically batch concurrent requests, but the application must still send as many events as possible in a single call. For example, if you insert 1 million transfers sequentially, one at a time, the insert rate will be a fraction of the potential, because the client will wait for a reply between each one.

const batch = []; // Array of transfer to create.
for (let i = 0; i < batch.len; i++) {
  const transfer_errors = await client.createTransfers(batch[i]);
  // Error handling omitted.
}

Instead, always batch as much as you can. The maximum batch size is set in the TigerBeetle server. The default is 8189.

const batch = []; // Array of transfer to create.
const BATCH_SIZE = 8189;
for (let i = 0; i < batch.length; i += BATCH_SIZE) {
  const transfer_errors = await client.createTransfers(
    batch.slice(i, Math.min(batch.length, BATCH_SIZE)),
  );
  // Error handling omitted.
}

Queues and Workers

If you are making requests to TigerBeetle from workers pulling jobs from a queue, you can batch requests to TigerBeetle by having the worker act on multiple jobs from the queue at once rather than one at a time. i.e. pulling multiple jobs from the queue rather than just one.
Transfer Flags

The transfer flags value is a bitfield. See details for these flags in the Transfers reference.

To toggle behavior for a transfer, combine enum values stored in the TransferFlags object (in TypeScript it is an actual enum) with bitwise-or:

    TransferFlags.linked
    TransferFlags.pending
    TransferFlags.post_pending_transfer
    TransferFlags.void_pending_transfer

For example, to link transfer0 and transfer1:

const transfer0 = {
  id: 4n,
  debit_account_id: 102n,
  credit_account_id: 103n,
  amount: 10n,
  pending_id: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  timeout: 0,
  ledger: 1,
  code: 720,
  flags: TransferFlags.linked,
  timestamp: 0n,
};
const transfer1 = {
  id: 5n,
  debit_account_id: 102n,
  credit_account_id: 103n,
  amount: 10n,
  pending_id: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  timeout: 0,
  ledger: 1,
  code: 720,
  flags: 0,
  timestamp: 0n,
};

// Create the transfer
const transfer_errors = await client.createTransfers([transfer0, transfer1]);
// Error handling omitted.

Two-Phase Transfers

Two-phase transfers are supported natively by toggling the appropriate flag. TigerBeetle will then adjust the credits_pending and debits_pending fields of the appropriate accounts. A corresponding post pending transfer then needs to be sent to post or void the transfer.
Post a Pending Transfer

With flags set to post_pending_transfer, TigerBeetle will post the transfer. TigerBeetle will atomically roll back the changes to debits_pending and credits_pending of the appropriate accounts and apply them to the debits_posted and credits_posted balances.

const transfer0 = {
  id: 6n,
  debit_account_id: 102n,
  credit_account_id: 103n,
  amount: 10n,
  pending_id: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  timeout: 0,
  ledger: 1,
  code: 720,
  flags: TransferFlags.pending,
  timestamp: 0n,
};

let transfer_errors = await client.createTransfers([transfer0]);
// Error handling omitted.

const transfer1 = {
  id: 7n,
  debit_account_id: 102n,
  credit_account_id: 103n,
  // Post the entire pending amount.
  amount: amount_max,
  pending_id: 6n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  timeout: 0,
  ledger: 1,
  code: 720,
  flags: TransferFlags.post_pending_transfer,
  timestamp: 0n,
};

transfer_errors = await client.createTransfers([transfer1]);
// Error handling omitted.

Void a Pending Transfer

In contrast, with flags set to void_pending_transfer, TigerBeetle will void the transfer. TigerBeetle will roll back the changes to debits_pending and credits_pending of the appropriate accounts and not apply them to the debits_posted and credits_posted balances.

const transfer0 = {
  id: 8n,
  debit_account_id: 102n,
  credit_account_id: 103n,
  amount: 10n,
  pending_id: 0n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  timeout: 0,
  ledger: 1,
  code: 720,
  flags: TransferFlags.pending,
  timestamp: 0n,
};

let transfer_errors = await client.createTransfers([transfer0]);
// Error handling omitted.

const transfer1 = {
  id: 9n,
  debit_account_id: 102n,
  credit_account_id: 103n,
  amount: 0n,
  pending_id: 8n,
  user_data_128: 0n,
  user_data_64: 0n,
  user_data_32: 0,
  timeout: 0,
  ledger: 1,
  code: 720,
  flags: TransferFlags.void_pending_transfer,
  timestamp: 0n,
};

transfer_errors = await client.createTransfers([transfer1]);
// Error handling omitted.

Transfer Lookup

NOTE: While transfer lookup exists, it is not a flexible query API. We are developing query APIs and there will be new methods for querying transfers in the future.

Transfer lookup is batched, like transfer creation. Pass in all ids to fetch, and matched transfers are returned.

If no transfer matches an id, no object is returned for that transfer. So the order of transfers in the response is not necessarily the same as the order of ids in the request. You can refer to the id field in the response to distinguish transfers.

const transfers = await client.lookupTransfers([1n, 2n]);

Get Account Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the transfers involving a given account, allowing basic filter and pagination capabilities.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

const filter = {
  account_id: 2n,
  user_data_128: 0n, // No filter by UserData.
  user_data_64: 0n,
  user_data_32: 0,
  code: 0, // No filter by Code.
  timestamp_min: 0n, // No filter by Timestamp.
  timestamp_max: 0n, // No filter by Timestamp.
  limit: 10, // Limit to ten transfers at most.
  flags: AccountFilterFlags.debits | // Include transfer from the debit side.
    AccountFilterFlags.credits | // Include transfer from the credit side.
    AccountFilterFlags.reversed, // Sort by timestamp in reverse-chronological order.
};

const account_transfers = await client.getAccountTransfers(filter);

Get Account Balances

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the point-in-time balances of a given account, allowing basic filter and pagination capabilities.

Only accounts created with the flag history set retain historical balances.

The balances in the response are sorted by timestamp in chronological or reverse-chronological order.

const filter = {
  account_id: 2n,
  user_data_128: 0n, // No filter by UserData.
  user_data_64: 0n,
  user_data_32: 0,
  code: 0, // No filter by Code.
  timestamp_min: 0n, // No filter by Timestamp.
  timestamp_max: 0n, // No filter by Timestamp.
  limit: 10, // Limit to ten balances at most.
  flags: AccountFilterFlags.debits | // Include transfer from the debit side.
    AccountFilterFlags.credits | // Include transfer from the credit side.
    AccountFilterFlags.reversed, // Sort by timestamp in reverse-chronological order.
};

const account_balances = await client.getAccountBalances(filter);

Query Accounts

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query accounts by the intersection of some fields and by timestamp range.

The accounts in the response are sorted by timestamp in chronological or reverse-chronological order.

const query_filter = {
  user_data_128: 1000n, // Filter by UserData.
  user_data_64: 100n,
  user_data_32: 10,
  code: 1, // Filter by Code.
  ledger: 0, // No filter by Ledger.
  timestamp_min: 0n, // No filter by Timestamp.
  timestamp_max: 0n, // No filter by Timestamp.
  limit: 10, // Limit to ten accounts at most.
  flags: QueryFilterFlags.reversed, // Sort by timestamp in reverse-chronological order.
};

const query_accounts = await client.queryAccounts(query_filter);

Query Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query transfers by the intersection of some fields and by timestamp range.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

const query_filter = {
  user_data_128: 1000n, // Filter by UserData.
  user_data_64: 100n,
  user_data_32: 10,
  code: 1, // Filter by Code.
  ledger: 0, // No filter by Ledger.
  timestamp_min: 0n, // No filter by Timestamp.
  timestamp_max: 0n, // No filter by Timestamp.
  limit: 10, // Limit to ten transfers at most.
  flags: QueryFilterFlags.reversed, // Sort by timestamp in reverse-chronological order.
};

const query_transfers = await client.queryTransfers(query_filter);

Linked Events

When the linked flag is specified for an account when creating accounts or a transfer when creating transfers, it links that event with the next event in the batch, to create a chain of events, of arbitrary length, which all succeed or fail together. The tail of a chain is denoted by the first event without this flag. The last event in a batch may therefore never have the linked flag set as this would leave a chain open-ended. Multiple chains or individual events may coexist within a batch to succeed or fail independently.

Events within a chain are executed within order, or are rolled back on error, so that the effect of each event in the chain is visible to the next, and so that the chain is either visible or invisible as a unit to subsequent events after the chain. The event that was the first to break the chain will have a unique error result. Other events in the chain will have their error result set to linked_event_failed.

const batch = []; // Array of transfer to create.
let linkedFlag = 0;
linkedFlag |= TransferFlags.linked;

// An individual transfer (successful):
batch.push({ id: 1n /* , ... */ });

// A chain of 4 transfers (the last transfer in the chain closes the chain with linked=false):
batch.push({ id: 2n, /* ..., */ flags: linkedFlag }); // Commit/rollback.
batch.push({ id: 3n, /* ..., */ flags: linkedFlag }); // Commit/rollback.
batch.push({ id: 2n, /* ..., */ flags: linkedFlag }); // Fail with exists
batch.push({ id: 4n, /* ..., */ flags: 0 }); // Fail without committing.

// An individual transfer (successful):
// This should not see any effect from the failed chain above.
batch.push({ id: 2n, /* ..., */ flags: 0 });

// A chain of 2 transfers (the first transfer fails the chain):
batch.push({ id: 2n, /* ..., */ flags: linkedFlag });
batch.push({ id: 3n, /* ..., */ flags: 0 });

// A chain of 2 transfers (successful):
batch.push({ id: 3n, /* ..., */ flags: linkedFlag });
batch.push({ id: 4n, /* ..., */ flags: 0 });

const transfer_errors = await client.createTransfers(batch);
// Error handling omitted.

Imported Events

When the imported flag is specified for an account when creating accounts or a transfer when creating transfers, it allows importing historical events with a user-defined timestamp.

The entire batch of events must be set with the flag imported.

It’s recommended to submit the whole batch as a linked chain of events, ensuring that if any event fails, none of them are committed, preserving the last timestamp unchanged. This approach gives the application a chance to correct failed imported events, re-submitting the batch again with the same user-defined timestamps.

// External source of time.
let historical_timestamp = 0n
// Events loaded from an external source.
const historical_accounts = []; // Loaded from an external source.
const historical_transfers = []; // Loaded from an external source.

// First, load and import all accounts with their timestamps from the historical source.
const accounts = [];
for (let index = 0; i < historical_accounts.length; i++) {
  let account = historical_accounts[i];
  // Set a unique and strictly increasing timestamp.
  historical_timestamp += 1;
  account.timestamp = historical_timestamp;
  // Set the account as `imported`.
  account.flags = AccountFlags.imported;
  // To ensure atomicity, the entire batch (except the last event in the chain)
  // must be `linked`.
  if (index < historical_accounts.length - 1) {
    account.flags |= AccountFlags.linked;
  }

  accounts.push(account);
}

const account_errors = await client.createAccounts(accounts);
// Error handling omitted.

// Then, load and import all transfers with their timestamps from the historical source.
const transfers = [];
for (let index = 0; i < historical_transfers.length; i++) {
  let transfer = historical_transfers[i];
  // Set a unique and strictly increasing timestamp.
  historical_timestamp += 1;
  transfer.timestamp = historical_timestamp;
  // Set the account as `imported`.
  transfer.flags = TransferFlags.imported;
  // To ensure atomicity, the entire batch (except the last event in the chain)
  // must be `linked`.
  if (index < historical_transfers.length - 1) {
    transfer.flags |= TransferFlags.linked;
  }

  transfers.push(transfer);
}

const transfer_errors = await client.createTransfers(transfers);
// Error handling omitted.

// Since it is a linked chain, in case of any error the entire batch is rolled back and can be retried
// with the same historical timestamps without regressing the cluster timestamp.

Timeouts And Cancellation

The Client retries indefinitely and doesn’t impose any per-request timeout. Cancellation is provided as a mechanism, and the specific cancellation policy is left to the application. A Client instance can be closed at any time. On close, all in-flight requests are canceled and return an error to the caller. Even if an error is returned, a request might still be processed by the TigerBeetle server. Reliable transaction submission explains how to make transfers retry-proof using IDs for end-to-end idempotency.
Edit this page
tigerbeetle-python

The TigerBeetle client for Python.
Prerequisites

Linux >= 5.6 is the only production environment we support. But for ease of development we also support macOS and Windows.

    Python (or PyPy, etc) >= 3.7

Setup

First, create a directory for your project and cd into the directory.

Then, install the TigerBeetle client:

pip install tigerbeetle

Now, create main.py and copy this into it:

import os

import tigerbeetle as tb

print("Import OK!")

# To enable debug logging, via Python's built in logging module:
# logging.basicConfig(level=logging.DEBUG)
# tb.configure_logging(debug=True)

Finally, build and run:

python3 main.py

Now that all prerequisites and dependencies are correctly set up, let’s dig into using TigerBeetle.
Sample projects

This document is primarily a reference guide to the client. Below are various sample projects demonstrating features of TigerBeetle.

    Basic: Create two accounts and transfer an amount between them.
    Two-Phase Transfer: Create two accounts and start a pending transfer between them, then post the transfer.
    Many Two-Phase Transfers: Create two accounts and start a number of pending transfers between them, posting and voiding alternating transfers.

Creating a Client

A client is created with a cluster ID and replica addresses for all replicas in the cluster. The cluster ID and replica addresses are both chosen by the system that starts the TigerBeetle cluster.

Clients are thread-safe and a single instance should be shared between multiple concurrent tasks. This allows events to be automatically batched.

Multiple clients are useful when connecting to more than one TigerBeetle cluster.

In this example the cluster ID is 0 and there is one replica. The address is read from the TB_ADDRESS environment variable and defaults to port 3000.

with tb.ClientSync(cluster_id=0, replica_addresses=os.getenv("TB_ADDRESS", "3000")) as client:
    # Use the client.
    pass

# Alternatively:
async with tb.ClientAsync(cluster_id=0, replica_addresses=os.getenv("TB_ADDRESS", "3000")) as client:
    # Use the client, async!
    pass

The following are valid addresses:

    3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1:3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1 (interpreted as 127.0.0.1:3001, 3001 is the default port)

Creating Accounts

See details for account fields in the Accounts reference.

account = tb.Account(
    id=tb.id(), # TigerBeetle time-based ID.
    debits_pending=0,
    debits_posted=0,
    credits_pending=0,
    credits_posted=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    ledger=1,
    code=718,
    flags=0,
    timestamp=0,
)

account_errors = client.create_accounts([account])
# Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.
Account Flags

The account flags value is a bitfield. See details for these flags in the Accounts reference.

To toggle behavior for an account, combine enum values stored in the AccountFlags object (it’s an enum.IntFlag) with bitwise-or:

    AccountFlags.linked
    AccountFlags.debits_must_not_exceed_credits
    AccountFlags.credits_must_not_exceed_credits
    AccountFlags.history

For example, to link two accounts where the first account additionally has the debits_must_not_exceed_credits constraint:

account0 = tb.Account(
    id=100,
    debits_pending=0,
    debits_posted=0,
    credits_pending=0,
    credits_posted=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    ledger=1,
    code=1,
    timestamp=0,
    flags=tb.AccountFlags.LINKED | tb.AccountFlags.DEBITS_MUST_NOT_EXCEED_CREDITS,
)
account1 = tb.Account(
    id=101,
    debits_pending=0,
    debits_posted=0,
    credits_pending=0,
    credits_posted=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    ledger=1,
    code=1,
    timestamp=0,
    flags=tb.AccountFlags.HISTORY,
)

account_errors = client.create_accounts([account0, account1])
# Error handling omitted.

Response and Errors

The response is an empty array if all accounts were created successfully. If the response is non-empty, each object in the response array contains error information for an account that failed. The error object contains an error code and the index of the account in the request batch.

See all error conditions in the create_accounts reference.

account0 = tb.Account(
    id=102,
    debits_pending=0,
    debits_posted=0,
    credits_pending=0,
    credits_posted=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    ledger=1,
    code=1,
    timestamp=0,
    flags=0,
)
account1 = tb.Account(
    id=103,
    debits_pending=0,
    debits_posted=0,
    credits_pending=0,
    credits_posted=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    ledger=1,
    code=1,
    timestamp=0,
    flags=0,
)
account2 = tb.Account(
    id=104,
    debits_pending=0,
    debits_posted=0,
    credits_pending=0,
    credits_posted=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    ledger=1,
    code=1,
    timestamp=0,
    flags=0,
)

account_errors = client.create_accounts([account0, account1, account2])
for error in account_errors:
    if error.result == tb.CreateAccountResult.EXISTS:
        print(f"Batch account at {error.index} already exists.")
    else:
        print(f"Batch account at ${error.index} failed to create: {error.result}.")

To handle errors you can compare the result code returned from client.create_accounts with enum values in the CreateAccountResult object.
Account Lookup

Account lookup is batched, like account creation. Pass in all IDs to fetch. The account for each matched ID is returned.

If no account matches an ID, no object is returned for that account. So the order of accounts in the response is not necessarily the same as the order of IDs in the request. You can refer to the ID field in the response to distinguish accounts.

accounts = client.lookup_accounts([100, 101])

Create Transfers

This creates a journal entry between two accounts.

See details for transfer fields in the Transfers reference.

transfers = [tb.Transfer(
    id=tb.id(), # TigerBeetle time-based ID.
    debit_account_id=102,
    credit_account_id=103,
    amount=10,
    pending_id=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    timeout=0,
    ledger=1,
    code=720,
    flags=0,
    timestamp=0,
)]

transfer_errors = client.create_transfers(transfers)
# Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.
Response and Errors

The response is an empty array if all transfers were created successfully. If the response is non-empty, each object in the response array contains error information for a transfer that failed. The error object contains an error code and the index of the transfer in the request batch.

See all error conditions in the create_transfers reference.

batch = [tb.Transfer(
    id=1,
    debit_account_id=102,
    credit_account_id=103,
    amount=10,
    pending_id=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    timeout=0,
    ledger=1,
    code=720,
    flags=0,
    timestamp=0,
),
    tb.Transfer(
    id=2,
    debit_account_id=102,
    credit_account_id=103,
    amount=10,
    pending_id=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    timeout=0,
    ledger=1,
    code=720,
    flags=0,
    timestamp=0,
),
    tb.Transfer(
    id=3,
    debit_account_id=102,
    credit_account_id=103,
    amount=10,
    pending_id=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    timeout=0,
    ledger=1,
    code=720,
    flags=0,
    timestamp=0,
)]

transfer_errors = client.create_transfers(batch)
for error in transfer_errors:
    if error.result == tb.CreateTransferResult.EXISTS:
        print(f"Batch transfer at {error.index} already exists.")
    else:
        print(f"Batch transfer at {error.index} failed to create: {error.result}.")

To handle errors you can compare the result code returned from client.create_transfers with enum values in the CreateTransferResult object.
Batching

TigerBeetle performance is maximized when you batch API requests. A client instance shared across multiple threads/tasks can automatically batch concurrent requests, but the application must still send as many events as possible in a single call. For example, if you insert 1 million transfers sequentially, one at a time, the insert rate will be a fraction of the potential, because the client will wait for a reply between each one.

batch = [] # Array of transfer to create.
for transfer in batch:
    transfer_errors = client.create_transfers([transfer])
    # Error handling omitted.

Instead, always batch as much as you can. The maximum batch size is set in the TigerBeetle server. The default is 8189.

batch = [] # Array of transfer to create.
BATCH_SIZE = 8189 #FIXME
for i in range(0, len(batch), BATCH_SIZE):
    transfer_errors = client.create_transfers(
        batch[i:min(len(batch), i + BATCH_SIZE)],
    )
    # Error handling omitted.

Queues and Workers

If you are making requests to TigerBeetle from workers pulling jobs from a queue, you can batch requests to TigerBeetle by having the worker act on multiple jobs from the queue at once rather than one at a time. i.e. pulling multiple jobs from the queue rather than just one.
Transfer Flags

The transfer flags value is a bitfield. See details for these flags in the Transfers reference.

To toggle behavior for a transfer, combine enum values stored in the TransferFlags object (it’s an enum.IntFlag) with bitwise-or:

    TransferFlags.linked
    TransferFlags.pending
    TransferFlags.post_pending_transfer
    TransferFlags.void_pending_transfer

For example, to link transfer0 and transfer1:

transfer0 = tb.Transfer(
    id=4,
    debit_account_id=102,
    credit_account_id=103,
    amount=10,
    pending_id=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    timeout=0,
    ledger=1,
    code=720,
    flags=tb.TransferFlags.LINKED,
    timestamp=0,
)
transfer1 = tb.Transfer(
    id=5,
    debit_account_id=102,
    credit_account_id=103,
    amount=10,
    pending_id=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    timeout=0,
    ledger=1,
    code=720,
    flags=0,
    timestamp=0,
)

# Create the transfer
transfer_errors = client.create_transfers([transfer0, transfer1])
# Error handling omitted.

Two-Phase Transfers

Two-phase transfers are supported natively by toggling the appropriate flag. TigerBeetle will then adjust the credits_pending and debits_pending fields of the appropriate accounts. A corresponding post pending transfer then needs to be sent to post or void the transfer.
Post a Pending Transfer

With flags set to post_pending_transfer, TigerBeetle will post the transfer. TigerBeetle will atomically roll back the changes to debits_pending and credits_pending of the appropriate accounts and apply them to the debits_posted and credits_posted balances.

transfer0 = tb.Transfer(
    id=6,
    debit_account_id=102,
    credit_account_id=103,
    amount=10,
    pending_id=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    timeout=0,
    ledger=1,
    code=720,
    flags=tb.TransferFlags.PENDING,
    timestamp=0,
)

transfer_errors = client.create_transfers([transfer0])
# Error handling omitted.

transfer1 = tb.Transfer(
    id=7,
    debit_account_id=102,
    credit_account_id=103,
    # Post the entire pending amount.
    amount=tb.amount_max,
    pending_id=6,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    timeout=0,
    ledger=1,
    code=720,
    flags=tb.TransferFlags.POST_PENDING_TRANSFER,
    timestamp=0,
)

transfer_errors = client.create_transfers([transfer1])
# Error handling omitted.

Void a Pending Transfer

In contrast, with flags set to void_pending_transfer, TigerBeetle will void the transfer. TigerBeetle will roll back the changes to debits_pending and credits_pending of the appropriate accounts and not apply them to the debits_posted and credits_posted balances.

transfer0 = tb.Transfer(
    id=8,
    debit_account_id=102,
    credit_account_id=103,
    amount=10,
    pending_id=0,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    timeout=0,
    ledger=1,
    code=720,
    flags=tb.TransferFlags.PENDING,
    timestamp=0,
)

transfer_errors = client.create_transfers([transfer0])
# Error handling omitted.

transfer1 = tb.Transfer(
    id=9,
    debit_account_id=102,
    credit_account_id=103,
    amount=0,
    pending_id=8,
    user_data_128=0,
    user_data_64=0,
    user_data_32=0,
    timeout=0,
    ledger=1,
    code=720,
    flags=tb.TransferFlags.VOID_PENDING_TRANSFER,
    timestamp=0,
)

transfer_errors = client.create_transfers([transfer1])
# Error handling omitted.

Transfer Lookup

NOTE: While transfer lookup exists, it is not a flexible query API. We are developing query APIs and there will be new methods for querying transfers in the future.

Transfer lookup is batched, like transfer creation. Pass in all ids to fetch, and matched transfers are returned.

If no transfer matches an id, no object is returned for that transfer. So the order of transfers in the response is not necessarily the same as the order of ids in the request. You can refer to the id field in the response to distinguish transfers.

transfers = client.lookup_transfers([1, 2])

Get Account Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the transfers involving a given account, allowing basic filter and pagination capabilities.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

filter = tb.AccountFilter(
    account_id=2,
    user_data_128=0, # No filter by UserData.
    user_data_64=0,
    user_data_32=0,
    code=0, # No filter by Code.
    timestamp_min=0, # No filter by Timestamp.
    timestamp_max=0, # No filter by Timestamp.
    limit=10, # Limit to ten transfers at most.
    flags=tb.AccountFilterFlags.DEBITS | # Include transfer from the debit side.
    tb.AccountFilterFlags.CREDITS | # Include transfer from the credit side.
    tb.AccountFilterFlags.REVERSED, # Sort by timestamp in reverse-chronological order.
)

account_transfers = client.get_account_transfers(filter)

Get Account Balances

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the point-in-time balances of a given account, allowing basic filter and pagination capabilities.

Only accounts created with the flag history set retain historical balances.

The balances in the response are sorted by timestamp in chronological or reverse-chronological order.

filter = tb.AccountFilter(
    account_id=2,
    user_data_128=0, # No filter by UserData.
    user_data_64=0,
    user_data_32=0,
    code=0, # No filter by Code.
    timestamp_min=0, # No filter by Timestamp.
    timestamp_max=0, # No filter by Timestamp.
    limit=10, # Limit to ten balances at most.
    flags=tb.AccountFilterFlags.DEBITS | # Include transfer from the debit side.
    tb.AccountFilterFlags.CREDITS | # Include transfer from the credit side.
    tb.AccountFilterFlags.REVERSED, # Sort by timestamp in reverse-chronological order.
)

account_balances = client.get_account_balances(filter)

Query Accounts

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query accounts by the intersection of some fields and by timestamp range.

The accounts in the response are sorted by timestamp in chronological or reverse-chronological order.

query_filter = tb.QueryFilter(
    user_data_128=1000, # Filter by UserData.
    user_data_64=100,
    user_data_32=10,
    code=1, # Filter by Code.
    ledger=0, # No filter by Ledger.
    timestamp_min=0, # No filter by Timestamp.
    timestamp_max=0, # No filter by Timestamp.
    limit=10, # Limit to ten accounts at most.
    flags=tb.QueryFilterFlags.REVERSED, # Sort by timestamp in reverse-chronological order.
)

query_accounts = client.query_accounts(query_filter)

Query Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query transfers by the intersection of some fields and by timestamp range.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

query_filter = tb.QueryFilter(
    user_data_128=1000, # Filter by UserData.
    user_data_64=100,
    user_data_32=10,
    code=1, # Filter by Code.
    ledger=0, # No filter by Ledger.
    timestamp_min=0, # No filter by Timestamp.
    timestamp_max=0, # No filter by Timestamp.
    limit=10, # Limit to ten transfers at most.
    flags=tb.QueryFilterFlags.REVERSED, # Sort by timestamp in reverse-chronological order.
)

query_transfers = client.query_transfers(query_filter)

Linked Events

When the linked flag is specified for an account when creating accounts or a transfer when creating transfers, it links that event with the next event in the batch, to create a chain of events, of arbitrary length, which all succeed or fail together. The tail of a chain is denoted by the first event without this flag. The last event in a batch may therefore never have the linked flag set as this would leave a chain open-ended. Multiple chains or individual events may coexist within a batch to succeed or fail independently.

Events within a chain are executed within order, or are rolled back on error, so that the effect of each event in the chain is visible to the next, and so that the chain is either visible or invisible as a unit to subsequent events after the chain. The event that was the first to break the chain will have a unique error result. Other events in the chain will have their error result set to linked_event_failed.

batch = [] # List of tb.Transfers to create.
linkedFlag = 0
linkedFlag |= tb.TransferFlags.LINKED

# An individual transfer (successful):
batch.append(tb.Transfer(id=1))

# A chain of 4 transfers (the last transfer in the chain closes the chain with linked=false):
batch.append(tb.Transfer(id=2, flags=linkedFlag)) # Commit/rollback.
batch.append(tb.Transfer(id=3, flags=linkedFlag)) # Commit/rollback.
batch.append(tb.Transfer(id=2, flags=linkedFlag)) # Fail with exists
batch.append(tb.Transfer(id=4, flags=0)) # Fail without committing.

# An individual transfer (successful):
# This should not see any effect from the failed chain above.
batch.append(tb.Transfer(id=2, flags=0 ))

# A chain of 2 transfers (the first transfer fails the chain):
batch.append(tb.Transfer(id=2, flags=linkedFlag))
batch.append(tb.Transfer(id=3, flags=0))

# A chain of 2 transfers (successful):
batch.append(tb.Transfer(id=3, flags=linkedFlag))
batch.append(tb.Transfer(id=4, flags=0))

transfer_errors = client.create_transfers(batch)
# Error handling omitted.

Imported Events

When the imported flag is specified for an account when creating accounts or a transfer when creating transfers, it allows importing historical events with a user-defined timestamp.

The entire batch of events must be set with the flag imported.

It’s recommended to submit the whole batch as a linked chain of events, ensuring that if any event fails, none of them are committed, preserving the last timestamp unchanged. This approach gives the application a chance to correct failed imported events, re-submitting the batch again with the same user-defined timestamps.

# External source of time.
historical_timestamp = 0
# Events loaded from an external source.
historical_accounts = [] # Loaded from an external source.
historical_transfers = [] # Loaded from an external source.

# First, load and import all accounts with their timestamps from the historical source.
accounts = []
for index, account in enumerate(historical_accounts):
    # Set a unique and strictly increasing timestamp.
    historical_timestamp += 1
    account.timestamp = historical_timestamp
    # Set the account as `imported`.
    account.flags = tb.AccountFlags.IMPORTED
    # To ensure atomicity, the entire batch (except the last event in the chain)
    # must be `linked`.
    if index < len(historical_accounts) - 1:
        account.flags |= tb.AccountFlags.LINKED

    accounts.append(account)

account_errors = client.create_accounts(accounts)
# Error handling omitted.

# The, load and import all transfers with their timestamps from the historical source.
transfers = []
for index, transfer in enumerate(historical_transfers):
    # Set a unique and strictly increasing timestamp.
    historical_timestamp += 1
    transfer.timestamp = historical_timestamp
    # Set the account as `imported`.
    transfer.flags = tb.TransferFlags.IMPORTED
    # To ensure atomicity, the entire batch (except the last event in the chain)
    # must be `linked`.
    if index < len(historical_transfers) - 1:
        transfer.flags |= tb.AccountFlags.LINKED

    transfers.append(transfer)

transfer_errors = client.create_transfers(transfers)
# Error handling omitted.

# Since it is a linked chain, in case of any error the entire batch is rolled back and can be retried
# with the same historical timestamps without regressing the cluster timestamp.

Timeouts And Cancellation

The Client retries indefinitely and doesn’t impose any per-request timeout. Cancellation is provided as a mechanism, and the specific cancellation policy is left to the application. A Client instance can be closed at any time. On close, all in-flight requests are canceled and return an error to the caller. Even if an error is returned, a request might still be processed by the TigerBeetle server. Reliable transaction submission explains how to make transfers retry-proof using IDs for end-to-end idempotency.
Edit this page
tigerbeetle-rust

The TigerBeetle client for Rust.

crates.io docs.rs
Prerequisites

Linux >= 5.6 is the only production environment we support. But for ease of development we also support macOS and Windows.

    Rust 1.68+

Setup

First, create a directory for your project and cd into the directory.

Then create Cargo.toml and copy this into it:

[package]
name = "tigerbeetle-test"
version = "0.1.0"
edition = "2024"

[dependencies]
tigerbeetle.path = "../.."
futures = "0.3"

Now, create src/main.rs and copy this into it:

use tigerbeetle as tb;

fn main() -> Result<(), Box<dyn std::error::Error>> {
    futures::executor::block_on(main_async())
}

async fn main_async() -> Result<(), Box<dyn std::error::Error>> {
    println!("hello world");
    Ok(())
}

Finally, build and run:

cargo run

Now that all prerequisites and dependencies are correctly set up, let’s dig into using TigerBeetle.
Sample projects

This document is primarily a reference guide to the client. Below are various sample projects demonstrating features of TigerBeetle.

    Basic: Create two accounts and transfer an amount between them.
    Two-Phase Transfer: Create two accounts and start a pending transfer between them, then post the transfer.
    Many Two-Phase Transfers: Create two accounts and start a number of pending transfers between them, posting and voiding alternating transfers.

Creating a Client

A client is created with a cluster ID and replica addresses for all replicas in the cluster. The cluster ID and replica addresses are both chosen by the system that starts the TigerBeetle cluster.

Clients are thread-safe and a single instance should be shared between multiple concurrent tasks. This allows events to be automatically batched.

Multiple clients are useful when connecting to more than one TigerBeetle cluster.

In this example the cluster ID is 0 and there is one replica. The address is read from the TB_ADDRESS environment variable and defaults to port 3000.

let cluster_id = 0;
let replica_address = std::env::var("TB_ADDRESS")
    .ok()
    .unwrap_or_else(|| String::from("3000"));
let client = tb::Client::new(cluster_id, &replica_address)?;

The following are valid addresses:

    3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1:3000 (interpreted as 127.0.0.1:3000)
    127.0.0.1 (interpreted as 127.0.0.1:3001, 3001 is the default port)

Creating Accounts

See details for account fields in the Accounts reference.

let account_errors = client
    .create_accounts(&[tb::Account {
        id: tb::id(),
        ledger: 1,
        code: 718,
        ..Default::default()
    }])
    .await?;
// Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.
Account Flags

The account flags value is a bitfield. See details for these flags in the Accounts reference.

To toggle behavior for an account, use the AccountFlags bitflags. You can combine multiple flags using the | operator. Here are a few examples:

    AccountFlags::Linked
    AccountFlags::DebitsMustNotExceedCredits
    AccountFlags::CreditsMustNotExceedDebits
    AccountFlags::History
    AccountFlags::Linked | AccountFlags::History

For example, to link two accounts where the first account additionally has the debits_must_not_exceed_credits constraint:

let account0 = tb::Account {
    id: 100,
    ledger: 1,
    code: 718,
    flags: tb::AccountFlags::DebitsMustNotExceedCredits | tb::AccountFlags::Linked,
    ..Default::default()
};
let account1 = tb::Account {
    id: 101,
    ledger: 1,
    code: 718,
    flags: tb::AccountFlags::History,
    ..Default::default()
};

let account_errors = client.create_accounts(&[account0, account1]).await?;
// Error handling omitted.

Response and Errors

The response is an empty array if all accounts were created successfully. If the response is non-empty, each object in the response array contains error information for an account that failed. The error object contains an error code and the index of the account in the request batch.

See all error conditions in the create_accounts reference.

let account0 = tb::Account {
    id: 102,
    ledger: 1,
    code: 718,
    ..Default::default()
};
let account1 = tb::Account {
    id: 103,
    ledger: 1,
    code: 718,
    ..Default::default()
};
let account2 = tb::Account {
    id: 104,
    ledger: 1,
    code: 718,
    ..Default::default()
};

let account_errors = client
    .create_accounts(&[account0, account1, account2])
    .await?;

assert!(account_errors.len() <= 3);

for err in account_errors {
    match err.result {
        tb::CreateAccountResult::Exists => {
            println!("Batch account at {} already exists.", err.index);
        }
        _ => {
            eprintln!(
                "Batch account at {} failed to create: {:?}",
                err.index, err.result
            );
        }
    }
}

To handle errors, iterate over the Vec<CreateAccountsResult> returned from client.create_accounts(). Each result contains an index field to map back to the input account and a result field with the CreateAccountResult enum.
Account Lookup

Account lookup is batched, like account creation. Pass in all IDs to fetch. The account for each matched ID is returned.

If no account matches an ID, no object is returned for that account. So the order of accounts in the response is not necessarily the same as the order of IDs in the request. You can refer to the ID field in the response to distinguish accounts.

let accounts = client.lookup_accounts(&[100, 101]).await?;

Create Transfers

This creates a journal entry between two accounts.

See details for transfer fields in the Transfers reference.

let transfers = vec![tb::Transfer {
    id: tb::id(),
    debit_account_id: 101,
    credit_account_id: 102,
    amount: 10,
    ledger: 1,
    code: 1,
    ..Default::default()
}];

let transfer_errors = client.create_transfers(&transfers).await?;
// Error handling omitted.

See details for the recommended ID scheme in time-based identifiers.
Response and Errors

The response is an empty array if all transfers were created successfully. If the response is non-empty, each object in the response array contains error information for a transfer that failed. The error object contains an error code and the index of the transfer in the request batch.

See all error conditions in the create_transfers reference.

let transfers = vec![
    tb::Transfer {
        id: 1,
        debit_account_id: 101,
        credit_account_id: 102,
        amount: 10,
        ledger: 1,
        code: 1,
        ..Default::default()
    },
    tb::Transfer {
        id: 2,
        debit_account_id: 101,
        credit_account_id: 102,
        amount: 10,
        ledger: 1,
        code: 1,
        ..Default::default()
    },
    tb::Transfer {
        id: 3,
        debit_account_id: 101,
        credit_account_id: 102,
        amount: 10,
        ledger: 1,
        code: 1,
        ..Default::default()
    },
];

let transfer_errors = client.create_transfers(&transfers).await?;

for err in transfer_errors {
    match err.result {
        tb::CreateTransferResult::Exists => {
            println!("Batch transfer at {} already exists.", err.index);
        }
        _ => {
            eprintln!(
                "Batch transfer at {} failed to create: {:?}",
                err.index, err.result
            );
        }
    }
}

To handle transfer errors, iterate over the Vec<CreateTransfersResult> returned from client.create_transfers(). Each result contains an index field to map back to the input transfer and a result field with the CreateTransferResult enum.
Batching

TigerBeetle performance is maximized when you batch API requests. A client instance shared across multiple threads/tasks can automatically batch concurrent requests, but the application must still send as many events as possible in a single call. For example, if you insert 1 million transfers sequentially, one at a time, the insert rate will be a fraction of the potential, because the client will wait for a reply between each one.

let batch: Vec<tb::Transfer> = vec![];
for transfer in &batch {
    let transfer_errors = client.create_transfers(&[*transfer]).await?;
    // Error handling omitted.
}

Instead, always batch as much as you can. The maximum batch size is set in the TigerBeetle server. The default is 8189.

let transfers: Vec<tb::Transfer> = vec![];
const BATCH_SIZE: usize = 8189;
for batch in transfers.chunks(BATCH_SIZE) {
    let transfer_errors = client.create_transfers(batch).await?;
    // Error handling omitted.
}

Queues and Workers

If you are making requests to TigerBeetle from workers pulling jobs from a queue, you can batch requests to TigerBeetle by having the worker act on multiple jobs from the queue at once rather than one at a time. i.e. pulling multiple jobs from the queue rather than just one.
Transfer Flags

The transfer flags value is a bitfield. See details for these flags in the Transfers reference.

To toggle behavior for a transfer, use the TransferFlags bitflags. You can combine multiple flags using the | operator. Here are a few examples:

    TransferFlags::Linked
    TransferFlags::Pending
    TransferFlags::PostPendingTransfer
    TransferFlags::VoidPendingTransfer
    TransferFlags::Linked | TransferFlags::Pending

For example, to link transfer0 and transfer1:

let transfer0 = tb::Transfer {
    id: 4,
    debit_account_id: 101,
    credit_account_id: 102,
    amount: 10,
    ledger: 1,
    code: 1,
    flags: tb::TransferFlags::Linked,
    ..Default::default()
};
let transfer1 = tb::Transfer {
    id: 5,
    debit_account_id: 101,
    credit_account_id: 102,
    amount: 10,
    ledger: 1,
    code: 1,
    ..Default::default()
};

let transfer_errors = client.create_transfers(&[transfer0, transfer1]).await?;
// Error handling omitted.

Two-Phase Transfers

Two-phase transfers are supported natively by toggling the appropriate flag. TigerBeetle will then adjust the credits_pending and debits_pending fields of the appropriate accounts. A corresponding post pending transfer then needs to be sent to post or void the transfer.
Post a Pending Transfer

With flags set to post_pending_transfer, TigerBeetle will post the transfer. TigerBeetle will atomically roll back the changes to debits_pending and credits_pending of the appropriate accounts and apply them to the debits_posted and credits_posted balances.

let transfer0 = tb::Transfer {
    id: 6,
    debit_account_id: 101,
    credit_account_id: 102,
    amount: 10,
    ledger: 1,
    code: 1,
    ..Default::default()
};

let transfer_errors = client.create_transfers(&[transfer0]).await?;
// Error handling omitted.

let transfer1 = tb::Transfer {
    id: 7,
    amount: u128::MAX,
    pending_id: 6,
    flags: tb::TransferFlags::PostPendingTransfer,
    ..Default::default()
};

let transfer_errors = client.create_transfers(&[transfer1]).await?;
// Error handling omitted.

Void a Pending Transfer

In contrast, with flags set to void_pending_transfer, TigerBeetle will void the transfer. TigerBeetle will roll back the changes to debits_pending and credits_pending of the appropriate accounts and not apply them to the debits_posted and credits_posted balances.

let transfer0 = tb::Transfer {
    id: 8,
    debit_account_id: 101,
    credit_account_id: 102,
    amount: 10,
    ledger: 1,
    code: 1,
    ..Default::default()
};

let transfer_errors = client.create_transfers(&[transfer0]).await?;
// Error handling omitted.

let transfer1 = tb::Transfer {
    id: 9,
    amount: 0,
    pending_id: 8,
    flags: tb::TransferFlags::VoidPendingTransfer,
    ..Default::default()
};

let transfer_errors = client.create_transfers(&[transfer1]).await?;
// Error handling omitted.

Transfer Lookup

NOTE: While transfer lookup exists, it is not a flexible query API. We are developing query APIs and there will be new methods for querying transfers in the future.

Transfer lookup is batched, like transfer creation. Pass in all ids to fetch, and matched transfers are returned.

If no transfer matches an id, no object is returned for that transfer. So the order of transfers in the response is not necessarily the same as the order of ids in the request. You can refer to the id field in the response to distinguish transfers.

let transfers = client.lookup_transfers(&[1, 2]).await?;

Get Account Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the transfers involving a given account, allowing basic filter and pagination capabilities.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

let filter = tb::AccountFilter {
    account_id: 2,
    user_data_128: 0,
    user_data_64: 0,
    user_data_32: 0,
    code: 0,
    reserved: Default::default(),
    timestamp_min: 0,
    timestamp_max: 0,
    limit: 10,
    flags: tb::AccountFilterFlags::Debits
        | tb::AccountFilterFlags::Credits
        | tb::AccountFilterFlags::Reversed,
};

let transfers = client.get_account_transfers(filter).await?;

Get Account Balances

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Fetches the point-in-time balances of a given account, allowing basic filter and pagination capabilities.

Only accounts created with the flag history set retain historical balances.

The balances in the response are sorted by timestamp in chronological or reverse-chronological order.

let filter = tb::AccountFilter {
    account_id: 2,
    user_data_128: 0,
    user_data_64: 0,
    user_data_32: 0,
    code: 0,
    reserved: Default::default(),
    timestamp_min: 0,
    timestamp_max: 0,
    limit: 10,
    flags: tb::AccountFilterFlags::Debits
        | tb::AccountFilterFlags::Credits
        | tb::AccountFilterFlags::Reversed,
};

let account_balances = client.get_account_balances(filter).await?;

Query Accounts

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query accounts by the intersection of some fields and by timestamp range.

The accounts in the response are sorted by timestamp in chronological or reverse-chronological order.

let filter = tb::QueryFilter {
    user_data_128: 1000,
    user_data_64: 100,
    user_data_32: 10,
    code: 1,
    ledger: 0,
    reserved: Default::default(),
    timestamp_min: 0,
    timestamp_max: 0,
    limit: 10,
    flags: tb::QueryFilterFlags::Reversed,
};

let accounts = client.query_accounts(filter).await?;

Query Transfers

NOTE: This is a preview API that is subject to breaking changes once we have a stable querying API.

Query transfers by the intersection of some fields and by timestamp range.

The transfers in the response are sorted by timestamp in chronological or reverse-chronological order.

let filter = tb::QueryFilter {
    user_data_128: 1000,
    user_data_64: 100,
    user_data_32: 10,
    code: 1,
    ledger: 0,
    reserved: Default::default(),
    timestamp_min: 0,
    timestamp_max: 0,
    limit: 10,
    flags: tb::QueryFilterFlags::Reversed,
};

let transfers = client.query_transfers(filter).await?;

Linked Events

When the linked flag is specified for an account when creating accounts or a transfer when creating transfers, it links that event with the next event in the batch, to create a chain of events, of arbitrary length, which all succeed or fail together. The tail of a chain is denoted by the first event without this flag. The last event in a batch may therefore never have the linked flag set as this would leave a chain open-ended. Multiple chains or individual events may coexist within a batch to succeed or fail independently.

Events within a chain are executed within order, or are rolled back on error, so that the effect of each event in the chain is visible to the next, and so that the chain is either visible or invisible as a unit to subsequent events after the chain. The event that was the first to break the chain will have a unique error result. Other events in the chain will have their error result set to linked_event_failed.

let mut batch = vec![];
let linked_flag = tb::TransferFlags::Linked;

// An individual transfer (successful):
batch.push(tb::Transfer {
    id: 1,
    ..Default::default()
});

// A chain of 4 transfers (the last transfer in the chain closes the chain with linked=false):
batch.push(tb::Transfer {
    id: 2,
    flags: linked_flag,
    ..Default::default()
});
batch.push(tb::Transfer {
    id: 3,
    flags: linked_flag,
    ..Default::default()
});
batch.push(tb::Transfer {
    id: 2,
    flags: linked_flag,
    ..Default::default()
});
batch.push(tb::Transfer {
    id: 4,
    ..Default::default()
});

// An individual transfer (successful):
// This should not see any effect from the failed chain above.
batch.push(tb::Transfer {
    id: 2,
    ..Default::default()
});

// A chain of 2 transfers (the first transfer fails the chain):
batch.push(tb::Transfer {
    id: 2,
    flags: linked_flag,
    ..Default::default()
});
batch.push(tb::Transfer {
    id: 3,
    ..Default::default()
});

// A chain of 2 transfers (successful):
batch.push(tb::Transfer {
    id: 3,
    flags: linked_flag,
    ..Default::default()
});
batch.push(tb::Transfer {
    id: 4,
    ..Default::default()
});

let transfer_errors = client.create_transfers(&batch).await?;
// Error handling omitted.

Imported Events

When the imported flag is specified for an account when creating accounts or a transfer when creating transfers, it allows importing historical events with a user-defined timestamp.

The entire batch of events must be set with the flag imported.

It’s recommended to submit the whole batch as a linked chain of events, ensuring that if any event fails, none of them are committed, preserving the last timestamp unchanged. This approach gives the application a chance to correct failed imported events, re-submitting the batch again with the same user-defined timestamps.

// External source of time.
let mut historical_timestamp: u64 = 0;
let historical_accounts: Vec<tb::Account> = vec![]; // Loaded from an external source.
let historical_transfers: Vec<tb::Transfer> = vec![]; // Loaded from an external source.

// First, load and import all accounts with their timestamps from the historical source.
let mut accounts_batch = vec![];
for (index, mut account) in historical_accounts.into_iter().enumerate() {
    // Set a unique and strictly increasing timestamp.
    historical_timestamp += 1;
    account.timestamp = historical_timestamp;

    account.flags = if index < accounts_batch.len() - 1 {
        tb::AccountFlags::Imported | tb::AccountFlags::Linked
    } else {
        tb::AccountFlags::Imported
    };

    accounts_batch.push(account);
}

let account_errors = client.create_accounts(&accounts_batch).await?;
// Error handling omitted.

// Then, load and import all transfers with their timestamps from the historical source.
let mut transfers_batch = vec![];
for (index, mut transfer) in historical_transfers.into_iter().enumerate() {
    // Set a unique and strictly increasing timestamp.
    historical_timestamp += 1;
    transfer.timestamp = historical_timestamp;

    transfer.flags = if index < transfers_batch.len() - 1 {
        tb::TransferFlags::Imported | tb::TransferFlags::Linked
    } else {
        tb::TransferFlags::Imported
    };

    transfers_batch.push(transfer);
}

let transfer_errors = client.create_transfers(&transfers_batch).await?;
// Error handling omitted.
// Since it is a linked chain, in case of any error the entire batch is rolled back and can be retried
// with the same historical timestamps without regressing the cluster timestamp.

Timeouts And Cancellation

The Client retries indefinitely and doesn’t impose any per-request timeout. Cancellation is provided as a mechanism, and the specific cancellation policy is left to the application. A Client instance can be closed at any time. On close, all in-flight requests are canceled and return an error to the caller. Even if an error is returned, a request might still be processed by the TigerBeetle server. Reliable transaction submission explains how to make transfers retry-proof using IDs for end-to-end idempotency.
Edit this page
Operating

This section is for anyone managing their own TigerBeetle cluster. While tiger beetles thrive even in the harshest conditions, there’s certainly a preferred way to handle one!

    Installing lists all the various way to get the freshest TigerBeetle binary.
    Hardware specifies the host requirements.
    Cluster specifies the overall cluster requirements and recommendations.
    Deploying spells out deployment process and its variations.
    Monitoring details how to monitor a TigerBeetle cluster.
    Upgrading explains how to move to a newer TigerBeetle version without downtime.
    Recovering explains how to repair the cluster when a replica is permanently lost.
    Change Data Capture explains how to stream data out of TigerBeetle.

Edit this page
Installing
Quick Install
Linux

curl -Lo tigerbeetle.zip https://linux.tigerbeetle.com && unzip tigerbeetle.zip
./tigerbeetle version

macOS

Windows

Latest Release

You can download prebuilt binaries for the latest release here:
	Linux 	Windows 	MacOS
x86_64 	tigerbeetle-x86_64-linux.zip 	tigerbeetle-x86_64-windows.zip 	tigerbeetle-universal-macos.zip
aarch64 	tigerbeetle-aarch64-linux.zip 	N/A 	tigerbeetle-universal-macos.zip
Past Releases

The releases page lists all past and current releases:

https://github.com/tigerbeetle/tigerbeetle/releases

TigerBeetle can be upgraded without downtime, this is documented in Upgrading.
Building from Source

Building from source is easy, but is not recommended for production deployments, as extra care is needed to ensure compatibility with clients and upgradability. Refer to the internal documentation for compilation instructions.
Client Libraries

Client libraries for .NET, Go, Java, Node.js, and Python are published to the respective package repositories, see Clients.
Edit this page
Hardware

TigerBeetle is designed to operate and provide more than adequate performance even on commodity hardware.
Storage

Local NVMe drives are highly recommended for production deployments, and there’s no requirement for RAID.

In cloud or more complex deployments, remote block storage (e.g., EBS, NVMe-oF) may be used but will be slower and care must be taken to ensure independent fault domains across replicas.

Currently, TigerBeetle uses around 16TiB for 40 billion transfers. If you wish to use more capacity than a single disk, RAID 10 / RAID 0 is recommended over parity RAID levels.

The data file is created before the server is initially run and grows automatically. TigerBeetle has been more extensively tested on ext4, but ext4 only supports data files up to 16TiB. XFS is supported, but has seen less testing. TigerBeetle can also be run against the raw block device.
Memory

ECC memory is required for production deployments.

A replica requires at least 6 GiB RAM per machine. Between 16 GiB and 32 GiB or more (depending on budget) is recommended to be allocated to each replica for caching. TigerBeetle uses static allocation and will use exactly how much memory is explicitly allocated to it for caching via command line argument.
CPU

TigerBeetle requires only a single core per replica machine. TigerBeetle at present does not utilize more cores, but may in future.

It’s recommended to have at least one additional core free for the operating system.
Network

A minimum of a 1Gbps network connection is recommended.
Multitenancy

There are no restrictions on sharing a server with other tenant processes.
Edit this page
Cluster Recommendations

A TigerBeetle cluster is a set of machines each running the TigerBeetle server for strict serializability, high availability and durability. The TigerBeetle server is a single binary.

Each server operates on a single local data file.

The TigerBeetle server binary plus its single data file is called a replica.

A cluster guarantees strict serializability, the highest level of consistency, by automatically electing a primary replica to order and backup transactions across replicas in the cluster.
Fault Tolerance

The optimal, recommended size for any production cluster is 6 replicas.

Given a cluster of 6 replicas:

    4/6 replicas are required to elect a new primary if the old primary fails.
    A cluster remains highly available (able to process transactions), preserving strict serializability, provided that at least 3/6 machines have not failed (provided that the primary has not also failed) or provided that at least 4/6 machines have not failed (if the primary also failed and a new primary needs to be elected).
    A cluster preserves durability (surviving, detecting, and repairing corruption of any data file) provided that the cluster remains available. If machines go offline temporarily and the cluster becomes available again later, the cluster will be able to repair data file corruption once availability is restored.
    A cluster will correctly remain unavailable if too many machine failures have occurred to preserve data. In other words, TigerBeetle is designed to operate correctly or else to shut down safely if safe operation with respect to strict serializability is no longer possible due to permanent data loss.

Geographic Fault Tolerance

All 6 replicas may be within the same data center (zero geographic fault tolerance), or spread across 2 or more data centers, availability zones or regions (“sites”) for geographic fault tolerance.

For mission critical availability, the optimal number of sites is 3, since each site would then contain 2 replicas so that the loss of an entire site would not impair the availability of the cluster.

Sites should preferably be within a few milliseconds of each other, since each transaction must be replicated across sites before being committed.
Hardware Fault Tolerance

It is important to ensure independent fault domains for each replica’s data file, that each replica’s data file is stored on a separate disk (required), machine (required), rack (recommended), data center (recommended) etc.
Edit this page
Deploying

TigerBeetle is a single, statically linked binary without external dependencies, so the overall deployment procedure is simple:

    Get the tigerbeetle binary onto each of the cluster’s machines (see Installing).
    Format the data files, specifying cluster id, replica count, and replica index.
    Start replicas, specifying path to the data file and addresses of all replicas in the cluster.

Here’s how to deploy a three replica cluster running on a single machine:

curl -Lo tigerbeetle.zip https://linux.tigerbeetle.com && unzip tigerbeetle.zip && ./tigerbeetle version
./tigerbeetle format --cluster=0 --replica-count=3 --replica=0 ./0_0.tigerbeetle
./tigerbeetle format --cluster=0 --replica-count=3 --replica=1 ./0_1.tigerbeetle
./tigerbeetle format --cluster=0 --replica-count=3 --replica=2 ./0_2.tigerbeetle

./tigerbeetle start --addresses=127.0.0.1:3000,127.0.0.1:3001,127.0.0.1:3002 ./0_0.tigerbeetle &
./tigerbeetle start --addresses=127.0.0.1:3000,127.0.0.1:3001,127.0.0.1:3002 ./0_1.tigerbeetle &
./tigerbeetle start --addresses=127.0.0.1:3000,127.0.0.1:3001,127.0.0.1:3002 ./0_2.tigerbeetle &

Here’s what the arguments mean:

    --cluster specifies a globally unique 128 bit cluster ID. It is recommended to use a random number for a cluster id, cluster ID 0 is reserved for testing.
    --replica-count specifies the size of the cluster. In the current version of TigerBeetle, cluster size can not be changed after creation, but this limitation will be lifted in the future.
    --replica is a zero-based index of the current replica. While --cluster and --replica-count arguments must match across all replicas of the cluster, --replica arguments must be unique.
    ./0_0.tigerbeetle is a path to the data file. It doesn’t matter how you name it, but the suggested naming schema is ${CLUSTER_ID}_${REPLICA_INDEX}.tigerbeetle.
    --addresses specify IP addresses of all the replicas in the cluster. The order of addresses must correspond to the order of replicas. In particular, the --addresses argument must be the same for all replicas and all clients, and the address at the replica index must correspond to replica’s own address.

Production deployment differs in three aspects (see Cluster Recommendations):

    Each replica runs on a dedicated machine.
    Six replicas are used rather than three.
    There’s a supervisor process to restart a replica process after a crash.

Deployment Recipes

We have recipes for some commonly used deployment tools:

    systemd
    Docker
    Managed

Edit this page
Deploying with systemd

The following includes an example systemd unit for running TigerBeetle with Linux systems that use systemd. The unit is configured to start a single-node cluster, so you may need to adjust it for other cluster configurations.
tigerbeetle.service

[Unit]
Description=TigerBeetle Replica
Documentation=https://docs.tigerbeetle.com/
After=network-online.target
Wants=network-online.target systemd-networkd-wait-online.service

[Service]
AmbientCapabilities=CAP_IPC_LOCK

Environment=TIGERBEETLE_CACHE_GRID_SIZE=1GiB
Environment=TIGERBEETLE_ADDRESSES=3001
Environment=TIGERBEETLE_REPLICA_COUNT=1
Environment=TIGERBEETLE_REPLICA_INDEX=0
Environment=TIGERBEETLE_CLUSTER_ID=0
Environment=TIGERBEETLE_DATA_FILE=%S/tigerbeetle/0_0.tigerbeetle

DevicePolicy=closed
DynamicUser=true
LockPersonality=true
ProtectClock=true
ProtectControlGroups=true
ProtectHome=true
ProtectHostname=true
ProtectKernelLogs=true
ProtectKernelModules=true
ProtectKernelTunables=true
ProtectProc=noaccess
ProtectSystem=strict
RestrictAddressFamilies=AF_INET AF_INET6
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true

StateDirectory=tigerbeetle
StateDirectoryMode=700

Type=exec
ExecStart=/usr/local/bin/tigerbeetle start --cache-grid=${TIGERBEETLE_CACHE_GRID_SIZE} --addresses=${TIGERBEETLE_ADDRESSES} ${TIGERBEETLE_DATA_FILE}

[Install]
WantedBy=multi-user.target

Adjusting

You can adjust multiple aspects of this systemd service. Each specific adjustment is listed below with instructions.

It is not recommended to adjust some values directly in the service file. When this is the case, the instructions will ask you to instead use systemd’s drop-in file support. Here’s how to do that:

    Install the service unit in systemd (usually by adding it to /etc/systemd/system).
    Create a drop-in file to override the environment variables. Run systemctl edit tigerbeetle.service. This will bring you to an editor with instructions.
    Add your overrides. Example:

    [Service]
    Environment=TIGERBEETLE_CACHE_GRID_SIZE=4GiB
    Environment=TIGERBEETLE_ADDRESSES=0.0.0.0:3001

Pre-start script

You can place the following script in /usr/local/bin. This script is responsible for ensuring that a replica data file exists. It will create a data file if it doesn’t exist.
tigerbeetle-pre-start.sh

#!/bin/sh
set -eu

if ! test -e "${TIGERBEETLE_DATA_FILE}"; then
  /usr/local/bin/tigerbeetle format --cluster="${TIGERBEETLE_CLUSTER_ID}" --replica="${TIGERBEETLE_REPLICA_INDEX}" --replica-count="${TIGERBEETLE_REPLICA_COUNT}" "${TIGERBEETLE_DATA_FILE}"
fi

The script assumes that /bin/sh exists and points to a POSIX-compliant shell, and the test utility is either built-in or in the script’s search path. If this is not the case, adjust the script’s shebang.

Add the following line to tigerbeetle.service before ExecStart.

ExecStartPre=/usr/local/bin/tigerbeetle-pre-start.sh

The service then executes the tigerbeetle-pre-start.sh script before starting TigerBeetle.
TigerBeetle executable

The tigerbeetle executable is assumed to be installed in /usr/local/bin. If this is not the case, adjust both tigerbeetle.service and tigerbeetle-pre-start.sh to use the correct location.
Environment variables

This service uses environment variables to provide default values for a simple single-node cluster. To configure a different cluster structure, or a cluster with different values, adjust the values in the environment variables. It is not recommended to change these default values directly in the service file, because it may be important to revert to the default behavior later. Instead, use systemd’s drop-in file support.
State directory and replica data file path

This service configures a state directory, which means that systemd will make sure the directory is created before the service starts, and the directory will have the correct permissions. This is especially important because the service uses systemd’s dynamic user capabilities. systemd forces the state directory to be in /var/lib, which means that this service will have its replica data file at /var/lib/tigerbeetle/. It is not recommended to adjust the state directory directly in the service file, because it may be important to revert to the default behavior later. Instead, use systemd’s drop-in file support. If you do so, remember to also adjust the TIGERBEETLE_DATA_FILE environment variable, because it also hardcodes the tigerbeetle state directory value.

Due to systemd’s dynamic user capabilities, the replica data file path will not be owned by any existing user of the system.
Hardening configurations

Some hardening configurations are enabled for added security when running the service. It is not recommended to change these, since they have additional implications on all other configurations and values defined in this service file. If you wish to change those, you are expected to understand those implications and make any other adjustments accordingly.
Development mode

The service was created assuming it’ll be used in a production scenario.

In case you want to use this service for development as well, you may need to adjust the ExecStart line to include the --development flag if your development environment doesn’t support Direct IO, or if you require smaller cache sizes and/or batch sizes due to memory constraints.
Memory Locking

TigerBeetle requires RLIMIT_MEMLOCK to be set high enough to:

    initialize io_uring, which requires memory shared with the kernel to be locked, as well as
    lock all allocated memory, and so prevent the kernel from swapping any pages to disk, which would not only affect performance but also bypass TigerBeetle’s storage fault-tolerance.

If the required memory cannot be locked, then the environment should be modified either by (in order of preference):

    giving the local tigerbeetle binary the CAP_IPC_LOCK capability (sudo setcap "cap_ipc_lock=+ep" ./tigerbeetle), or
    raising the global memlock value under /etc/security/limits.conf, or else
    disabling swap (io_uring may still require an RLIMIT increase).

Memory locking is disabled for development environments when using the --development flag.

For Linux running under Docker, refer to Allowing MEMLOCK.
Edit this page
Docker

TigerBeetle can be run using Docker. However, it is not recommended.

TigerBeetle is distributed as a single, small, statically-linked binary. It should be easy to run directly on the target machine. Using Docker as an abstraction adds complexity while providing relatively little in this case.
Image

The Docker image is available from the GitHub Container Registry:

https://github.com/tigerbeetle/tigerbeetle/pkgs/container/tigerbeetle
Format the Data File

When using Docker, the data file must be mounted as a volume:

docker run --security-opt seccomp=unconfined \
     -v $(pwd)/data:/data ghcr.io/tigerbeetle/tigerbeetle \
    format --cluster=0 --replica=0 --replica-count=1 /data/0_0.tigerbeetle

info(io): creating "0_0.tigerbeetle"...
info(io): allocating 660.140625MiB...

Run the Server

docker run -it --security-opt seccomp=unconfined \
    -p 3000:3000 -v $(pwd)/data:/data ghcr.io/tigerbeetle/tigerbeetle \
    start --addresses=0.0.0.0:3000 /data/0_0.tigerbeetle

info(io): opening "0_0.tigerbeetle"...
info(main): 0: cluster=0: listening on 0.0.0.0:3000

Run a Multi-Node Cluster Using Docker Compose

Format the data file for each replica:

docker run --security-opt seccomp=unconfined -v $(pwd)/data:/data ghcr.io/tigerbeetle/tigerbeetle format --cluster=0 --replica=0 --replica-count=3 /data/0_0.tigerbeetle
docker run --security-opt seccomp=unconfined -v $(pwd)/data:/data ghcr.io/tigerbeetle/tigerbeetle format --cluster=0 --replica=1 --replica-count=3 /data/0_1.tigerbeetle
docker run --security-opt seccomp=unconfined -v $(pwd)/data:/data ghcr.io/tigerbeetle/tigerbeetle format --cluster=0 --replica=2 --replica-count=3 /data/0_2.tigerbeetle

Note that the data file stores which replica in the cluster the file belongs to.

Then, create a docker-compose.yml file:

version: "3.7"

##
# Note: this example might only work with linux + using `network_mode:host` because of 2 reasons:
#
# 1. When specifying an internal docker network, other containers are only available using dns based routing:
#    e.g. from tigerbeetle_0, the other replicas are available at `tigerbeetle_1:3002` and
#    `tigerbeetle_2:3003` respectively.
#
# 2. Tigerbeetle performs some validation of the ip address provided in the `--addresses` parameter
#    and won't let us specify a custom domain name.
#
# The workaround for now is to use `network_mode:host` in the containers instead of specifying our
# own internal docker network
##

services:
  tigerbeetle_0:
    image: ghcr.io/tigerbeetle/tigerbeetle
    command: "start --addresses=0.0.0.0:3001,0.0.0.0:3002,0.0.0.0:3003 /data/0_0.tigerbeetle"
    network_mode: host
    volumes:
      - ./data:/data
    security_opt:
      - "seccomp=unconfined"

  tigerbeetle_1:
    image: ghcr.io/tigerbeetle/tigerbeetle
    command: "start --addresses=0.0.0.0:3001,0.0.0.0:3002,0.0.0.0:3003 /data/0_1.tigerbeetle"
    network_mode: host
    volumes:
      - ./data:/data
    security_opt:
      - "seccomp=unconfined"

  tigerbeetle_2:
    image: ghcr.io/tigerbeetle/tigerbeetle
    command: "start --addresses=0.0.0.0:3001,0.0.0.0:3002,0.0.0.0:3003 /data/0_2.tigerbeetle"
    network_mode: host
    volumes:
      - ./data:/data
    security_opt:
      - "seccomp=unconfined"

And run it:

docker-compose up

docker-compose up
Starting tigerbeetle_0   ... done
Starting tigerbeetle_2   ... done
Recreating tigerbeetle_1 ... done
Attaching to tigerbeetle_0, tigerbeetle_2, tigerbeetle_1
tigerbeetle_1    | info(io): opening "0_1.tigerbeetle"...
tigerbeetle_2    | info(io): opening "0_2.tigerbeetle"...
tigerbeetle_0    | info(io): opening "0_0.tigerbeetle"...
tigerbeetle_0    | info(main): 0: cluster=0: listening on 0.0.0.0:3001
tigerbeetle_2    | info(main): 2: cluster=0: listening on 0.0.0.0:3003
tigerbeetle_1    | info(main): 1: cluster=0: listening on 0.0.0.0:3002
tigerbeetle_0    | info(message_bus): connected to replica 1
tigerbeetle_0    | info(message_bus): connected to replica 2
tigerbeetle_1    | info(message_bus): connected to replica 2
tigerbeetle_1    | info(message_bus): connection from replica 0
tigerbeetle_2    | info(message_bus): connection from replica 0
tigerbeetle_2    | info(message_bus): connection from replica 1
tigerbeetle_0    | info(clock): 0: system time is 83ns ahead
tigerbeetle_2    | info(clock): 2: system time is 83ns ahead
tigerbeetle_1    | info(clock): 1: system time is 78ns ahead

... and so on ...

Troubleshooting
error: PermissionDenied

If you see this error at startup, it is likely because you are running Docker 25.0.0 or newer, which blocks io_uring by default. Set --security-opt seccomp=unconfined to fix it.
exited with code 137

If you see this error without any logs from TigerBeetle, it is likely that the Linux OOMKiller is killing the process. If you are running Docker inside a virtual machine (such as is required on Docker or Podman for macOS), try increasing the virtual machine memory limit.

Alternatively, in a development environment, you can lower the size of the cache so TigerBeetle uses less memory. For example, set --cache-grid=256MiB when running tigerbeetle start.
Debugging panics

If TigerBeetle panics and you can reproduce the panic, you can get a better stack trace by switching to a debug image (by using the :debug Docker image tag).

docker run -p 3000:3000 -v $(pwd)/data:/data ghcr.io/tigerbeetle/tigerbeetle:debug \
    start --addresses=0.0.0.0:3000 /data/0_0.tigerbeetle

On MacOS
error: SystemResources

If you get error: SystemResources when running TigerBeetle in Docker on macOS, the container may be blocking TigerBeetle from locking memory, which is necessary both for io_uring and to prevent the kernel’s use of swap from bypassing TigerBeetle’s storage fault tolerance.
Allowing MEMLOCK

To raise the memory lock limits under Docker, execute one of the following:

    Run docker run with --cap-add IPC_LOCK
    Run docker run with --ulimit memlock=-1:-1
    Or modify the defaults in $HOME/.docker/daemon.json and restart the Docker for Mac application:

{
  ... other settings ...
  "default-ulimits": {
    "memlock": {
      "Hard": -1,
      "Name": "memlock",
      "Soft": -1
    }
  },
  ... other settings ...
}

If you are running TigerBeetle with Docker Compose, you will need to add the IPC_LOCK capability like this:

... rest of docker-compose.yml ...

services:
  tigerbeetle_0:
    image: ghcr.io/tigerbeetle/tigerbeetle
    command: "start --addresses=0.0.0.0:3001,0.0.0.0:3002,0.0.0.0:3003 /data/0_0.tigerbeetle"
    network_mode: host
    cap_add:       # HERE
      - IPC_LOCK   # HERE
    volumes:
      - ./data:/data

... rest of docker-compose.yml ...

See https://github.com/tigerbeetle/tigerbeetle/issues/92 for discussion.
Edit this page
Fully Managed

For enterprises committed to excellence, TigerBeetle’s world-class team provides:

    fully managed cross-cloud deployments with automated disaster recovery;
    24/7 responsiveness with proactive monitoring.

Dedicated expertise from senior engineers ensures success (and sleep at night) at every step – from chart of accounts design and proof-of-concept, through production to monster scale. Contact us at sales@tigerbeetle.com to set up a call.

Are you a startup? Check out the Startup Program.
Edit this page
Monitoring

TigerBeetle supports emitting metrics via StatsD, and uses the DogStatsD format for tags.

This requires a StatsD compatible agent running locally. The Datadog Agent works out of the box with its default configuration, as does Telegraf’s StatsD plugin, with datadog_extensions enabled.

You can enable emitting metrics by adding the following CLI flags to each replica, depending on your deployment method:

--experimental --statsd=127.0.0.1:8125

The --statsd argument must be specified as an IP:Port address (IPv4 or IPv6). DNS names are not currently supported.

All TigerBeetle metrics are namespaced under tb. and are tagged with cluster (the cluster ID specified at format time) and replica (the replica index). Specific metrics might have additional tags. You can see a full list of metrics and cardinality by running tigerbeetle inspect metrics.
Specific Metrics
Overall status

The replica_status metric corresponds to the overall status of the replica. If it’s anything other than 0, it should be alerted on as it indicates a non-normal status. The full values are:
Value 	Status 	Explanation
0 	normal 	The replica is functioning normally.
1 	view_change 	The replica is doing a view change.
2 	recovering 	The replica is recovering. Usually, this will be present on startup before immediately transitioning to normal.
3 	recovering_head 	The replica’s persistent state is corrupted, and it can’t participate in consensus. It will try and recover from the remainder of the cluster.
State sync status

The replica_sync_stage metric corresponds to the state sync stage. If this is anything other than 0, the replica is undergoing state sync and should be alerted on.
Operations timing

The replica_request timing metric can help inform how long requests are taking. This is tagged with the operation type (e.g., create_accounts) and is the closest measure of how long a request takes end to end, from the replica’s point of view.

It’s recommended to additionally add metrics around your TigerBeetle client code, to measure the full request latency, including things like network delay which aren’t captured here.
Cache monitoring and sizing

The grid_cache_hits and grid_cache_misses metrics can help inform if your grid cache (--cache-grid) is sized too small for your workload.
System Monitoring

In addition to TigerBeetle’s own metrics, it’s recommended to monitor and alert on a few additional system level metrics. These are:

    Disk space used, on the path that has the TigerBeetle data file.
    NTP clock sync status.
    Memory utilization: once started, TigerBeetle will use a fixed amount of memory and not change. A change in memory utilization can indicate a problem with other processes on the server.
    CPU utilization: TigerBeetle will use at most a single core at present. CPU utilization exceeding a single core can indicate a problem with other processes on the server.

While a specific alerting threshold is hard to define for the following, they are useful to monitor to help diagnose problems:

    Network bandwidth utilization.
    Disk bandwidth utilization.

Edit this page
Upgrading

TigerBeetle guarantees storage stability and provides forward upgradeability. In other words, data files created by a particular past version of TigerBeetle can be migrated to any future version of TigerBeetle.

Migration is automatic and the upgrade process is usually as simple as:

    Upgrade the replicas, by replacing the ./tigerbeetle binary with a newer version on each replica (they will restart automatically when needed).
    Upgrade the clients, by updating the corresponding client libraries, recompiling and redeploying as usual.

There’s no need to stop the cluster for upgrades, and the client upgrades can be rolled out gradually as any change to the client code might.

NOTE: if you are upgrading from 0.15.3 (the first stable version), the upgrade procedure is more involved, see the release notes for 0.15.4.
API Stability

At the moment, TigerBeetle doesn’t guarantee complete API stability, subscribe to the tracking issue #2231 to receive notifications about breaking changes!
Planning for upgrades

When upgrading TigerBeetle, each release specifies two important versions:

    the oldest release that can be upgraded from and,
    the oldest supported client version.

It’s critical to make sure that the release you intend to upgrade from is supported by the release you’re upgrading to. This is a hard requirement, but also a hard guarantee: if you wish to upgrade to 0.15.20 which says it supports down to 0.15.5, 0.15.5 will work and 0.15.4 will not. You will have to perform multiple upgrades in this case.

The upgrade process involves first upgrading the replicas, followed by upgrading the clients. The client version cannot be newer than the replica version, and will fail with an error message if so. Provided the supported version ranges overlap, coordinating the upgrade between clients and replicas is not required.

Upgrading causes a short period of unavailability as the replicas restart. This is on the order of 5 seconds, and will show up as a latency spike on requests. The TigerBeetle clients will internally retry any requests during the period.

Even though this period is short, scheduling a maintenance window for upgrades is still recommended, for an extra layer of safety.

Any special instructions, like that when upgrading from 0.15.3 to 0.15.4, will be explicitly mentioned in the changelog and release notes.

Additionally, subscribe to this tracking issue to be notified when there are breaking API/behavior changes that are visible to the client.
Upgrading binary-based installations

If TigerBeetle is installed under /usr/bin/tigerbeetle, and you wish to upgrade to 0.15.4:

# SSH to each replica, in no particular order:
cd /tmp
wget https://github.com/tigerbeetle/tigerbeetle/releases/download/0.15.4/tigerbeetle-x86_64-linux.zip
unzip tigerbeetle-x86_64-linux.zip

# Put the binary on the same file system as the target, so mv is atomic.
mv tigerbeetle /usr/bin/tigerbeetle-new

mv /usr/bin/tigerbeetle /usr/bin/tigerbeetle-old
mv /usr/bin/tigerbeetle-new /usr/bin/tigerbeetle

# Restart TigerBeetle. Only required when upgrading from 0.15.3.
# Otherwise, it will detect new versions are available and coordinate the upgrade itself.
systemctl restart tigerbeetle # or, however you are managing TigerBeetle.

Upgrading Docker-based installations

If you’re running TigerBeetle inside Kubernetes or Docker, update the tag that is pointed to the release you wish to upgrade to. Before beginning, it’s strongly recommended to have a rolling deploy strategy set up.

For example:

image: ghcr.io/tigerbeetle/tigerbeetle:0.15.3

becomes

image: ghcr.io/tigerbeetle/tigerbeetle:0.15.4

Due to the way upgrades work internally, this will restart with the new binary available, but still running the older version. TigerBeetle will then coordinate the actual upgrade when all replicas are ready and have the latest version available.
Upgrading clients

Update your language’s specific package management, to reference the same version of the TigerBeetle client:
.NET

dotnet add package tigerbeetle --version 0.15.4

Go

go mod edit -require github.com/tigerbeetle/tigerbeetle-go@v0.15.4

Java

Edit your pom.xml:

    <dependency>
        <groupId>com.tigerbeetle</groupId>
        <artifactId>tigerbeetle-java</artifactId>
        <version>0.15.4</version>
    </dependency>

Node.js

npm install --save-exact tigerbeetle-node@0.15.4

Python

pip install tigerbeetle==0.15.4

Troubleshooting
Upgrading to a newer version with incompatible clients

If a release of TigerBeetle no longer supports the client version you’re using, it’s still possible to upgrade, with two options:

    Upgrade the replicas to the latest version. In this case, the clients will stop working for the duration of the upgrade and unavailability will be extended.
    Upgrade the replicas to the latest release that supports the client version in use, then upgrade the clients to that version. Repeat this until you’re on the latest release.

Edit this page
Recovering

If a replica’s data file is permanently lost (for example, if the SSD fails) then a new data file must be reformatted to restore the cluster.

The tigerbeetle format command must not be used for this purpose. The issue is that tigerbeetle format would create a replica that believes that any operation that it hasn’t seen can be safely nack’d – unaware of the promises it made which were lost with the old data file. This could cause the cluster to lose committed data.

Instead of tigerbeetle format, use the tigerbeetle recover command (see below).

Note that tigerbeetle recover requires the cluster to be healthy and capable of view-changing.

Once tigerbeetle recover succeeds, run tigerbeetle start as normal. At this point, the new replica will rejoin the cluster and state sync to repair itself.
Example

./tigerbeetle recover \
  --cluster=0 \
  --addresses=127.0.0.1:3000,127.0.0.1:3001,127.0.0.1:3002 \
  --replica=2 \
  --replica-count=3 \
  ./0_2.tigerbeetle

(--addresses should include an address for the recovering replica, but it can be any address as it is just a placeholder.)
Edit this page
Change Data Capture

TigerBeetle can stream changes (transfers and balance updates) to message queues using the AMQP 0.9.1 protocol, which is compatible with RabbitMQ and various other message brokers.

See Installing for instructions on how to deploy the TigerBeetle binary.

Here’s how to start the CDC job:

./tigerbeetle amqp --addresses=127.0.0.1:3000,127.0.0.1:3001,127.0.0.1:3002 --cluster=0 \
    --host=127.0.0.1 \
    --vhost=/ \
    --user=guest --password=guest \
    --publish-exchange=tigerbeetle

Here what the arguments mean:

    --addresses specify IP addresses of all the replicas in the cluster. The order of addresses must correspond to the order of replicas.

    --cluster specifies a globally unique 128 bit cluster ID.

    --host the AMQP host address in the format ip:port.
    Both IPv4 and IPv6 addresses are supported. If port is omitted, the AMQP default 5672 is used.
    Multiple addresses (for clustered environments) and DNS names are not supported.
    The operator must resolve the IP address of the preferred/reachable server.
    The CDC job will exit with a non-zero code in case of any connectivity or configuration issue with the AMQP server.

    --vhost the AMQP virtual host name.

    --user the AMQP username.

    --password the AMQP password.
    Only PLAIN authentication is supported.

    --publish-exchange the exchange name.
    Must be a pre-existing exchange provided by the operator.
    Optional. May be omitted if --publish-routing-key is present.

    --publish-routing-key the routing key used in combination with the exchange.
    Optional. May be omitted if publish-exchange is present.

    --event-count-max the maximum number of events fetched from TigerBeetle and published to the AMQP server per batch.
    Optional. Defaults to 2730 if omitted.

    --idle-interval-ms the time interval in milliseconds to wait before querying again when the last query returned no events.
    Optional. Defaults to 1000 ms if omitted.

    --requests-per-second-limit throttles the maximum number of requests per second made to TigerBeetle.
    Must be greater than zero.
    Optional. No limit if omitted.

    --amqp-timeout-seconds the maximum time, in seconds, to wait for a reply from the AMQP server. If exceeded, the process exits with a non-zero code.
    Must be greater than 0.
    Optional. Defaults to 30 seconds if omitted.

    --tigerbeetle-timeout-seconds the maximum time, in seconds, to wait for a reply from the TigerBeetle cluster. If exceeded, the process exits with a non-zero code.
    Must be greater than 0.
    Optional. Defaults to 30 seconds if omitted.

    --timestamp-last overrides the last published timestamp, resuming from this point.
    This is a TigerBeetle timestamp with nanosecond precision.
    Optional. If omitted, the last acknowledged timestamp is used.

Message content:

Messages are published with custom headers, allowing users to implement routing and filtering rules.

Message headers:
Key 	AMQP data type 	Description
event_type 	string 	The event type.
ledger 	long_long_int 	The ledger of the transfer and accounts.
transfer_code 	long_int 	The transfer code.
debit_account_code 	long_int 	The debit account code.
credit_account_code 	long_int 	The credit account code.
app_id 	string 	Constant tigerbeetle.
content_type 	string 	Constant application/json
delivery_mode 	short_short_uint 	Constant 2 which means persistent.
timestamp 	timestamp 	The event timestamp.¹

    ¹ AMQP timestamps are represented in seconds, so TigerBeetle timestamps are truncated.
    Use the timestamp field in the message body for full nanosecond precision.

Message body:

Each event published contains information about the transfer and the accounts involved.

    type: The type of event.
    One of single_phase, two_phase_pending, two_phase_posted, two_phase_voided or two_phase_expired.
    See the Two-Phase Transfers for more details.

    timestamp: The event timestamp.
    Usually, it’s the same as the transfer’s timestamp, except when event_type == 'two_phase_expired' when it’s the expiry timestamp.

    ledger: The ledger code.

    transfer: Full details of the transfer.
    For two_phase_expired events, it’s the pending transfer that was reverted.

    debit_account: Full details of the debit account, with the balance as of the time of the event.

    credit_account: Full details of the credit account, with the balance as of the time of the event.

The message body is encoded as a UTF-8 JSON without line breaks or spaces. Long integers such as u128 and u64 are encoded as JSON strings to improve interoperability.

Here is a formatted example (with indentation and line breaks) for readability.

{
  "timestamp": "1745328372758695656",
  "type": "single_phase",
  "ledger": 2,
  "transfer": {
    "id": 9082709,
    "amount": 3794,
    "pending_id": 0,
    "user_data_128": "79248595801719937611592367840129079151",
    "user_data_64": "13615171707598273871",
    "user_data_32": 3229992513,
    "timeout": 0,
    "code": 20295,
    "flags": 0,
    "timestamp": "1745328372758695656"
  },
  "debit_account": {
    "id": 3750,
    "debits_pending": 0,
    "debits_posted": 8463768,
    "credits_pending": 0,
    "credits_posted": 8861179,
    "user_data_128": "118966247877720884212341541320399553321",
    "user_data_64": "526432537153007844",
    "user_data_32": 4157247332,
    "code": 1,
    "flags": 0,
    "timestamp": "1745328270103398016"
  },
  "credit_account": {
    "id": 6765,
    "debits_pending": 0,
    "debits_posted": 8669204,
    "credits_pending": 0,
    "credits_posted": 8637251,
    "user_data_128": "43670023860556310170878798978091998141",
    "user_data_64": "12485093662256535374",
    "user_data_32": 1924162092,
    "code": 1,
    "flags": 0,
    "timestamp": "1745328270103401031"
  }
}

Guarantees

TigerBeetle guarantees at-least-once semantics when publishing to message brokers, and makes a best effort to prevent duplicate messages. However, during crash recovery, the CDC job may replay unacknowledged messages that could have been already delivered to consumers.

It is the consumer’s responsibility to perform idempotency checks when processing messages.
Upgrading

The CDC job requires TigerBeetle cluster version 0.16.43 or greater.

The same upgrade planning recommended for clients applies to the CDC job. The CDC job version must not be newer than the cluster version, and if so will fail with an error message.

Any transactions originally created by TigerBeetle versions before 0.16.29 have the following limitations for CDC processing:

    Events of type two_phase_expired are not supported.
    Only transfers where both the debit and credit accounts have the flags.history enabled are visible to CDC.

Transactions committed after version 0.16.29 are fully compatible with CDC and do not require the history flag.
CDC to RabbitMQ (AMQP 0.9.1) in production
High Availability

The CDC job is single instance. Starting a second tigerbeetle amqp with the same cluster_id will exit with a non-zero exit code. For high availability, the CDC job could be monitored for crashes and restarted in case a failure.

The CDC job itself is stateless, and will resume from the last event acknowledged by RabbitMQ, however it may replay events that weren’t acknowledged but received by the exchange.
TLS Support

For secure AMQPS connections, we recommend using a TLS Tunnel to wrap the connection between TigerBeetle and RabbitMQ.
Event Replay

By default, when the CDC job starts, it resumes from the timestamp of the last acknowledged event in RabbitMQ. This can be overridden to using --timestamp-last. For example, --timestamp-last=0 will replay all events.
Edit this page
Reference

Like the Coding section, the reference is aimed at programmers building applications on top of TigerBeetle. While Coding provides a series of topical guides, Reference exhaustively documents every single aspect of TigerBeetle. Any answer can be found here, but it might take some digging!

    Client Sessions
    Account
    Transfer
    AccountBalance
    AccountFilter
    QueryFilter
    Requests
        create_accounts
        create_transfers
        lookup_accounts
        lookup_transfers
        get_account_balances
        get_account_transfers
        query_accounts
        query_transfers

Edit this page
Client Sessions

A client session is a sequence of requests and replies sent between a client and a cluster.

A client session may have at most one in-flight request — i.e. at most one unique request on the network for which a reply has not been received. This simplifies consistency and allows the cluster to statically guarantee capacity in its incoming message queue. Additional requests from the application are queued by the client, to be dequeued and sent when their preceding request receives a reply.

Similar to other databases, TigerBeetle has a hard limit on the number of concurrent client sessions. To maximize throughput, users are encouraged to minimize the number of concurrent clients and batch as many events as possible per request.
Lifecycle

A client session begins when a client registers itself with the cluster.

    Each client session has a unique identifier (“client id”) — an ephemeral random 128-bit id.
    The client sends a special “register” message which is committed by the cluster, at which point the client is “registered” — once it receives the reply, it may begin sending requests.
    Client registration is handled automatically by the TigerBeetle client implementation when the client is initialized, before it sends its first request.
    When a client restarts (for example, the application service running the TigerBeetle client is restarted) it does not resume its old session — it starts a new session, with a new (random) client id.

A client session ends when either:

    the client session is evicted, or
    the client terminates

— whichever occurs first.
Eviction

When a client session is registering and the number of active sessions in the cluster is already at the cluster’s concurrent client session limit (config.clients_max, 64 by default), an existing client session must be evicted to make space for the new session.

    After a session is evicted by the cluster, no future requests from that session will ever execute.
    The evicted session is chosen as the session that committed a request the longest time ago.

The cluster sends a message to notify the evicted session that it has ended. Typically the evicted client is no longer active (already terminated), but if it is active, the eviction message causes it to self-terminate, bubbling up to the application as an session evicted error.

If active clients are terminating with session evicted errors, it most likely indicates that the application is trying to run too many concurrent clients. For performance reasons, it is recommended to batch as many events as possible into each request sent by each client.
Retries

A client session will automatically retry a request until either:

    the client receives a corresponding reply from the cluster, or
    the client is terminated.

Unlike most database or RPC clients:

    the TigerBeetle client will never time out
    the TigerBeetle client has no retry limits
    the TigerBeetle client does not surface network errors

With TigerBeetle’s strict consistency model, surfacing these errors at the client/application level would be misleading. An error would imply that a request did not execute, when that is not known:

    A request delayed by the network could execute after its timeout.
    A reply delayed by the network could execute before its timeout.

Guarantees

    A client session may have at most one in-flight request.
    A client session reads its own writes, meaning that read operations that happen after a given write operation will observe the effects of the write.
    A client session observes writes in the order that they occur on the cluster.
    A client session observes debits_posted and credits_posted as monotonically increasing. That is, a client session will never see credits_posted or debits_posted decrease.
    A client session never observes uncommitted updates.
    A client session never observes a broken invariant (e.g. flags.credits_must_not_exceed_debits or flags.linked).
    Multiple client sessions may receive replies out of order relative to one another. For example, if two clients submit requests around the same time, the client whose request is committed first might receive the reply later.
    A client session can consider a request executed when it receives a reply for the request.
    If a client session is terminated and restarts, it is guaranteed to see the effects of updates for which the corresponding reply was received prior to termination.
    If a client session is terminated and restarts, it is not guaranteed to see the effects of updates for which the corresponding reply was not received prior to the restart. Those updates may occur at any point in the future, or never. Handling application crash recovery safely requires using ids to idempotently retry events.

Edit this page
Account

An Account is a record storing the cumulative effect of committed transfers.
Updates

Account fields cannot be changed by the user after creation. However, debits and credits fields are updated by TigerBeetle as transfers move money to and from an account.
Deletion

Accounts cannot be deleted after creation. This provides a strong guarantee for an audit trail – and the account record is only 128 bytes.

If an account is no longer in use, you may want to zero out its balance.
Guarantees

    Accounts are immutable. They are never modified once they are successfully created (excluding balance fields, which are modified by transfers).
    There is at most one Account with a particular id.
    The sum of all accounts’ debits_pending equals the sum of all accounts’ credits_pending.
    The sum of all accounts’ debits_posted equals the sum of all accounts’ credits_posted.

Fields
id

This is a unique, client-defined identifier for the account.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    Must not be zero or 2^128 - 1 (the highest 128-bit unsigned integer)
    Must not conflict with another account in the cluster

See the id section in the data modeling doc for more recommendations on choosing an ID scheme.

Note that account IDs are unique for the cluster – not per ledger. If you want to store a relationship between accounts, such as indicating that multiple accounts on different ledgers belong to the same user, you should store a user ID in one of the user_data fields.
debits_pending

debits_pending counts debits reserved by pending transfers. When a pending transfer posts, voids, or times out, the amount is removed from debits_pending.

Money in debits_pending is reserved — that is, it cannot be spent until the corresponding pending transfer resolves.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    Must be zero when the account is created

debits_posted

Amount of posted debits.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    Must be zero when the account is created

credits_pending

credits_pending counts credits reserved by pending transfers. When a pending transfer posts, voids, or times out, the amount is removed from credits_pending.

Money in credits_pending is reserved — that is, it cannot be spent until the corresponding pending transfer resolves.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    Must be zero when the account is created

credits_posted

Amount of posted credits.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    Must be zero when the account is created

user_data_128

This is an optional 128-bit secondary identifier to link this account to an external entity or event.

When set to zero, no secondary identifier will be associated with the account, therefore only non-zero values can be used as query filter.

As an example, you might use a ULID that ties together a group of accounts.

For more information, see Data Modeling.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)

user_data_64

This is an optional 64-bit secondary identifier to link this account to an external entity or event.

When set to zero, no secondary identifier will be associated with the account, therefore only non-zero values can be used as query filter.

As an example, you might use this field store an external timestamp.

For more information, see Data Modeling.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)

user_data_32

This is an optional 32-bit secondary identifier to link this account to an external entity or event.

When set to zero, no secondary identifier will be associated with the account, therefore only non-zero values can be used as query filter.

As an example, you might use this field to store a timezone or locale.

For more information, see Data Modeling.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)

reserved

This space may be used for additional data in the future.

Constraints:

    Type is 4 bytes
    Must be zero

ledger

This is an identifier that partitions the sets of accounts that can transact with each other.

See data modeling for more details about how to think about setting up your ledgers.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)
    Must not be zero

code

This is a user-defined enum denoting the category of the account.

As an example, you might use codes 1000-3340 to indicate asset accounts in general, where 1001 is Bank Account and 1002 is Money Market Account and 2003 is Motor Vehicles and so on.

Constraints:

    Type is 16-bit unsigned integer (2 bytes)
    Must not be zero

flags

A bitfield that toggles additional behavior.

Constraints:

    Type is 16-bit unsigned integer (2 bytes)
    Some flags are mutually exclusive; see flags_are_mutually_exclusive.

flags.linked

This flag links the result of this account creation to the result of the next one in the request, such that they will either succeed or fail together.

The last account in a chain of linked accounts does not have this flag set.

You can read more about linked events.
flags.debits_must_not_exceed_credits

When set, transfers will be rejected that would cause this account’s debits to exceed credits. Specifically when account.debits_pending + account.debits_posted + transfer.amount > account.credits_posted.

This cannot be set when credits_must_not_exceed_debits is also set.
flags.credits_must_not_exceed_debits

When set, transfers will be rejected that would cause this account’s credits to exceed debits. Specifically when account.credits_pending + account.credits_posted + transfer.amount > account.debits_posted.

This cannot be set when debits_must_not_exceed_credits is also set.
flags.history

When set, the account will retain the history of balances at each transfer.

Note that the get_account_balances operation only works for accounts with this flag set.
flags.imported

When set, allows importing historical Accounts with their original timestamp.

TigerBeetle will not use the cluster clock to assign the timestamp, allowing the user to define it, expressing when the account was effectively created by an external event.

To maintain system invariants regarding auditability and traceability, some constraints are necessary:

    It is not allowed to mix events with the imported flag set and not set in the same batch. The application must submit batches of imported events separately.

    User-defined timestamps must be unique and expressed as nanoseconds since the UNIX epoch. No two objects can have the same timestamp, even different objects like an Account and a Transfer cannot share the same timestamp.

    User-defined timestamps must be a past date, never ahead of the cluster clock at the time the request arrives.

    Timestamps must be strictly increasing.

    Even user-defined timestamps that are required to be past dates need to be at least one nanosecond ahead of the timestamp of the last account committed by the cluster.

    Since the timestamp cannot regress, importing past events can be naturally restrictive without coordination, as the last timestamp can be updated using the cluster clock during regular cluster activity. Instead, it’s recommended to import events only on a fresh cluster or during a scheduled maintenance window.

    It’s recommended to submit the entire batch as a linked chain, ensuring that if any account fails, none of them are committed, preserving the last timestamp unchanged. This approach gives the application a chance to correct failed imported accounts, re-submitting the batch again with the same user-defined timestamps.

flags.closed

When set, the account will reject further transfers, except for voiding two-phase transfers that are still pending.

    This flag can be set during the account creation.
    This flag can also be set by sending a two-phase pending transfer with the Transfer.flags.closing_debit and/or Transfer.flags.closing_credit flags set.
    This flag can be unset by voiding the two-phase pending transfer that closed the account.

timestamp

This is the time the account was created, as nanoseconds since UNIX epoch. You can read more about Time in TigerBeetle.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)

    Must be 0 when the Account is created with flags.imported not set

    It is set by TigerBeetle to the moment the account arrives at the cluster.

    Must be greater than 0 and less than 2^63 when the Account is created with flags.imported set

Internals

If you’re curious and want to learn more, you can find the source code for this struct in src/tigerbeetle.zig. Search for const Account = extern struct {.

You can find the source code for creating an account in src/state_machine.zig. Search for fn create_account(.
Edit this page
Transfer

A transfer is an immutable record of a financial transaction between two accounts.

In TigerBeetle, financial transactions are called “transfers” instead of “transactions” because the latter term is heavily overloaded in the context of databases.

Note that transfers debit a single account and credit a single account on the same ledger. You can compose these into more complex transactions using the methods described in Currency Exchange and Multi-Debit, Multi-Credit Transfers.
Updates

Transfers cannot be modified after creation.

If a detail of a transfer is incorrect and needs to be modified, this is done using correcting transfers.
Deletion

Transfers cannot be deleted after creation.

If a transfer is made in error, its effects can be reversed using a correcting transfer.
Guarantees

    Transfers are immutable. They are never modified once they are successfully created.
    There is at most one Transfer with a particular id.
    A pending transfer resolves at most once.
    Transfer timeouts are deterministic, driven by the cluster’s timestamp.

Modes

Transfers can either be Single-Phase, where they are executed immediately, or Two-Phase, where they are first put in a Pending state and then either Posted or Voided. For more details on the latter, see the Two-Phase Transfer guide.

Fields used by each mode of transfer:
Field 	Single-Phase 	Pending 	Post-Pending 	Void-Pending
id 	required 	required 	required 	required
debit_account_id 	required 	required 	optional 	optional
credit_account_id 	required 	required 	optional 	optional
amount 	required 	required 	required 	optional
pending_id 	none 	none 	required 	required
user_data_128 	optional 	optional 	optional 	optional
user_data_64 	optional 	optional 	optional 	optional
user_data_32 	optional 	optional 	optional 	optional
timeout 	none 	optional¹ 	none 	none
ledger 	required 	required 	optional 	optional
code 	required 	required 	optional 	optional
flags.linked 	optional 	optional 	optional 	optional
flags.pending 	false 	true 	false 	false
flags.post_pending_transfer 	false 	false 	true 	false
flags.void_pending_transfer 	false 	false 	false 	true
flags.balancing_debit 	optional 	optional 	false 	false
flags.balancing_credit 	optional 	optional 	false 	false
flags.closing_debit 	optional 	true 	false 	false
flags.closing_credit 	optional 	true 	false 	false
flags.imported 	optional 	optional 	optional 	optional
timestamp 	none² 	none² 	none² 	none²

    ¹ None if flags.imported is set.
    ² Required if flags.imported is set.

Fields
id

This is a unique identifier for the transaction.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    Must not be zero or 2^128 - 1
    Must not conflict with another transfer in the cluster

See the id section in the data modeling doc for more recommendations on choosing an ID scheme.

Note that transfer IDs are unique for the cluster – not the ledger. If you want to store a relationship between multiple transfers, such as indicating that multiple transfers on different ledgers were part of a single transaction, you should store a transaction ID in one of the user_data fields.
debit_account_id

This refers to the account to debit the transfer’s amount.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    When flags.post_pending_transfer and flags.void_pending_transfer are not set:
        Must match an existing account
        Must not be the same as credit_account_id
    When flags.post_pending_transfer or flags.void_pending_transfer are set:
        If debit_account_id is zero, it will be automatically set to the pending transfer’s debit_account_id.
        If debit_account_id is nonzero, it must match the corresponding pending transfer’s debit_account_id.
    When flags.imported is set:
        The matching account’s timestamp must be less than or equal to the transfer’s timestamp.

credit_account_id

This refers to the account to credit the transfer’s amount.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    When flags.post_pending_transfer and flags.void_pending_transfer are not set:
        Must match an existing account
        Must not be the same as debit_account_id
    When flags.post_pending_transfer or flags.void_pending_transfer are set:
        If credit_account_id is zero, it will be automatically set to the pending transfer’s credit_account_id.
        If credit_account_id is nonzero, it must match the corresponding pending transfer’s credit_account_id.
    When flags.imported is set:
        The matching account’s timestamp must be less than or equal to the transfer’s timestamp.

amount

This is how much should be debited from the debit_account_id account and credited to the credit_account_id account.

Note that this is an unsigned 128-bit integer. You can read more about using debits and credits to represent positive and negative balances as well as fractional amounts and asset scales.

    When flags.balancing_debit is set, this is the maximum amount that will be debited/credited, where the actual transfer amount is determined by the debit account’s constraints.
    When flags.balancing_credit is set, this is the maximum amount that will be debited/credited, where the actual transfer amount is determined by the credit account’s constraints.
    When flags.post_pending_transfer is set, the amount posted will be:
        the pending transfer’s amount, when the posted transfer’s amount is AMOUNT_MAX
        the posting transfer’s amount, when the posted transfer’s amount is less than or equal to the pending transfer’s amount.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    When flags.void_pending_transfer is set:
        If amount is zero, it will be automatically be set to the pending transfer’s amount.
        If amount is nonzero, it must be equal to the pending transfer’s amount.
    When flags.post_pending_transfer is set:
        If amount is AMOUNT_MAX (2^128 - 1), it will automatically be set to the pending transfer’s amount.
        If amount is not AMOUNT_MAX, it must be less than or equal to the pending transfer’s amount.

Client release < 0.16.0

Examples

    For representing fractional amounts (e.g. $12.34), see Fractional Amounts.
    For balancing transfers, see Close Account.

pending_id

If this transfer will post or void a pending transfer, pending_id references that pending transfer. If this is not a post or void transfer, it must be zero.

See the section on Two-Phase Transfers for more information on how the pending_id is used.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    Must be zero if neither void nor pending transfer flag is set
    Must match an existing transfer’s id if non-zero

user_data_128

This is an optional 128-bit secondary identifier to link this transfer to an external entity or event.

When set to zero, no secondary identifier will be associated with the transfer, therefore only non-zero values can be used as query filter.

When set to zero, if flags.post_pending_transfer or flags.void_pending_transfer is set, then it will be automatically set to the pending transfer’s user_data_128.

As an example, you might generate a TigerBeetle Time-Based Identifier that ties together a group of transfers.

For more information, see Data Modeling.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)

user_data_64

This is an optional 64-bit secondary identifier to link this transfer to an external entity or event.

When set to zero, no secondary identifier will be associated with the transfer, therefore only non-zero values can be used as query filter.

When set to zero, if flags.post_pending_transfer or flags.void_pending_transfer is set, then it will be automatically set to the pending transfer’s user_data_64.

As an example, you might use this field store an external timestamp.

For more information, see Data Modeling.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)

user_data_32

This is an optional 32-bit secondary identifier to link this transfer to an external entity or event.

When set to zero, no secondary identifier will be associated with the transfer, therefore only non-zero values can be used as query filter.

When set to zero, if flags.post_pending_transfer or flags.void_pending_transfer is set, then it will be automatically set to the pending transfer’s user_data_32.

As an example, you might use this field to store a timezone or locale.

For more information, see Data Modeling.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)

timeout

This is the interval in seconds after a pending transfer’s arrival at the cluster that it may be posted or voided. Zero denotes absence of timeout.

Non-pending transfers cannot have a timeout.

Imported transfers cannot have a timeout.

TigerBeetle makes a best-effort approach to remove pending balances of expired transfers automatically:

    Transfers expire exactly at their expiry time (timestamp plus timeout converted in nanoseconds).

    Pending balances will never be removed before its expiry.

    Expired transfers cannot be manually posted or voided.

    It is not guaranteed that the pending balance will be removed exactly at its expiry.

    In particular, client requests may observe still-pending balances for expired transfers.

    Pending balances are removed in chronological order by expiry. If multiple transfers expire at the same time, then ordered by the transfer’s creation timestamp.

    If a transfer A has expiry E₁ and transfer B has expiry E₂, and E₁<E₂, if transfer B had the pending balance removed, then transfer A had the pending balance removed as well.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)
    Must be zero if flags.pending is not set
    Must be zero if flags.imported is set.

The timeout is an interval in seconds rather than an absolute timestamp because this is more robust to clock skew between the cluster and the application. (Watch this talk on Detecting Clock Sync Failure in Highly Available Systems on YouTube for more details.)
ledger

This is an identifier that partitions the sets of accounts that can transact with each other.

See data modeling for more details about how to think about setting up your ledgers.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)
    When flags.post_pending_transfer or flags.void_pending_transfer is set:
        If ledger is zero, it will be automatically be set to the pending transfer’s ledger.
        If ledger is nonzero, it must match the ledger value on the pending transfer’s debit_account_id and credit_account_id.
    When flags.post_pending_transfer and flags.void_pending_transfer are not set:
        ledger must not be zero.
        ledger must match the ledger value on the accounts referenced in debit_account_id and credit_account_id.

code

This is a user-defined enum denoting the reason for (or category of) the transfer.

Constraints:

    Type is 16-bit unsigned integer (2 bytes)
    When flags.post_pending_transfer or flags.void_pending_transfer is set:
        If code is zero, it will be automatically be set to the pending transfer’s code.
        If code is nonzero, it must match the pending transfer’s code.
    When flags.post_pending_transfer and flags.void_pending_transfer are not set, code must not be zero.

flags

This specifies (optional) transfer behavior.

Constraints:

    Type is 16-bit unsigned integer (2 bytes)
    Some flags are mutually exclusive; see flags_are_mutually_exclusive.

flags.linked

This flag links the result of this transfer to the outcome of the next transfer in the request such that they will either succeed or fail together.

The last transfer in a chain of linked transfers does not have this flag set.

You can read more about linked events.
Examples

    Currency Exchange

flags.pending

Mark the transfer as a pending transfer.
flags.post_pending_transfer

Mark the transfer as a post-pending transfer.
flags.void_pending_transfer

Mark the transfer as a void-pending transfer.
flags.balancing_debit

Transfer at most amount — automatically transferring less than amount as necessary such that debit_account.debits_pending + debit_account.debits_posted ≤ debit_account.credits_posted.

The amount of the recorded transfer is set to the actual amount that was transferred, which is less than or equal to the amount that was passed to create_transfers.

Retrying a balancing transfer will return exists_with_different_amount only when the maximum amount passed to create_transfers is insufficient to fulfill the amount that was actually transferred. Otherwise it may return exists even if the retry amount differs from the original value.

flags.balancing_debit is exclusive with the flags.post_pending_transfer/flags.void_pending_transfer flags because posting or voiding a pending transfer will never exceed/overflow either account’s limits.

flags.balancing_debit is compatible with (and orthogonal to) flags.balancing_credit.
Client release < 0.16.0

Examples

    Close Account

flags.balancing_credit

Transfer at most amount — automatically transferring less than amount as necessary such that credit_account.credits_pending + credit_account.credits_posted ≤ credit_account.debits_posted.

The amount of the recorded transfer is set to the actual amount that was transferred, which is less than or equal to the amount that was passed to create_transfers.

Retrying a balancing transfer will return exists_with_different_amount only when the maximum amount passed to create_transfers is insufficient to fulfill the amount that was actually transferred. Otherwise it may return exists even if the retry amount differs from the original value.

flags.balancing_credit is exclusive with the flags.post_pending_transfer/flags.void_pending_transfer flags because posting or voiding a pending transfer will never exceed/overflow either account’s limits.

flags.balancing_credit is compatible with (and orthogonal to) flags.balancing_debit.
Client release < 0.16.0

Examples

    Close Account

flags.closing_debit

When set, it will cause the Account.flags.closed flag of the debit account to be set if the transfer succeeds.

This flag requires a two-phase transfer, so the flag flags.pending must also be set. This ensures that closing transfers are reversible by voiding the pending transfer, and requires that the reversal operation references the corresponding closing transfer, guarding against unexpected interleaving of close/unclose operations.
flags.closing_credit

When set, it will cause the Account.flags.closed flag of the credit account to be set if the transfer succeeds.

This flag requires a two-phase transfer, so the flag flags.pending must also be set. This ensures that closing transfers are reversible by voiding the pending transfer, and requires that the reversal operation references the corresponding closing transfer, guarding against unexpected interleaving of close/unclose operations.
flags.imported

When set, allows importing historical Transfers with their original timestamp.

TigerBeetle will not use the cluster clock to assign the timestamp, allowing the user to define it, expressing when the transfer was effectively created by an external event.

To maintain system invariants regarding auditability and traceability, some constraints are necessary:

    It is not allowed to mix events with the imported flag set and not set in the same batch. The application must submit batches of imported events separately.

    User-defined timestamps must be unique and expressed as nanoseconds since the UNIX epoch. No two objects can have the same timestamp, even different objects like an Account and a Transfer cannot share the same timestamp.

    User-defined timestamps must be a past date, never ahead of the cluster clock at the time the request arrives.

    Timestamps must be strictly increasing.

    Even user-defined timestamps that are required to be past dates need to be at least one nanosecond ahead of the timestamp of the last transfer committed by the cluster.

    Since the timestamp cannot regress, importing past events can be naturally restrictive without coordination, as the last timestamp can be updated using the cluster clock during regular cluster activity. Instead, it’s recommended to import events only on a fresh cluster or during a scheduled maintenance window.

    It’s recommended to submit the entire batch as a linked chain, ensuring that if any transfer fails, none of them are committed, preserving the last timestamp unchanged. This approach gives the application a chance to correct failed imported transfers, re-submitting the batch again with the same user-defined timestamps.

    Imported transfers cannot have a timeout.

    It’s possible to import pending transfers with a user-defined timestamp, but since it’s not driven by the cluster clock, it cannot define a timeout for automatic expiration. In those cases, the two-phase post or rollback must be done manually.

timestamp

This is the time the transfer was created, as nanoseconds since UNIX epoch. You can read more about Time in TigerBeetle.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)

    Must be 0 when the Transfer is created with flags.imported not set

    It is set by TigerBeetle to the moment the transfer arrives at the cluster.

    Must be greater than 0 and less than 2^63 when the Transfer is created with flags.imported set

Internals

If you’re curious and want to learn more, you can find the source code for this struct in src/tigerbeetle.zig. Search for const Transfer = extern struct {.

You can find the source code for creating a transfer in src/state_machine.zig. Search for fn create_transfer(.
Edit this page
AccountBalance

An AccountBalance is a record storing the Account’s balance at a given point in time.

Only Accounts with the flag history set retain historical balances.
Fields
timestamp

This is the time the account balance was updated, as nanoseconds since UNIX epoch.

The timestamp refers to the same Transfer.timestamp which changed the Account.

The amounts refer to the account balance recorded after the transfer execution.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)

debits_pending

Amount of pending debits.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)

debits_posted

Amount of posted debits.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)

credits_pending

Amount of pending credits.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)

credits_posted

Amount of posted credits.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)

reserved

This space may be used for additional data in the future.

Constraints:

    Type is 56 bytes
    Must be zero

Edit this page
AccountFilter

An AccountFilter is a record containing the filter parameters for querying the account transfers and the account historical balances.
Fields
account_id

The unique identifier of the account for which the results will be retrieved.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)
    Must not be zero or 2^128 - 1

user_data_128

Filter the results by the field Transfer.user_data_128. Optional; set to zero to disable the filter.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)

user_data_64

Filter the results by the field Transfer.user_data_64. Optional; set to zero to disable the filter.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)

user_data_32

Filter the results by the field Transfer.user_data_32. Optional; set to zero to disable the filter.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)

code

Filter the results by the Transfer.code. Optional; set to zero to disable the filter.

Constraints:

    Type is 16-bit unsigned integer (2 bytes)

reserved

This space may be used for additional data in the future.

Constraints:

    Type is 58 bytes
    Must be zero

timestamp_min

The minimum Transfer.timestamp from which results will be returned, inclusive range. Optional; set to zero to disable the lower-bound filter.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)
    Must be less than 2^63.

timestamp_max

The maximum Transfer.timestamp from which results will be returned, inclusive range. Optional; set to zero to disable the upper-bound filter.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)
    Must be less than 2^63.

limit

The maximum number of results that can be returned by this query.

Limited by the maximum message size.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)
    Must not be zero

flags

A bitfield that specifies querying behavior.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)

flags.debits

Whether or not to include results where the field debit_account_id matches the parameter account_id.
flags.credits

Whether or not to include results where the field credit_account_id matches the parameter account_id.
flags.reversed

Whether the results are sorted by timestamp in chronological or reverse-chronological order. If the flag is not set, the event that happened first (has the smallest timestamp) will come first. If the flag is set, the event that happened last (has the largest timestamp) will come first.
Edit this page
QueryFilter

A QueryFilter is a record containing the filter parameters for querying accounts and querying transfers.
Fields
user_data_128

Filter the results by the field Account.user_data_128 or Transfer.user_data_128. Optional; set to zero to disable the filter.

Constraints:

    Type is 128-bit unsigned integer (16 bytes)

user_data_64

Filter the results by the field Account.user_data_64 or Transfer.user_data_64. Optional; set to zero to disable the filter.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)

user_data_32

Filter the results by the field Account.user_data_32 or Transfer.user_data_32. Optional; set to zero to disable the filter.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)

ledger

Filter the results by the field Account.ledger or Transfer.ledger. Optional; set to zero to disable the filter.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)

code

Filter the results by the field Account.code or Transfer.code. Optional; set to zero to disable the filter.

Constraints:

    Type is 16-bit unsigned integer (2 bytes)

reserved

This space may be used for additional data in the future.

Constraints:

    Type is 6 bytes
    Must be zero

timestamp_min

The minimum Account.timestamp or Transfer.timestamp from which results will be returned, inclusive range. Optional; set to zero to disable the lower-bound filter.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)
    Must not be 2^64 - 1

timestamp_max

The maximum Account.timestamp or Transfer.timestamp from which results will be returned, inclusive range. Optional; set to zero to disable the upper-bound filter.

Constraints:

    Type is 64-bit unsigned integer (8 bytes)
    Must not be 2^64 - 1

limit

The maximum number of results that can be returned by this query.

Limited by the maximum message size.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)
    Must not be zero

flags

A bitfield that specifies querying behavior.

Constraints:

    Type is 32-bit unsigned integer (4 bytes)

flags.reversed

Whether the results are sorted by timestamp in chronological or reverse-chronological order. If the flag is not set, the event that happened first (has the smallest timestamp) will come first. If the flag is set, the event that happened last (has the largest timestamp) will come first.
Edit this page
Requests

TigerBeetle supports the following request types:

    create_accounts: create Accounts
    create_transfers: create Transfers
    lookup_accounts: fetch Accounts by id
    lookup_transfers: fetch Transfers by id
    get_account_transfers: fetch Transfers by debit_account_id or credit_account_id
    get_account_balances: fetch the historical account balance by the Account’s id.
    query_accounts: query Accounts
    query_transfers: query Transfers

More request types, including more powerful queries, are coming soon!
Edit this page
create_accounts

Create one or more Accounts.
Event

The account to create. See Account for constraints.
Result

Results are listed in this section in order of descending precedence — that is, if more than one error is applicable to the account being created, only the result listed first is returned.
ok

The account was successfully created; it did not previously exist.

Note that ok is generated by the client implementation; the network protocol does not include a result when the account was successfully created.
linked_event_failed

The account was not created. One or more of the accounts in the linked chain is invalid, so the whole chain failed.
linked_event_chain_open

The account was not created. The Account.flags.linked flag was set on the last event in the batch, which is not legal. (flags.linked indicates that the chain continues to the next operation).
imported_event_expected

The account was not created. The Account.flags.imported was set on the first account of the batch, but not all accounts in the batch. Batches cannot mix imported accounts with non-imported accounts.
imported_event_not_expected

The account was not created. The Account.flags.imported was expected to not be set, as it’s not allowed to mix accounts with different imported flag in the same batch. The first account determines the entire operation.
timestamp_must_be_zero

This result only applies when Account.flags.imported is not set.

The account was not created. The Account.timestamp is nonzero, but must be zero. The cluster is responsible for setting this field.

The Account.timestamp can only be assigned when creating accounts with Account.flags.imported set.
imported_event_timestamp_out_of_range

This result only applies when Account.flags.imported is set.

The account was not created. The Account.timestamp is out of range, but must be a user-defined timestamp greater than 0 and less than 2^63.
imported_event_timestamp_must_not_advance

This result only applies when Account.flags.imported is set.

The account was not created. The user-defined Account.timestamp is greater than the current cluster time, but it must be a past timestamp.
reserved_field

The account was not created. Account.reserved is nonzero, but must be zero.
reserved_flag

The account was not created. Account.flags.reserved is nonzero, but must be zero.
id_must_not_be_zero

The account was not created. Account.id is zero, which is a reserved value.
id_must_not_be_int_max

The account was not created. Account.id is 2^128 - 1, which is a reserved value.
exists_with_different_flags

An account with the same id already exists, but with different flags.
exists_with_different_user_data_128

An account with the same id already exists, but with different user_data_128.
exists_with_different_user_data_64

An account with the same id already exists, but with different user_data_64.
exists_with_different_user_data_32

An account with the same id already exists, but with different user_data_32.
exists_with_different_ledger

An account with the same id already exists, but with different ledger.
exists_with_different_code

An account with the same id already exists, but with different code.
exists

An account with the same id already exists.

With the possible exception of the following fields, the existing account is identical to the account in the request:

    timestamp
    debits_pending
    debits_posted
    credits_pending
    credits_posted

To correctly recover from application crashes, many applications should handle exists exactly as ok.
flags_are_mutually_exclusive

The account was not created. An account cannot be created with the specified combination of Account.flags.

The following flags are mutually exclusive:

    Account.flags.debits_must_not_exceed_credits
    Account.flags.credits_must_not_exceed_debits

debits_pending_must_be_zero

The account was not created. Account.debits_pending is nonzero, but must be zero.

An account’s debits and credits are only modified by transfers.
debits_posted_must_be_zero

The account was not created. Account.debits_posted is nonzero, but must be zero.

An account’s debits and credits are only modified by transfers.
credits_pending_must_be_zero

The account was not created. Account.credits_pending is nonzero, but must be zero.

An account’s debits and credits are only modified by transfers.
credits_posted_must_be_zero

The account was not created. Account.credits_posted is nonzero, but must be zero.

An account’s debits and credits are only modified by transfers.
ledger_must_not_be_zero

The account was not created. Account.ledger is zero, but must be nonzero.
code_must_not_be_zero

The account was not created. Account.code is zero, but must be nonzero.
imported_event_timestamp_must_not_regress

This result only applies when Account.flags.imported is set.

The account was not created. The user-defined Account.timestamp regressed, but it must be greater than the last timestamp assigned to any Account in the cluster and cannot be equal to the timestamp of any existing Transfer.
Client libraries

For language-specific docs see:

    .NET library
    Java library
    Go library
    Node.js library
    Python library

Internals

If you’re curious and want to learn more, you can find the source code for creating an account in src/state_machine.zig. Search for fn create_account( and fn execute(.
Edit this page
create_transfers

Create one or more Transfers. A successfully created transfer will modify the amount fields of its debit and credit accounts.
Event

The transfer to create. See Transfer for constraints.
Result

Results are listed in this section in order of descending precedence — that is, if more than one error is applicable to the transfer being created, only the result listed first is returned.
ok

The transfer was successfully created; did not previously exist.

Note that ok is generated by the client implementation; the network protocol does not include a result when the transfer was successfully created.
linked_event_failed

The transfer was not created. One or more of the other transfers in the linked chain is invalid, so the whole chain failed.
linked_event_chain_open

The transfer was not created. The Transfer.flags.linked flag was set on the last event in the batch, which is not legal. (flags.linked indicates that the chain continues to the next operation).
imported_event_expected

The transfer was not created. The Transfer.flags.imported was set on the first transfer of the batch, but not all transfers in the batch. Batches cannot mix imported transfers with non-imported transfers.
imported_event_not_expected

The transfer was not created. The Transfer.flags.imported was expected to not be set, as it’s not allowed to mix transfers with different imported flag in the same batch. The first transfer determines the entire operation.
timestamp_must_be_zero

This result only applies when Account.flags.imported is not set.

The transfer was not created. The Transfer.timestamp is nonzero, but must be zero. The cluster is responsible for setting this field.

The Transfer.timestamp can only be assigned when creating transfers with Transfer.flags.imported set.
imported_event_timestamp_out_of_range

This result only applies when Transfer.flags.imported is set.

The transfer was not created. The Transfer.timestamp is out of range, but must be a user-defined timestamp greater than 0 and less than 2^63.
imported_event_timestamp_must_not_advance

This result only applies when Transfer.flags.imported is set.

The transfer was not created. The user-defined Transfer.timestamp is greater than the current cluster time, but it must be a past timestamp.
reserved_flag

The transfer was not created. Transfer.flags.reserved is nonzero, but must be zero.
id_must_not_be_zero

The transfer was not created. Transfer.id is zero, which is a reserved value.
id_must_not_be_int_max

The transfer was not created. Transfer.id is 2^128 - 1, which is a reserved value.
exists_with_different_flags

A transfer with the same id already exists, but with different flags.
exists_with_different_pending_id

A transfer with the same id already exists, but with a different pending_id.
exists_with_different_timeout

A transfer with the same id already exists, but with a different timeout.
exists_with_different_debit_account_id

A transfer with the same id already exists, but with a different debit_account_id.
exists_with_different_credit_account_id

A transfer with the same id already exists, but with a different credit_account_id.
exists_with_different_amount

A transfer with the same id already exists, but with a different amount.

If the transfer has flags.balancing_debit or flags.balancing_credit set, then the actual amount transferred exceeds this failed transfer’s amount.
exists_with_different_user_data_128

A transfer with the same id already exists, but with a different user_data_128.
exists_with_different_user_data_64

A transfer with the same id already exists, but with a different user_data_64.
exists_with_different_user_data_32

A transfer with the same id already exists, but with a different user_data_32.
exists_with_different_ledger

A transfer with the same id already exists, but with a different ledger.
exists_with_different_code

A transfer with the same id already exists, but with a different code.
exists

A transfer with the same id already exists.

If the transfer has flags.balancing_debit or flags.balancing_credit set, then the existing transfer may have a different amount, limited to the maximum amount of the transfer in the request.

If the transfer has flags.post_pending_transfer set, then the existing transfer may have a different amount:

    If the original posted amount was less than the pending amount, then the transfer amount must be equal to the posted amount.
    Otherwise, the transfer amount must be greater than or equal to the pending amount.

Client release < 0.16.0

Otherwise, with the possible exception of the timestamp field, the existing transfer is identical to the transfer in the request.

To correctly recover from application crashes, many applications should handle exists exactly as ok.
id_already_failed

The transfer was not created. A previous transfer with the same id failed due to one of the following transient errors:

    debit_account_not_found
    credit_account_not_found
    pending_transfer_not_found
    exceeds_credits
    exceeds_debits
    debit_account_already_closed
    credit_account_already_closed

Transient errors depend on the database state at a given point in time, and each attempt is uniquely associated with the corresponding Transfer.id. This behavior guarantees that retrying a transfer will not produce a different outcome (either success or failure).

Without this mechanism, a transfer that previously failed could succeed if retried when the underlying state changes (e.g., the target account has sufficient credits).

Note: The application should retry an event only if it was unable to acknowledge the last response (e.g., due to an application restart) or because it is correcting a previously rejected malformed request (e.g., due to an application bug). If the application intends to submit the transfer again even after a transient error, it must generate a new idempotency id.
Client release < 0.16.4

flags_are_mutually_exclusive

The transfer was not created. A transfer cannot be created with the specified combination of Transfer.flags.

Flag compatibility (✓ = compatible, ✗ = mutually exclusive):

    flags.pending
        ✗ flags.post_pending_transfer
        ✗ flags.void_pending_transfer
        ✓ flags.balancing_debit
        ✓ flags.balancing_credit
        ✓ flags.closing_debit
        ✓ flags.closing_credit
        ✓ flags.imported
    flags.post_pending_transfer
        ✗ flags.pending
        ✗ flags.void_pending_transfer
        ✗ flags.balancing_debit
        ✗ flags.balancing_credit
        ✗ flags.closing_debit
        ✗ flags.closing_credit
        ✓ flags.imported
    flags.void_pending_transfer
        ✗ flags.pending
        ✗ flags.post_pending_transfer
        ✗ flags.balancing_debit
        ✗ flags.balancing_credit
        ✗ flags.closing_debit
        ✗ flags.closing_credit
        ✓ flags.imported
    flags.balancing_debit
        ✓ flags.pending
        ✗ flags.void_pending_transfer
        ✗ flags.post_pending_transfer
        ✓ flags.balancing_credit
        ✓ flags.closing_debit
        ✓ flags.closing_credit
        ✓ flags.imported
    flags.balancing_credit
        ✓ flags.pending
        ✗ flags.void_pending_transfer
        ✗ flags.post_pending_transfer
        ✓ flags.balancing_debit
        ✓ flags.closing_debit
        ✓ flags.closing_credit
        ✓ flags.imported
    flags.closing_debit
        ✓ flags.pending
        ✗ flags.post_pending_transfer
        ✗ flags.void_pending_transfer
        ✓ flags.balancing_debit
        ✓ flags.balancing_credit
        ✓ flags.closing_credit
        ✓ flags.imported
    flags.closing_credit
        ✓ flags.pending
        ✗ flags.post_pending_transfer
        ✗ flags.void_pending_transfer
        ✓ flags.balancing_debit
        ✓ flags.balancing_credit
        ✓ flags.closing_debit
        ✓ flags.imported
    flags.imported
        ✓ flags.pending
        ✓ flags.post_pending_transfer
        ✓ flags.void_pending_transfer
        ✓ flags.balancing_debit
        ✓ flags.balancing_credit
        ✓ flags.closing_debit
        ✓ flags.closing_credit

debit_account_id_must_not_be_zero

The transfer was not created. Transfer.debit_account_id is zero, but must be a valid account id.
debit_account_id_must_not_be_int_max

The transfer was not created. Transfer.debit_account_id is 2^128 - 1, but must be a valid account id.
credit_account_id_must_not_be_zero

The transfer was not created. Transfer.credit_account_id is zero, but must be a valid account id.
credit_account_id_must_not_be_int_max

The transfer was not created. Transfer.credit_account_id is 2^128 - 1, but must be a valid account id.
accounts_must_be_different

The transfer was not created. Transfer.debit_account_id and Transfer.credit_account_id must not be equal.

That is, an account cannot transfer money to itself.
pending_id_must_be_zero

The transfer was not created. Only post/void transfers can reference a pending transfer.

Either:

    Transfer.flags.post_pending_transfer must be set, or
    Transfer.flags.void_pending_transfer must be set, or
    Transfer.pending_id must be zero.

pending_id_must_not_be_zero

The transfer was not created. Transfer.flags.post_pending_transfer or Transfer.flags.void_pending_transfer is set, but Transfer.pending_id is zero. A posting or voiding transfer must reference a pending transfer.
pending_id_must_not_be_int_max

The transfer was not created. Transfer.pending_id is 2^128 - 1, which is a reserved value.
pending_id_must_be_different

The transfer was not created. Transfer.pending_id is set to the same id as Transfer.id. Instead it should refer to a different (existing) transfer.
timeout_reserved_for_pending_transfer

The transfer was not created. Transfer.timeout is nonzero, but only pending transfers have nonzero timeouts.
closing_transfer_must_be_pending

The transfer was not created. Transfer.flags.pending is not set, but closing transfers must be two-phase pending transfers.

If either Transfer.flags.closing_debit or Transfer.flags.closing_credit is set, Transfer.flags.pending must also be set.

This ensures that closing transfers are reversible by voiding the pending transfer, and requires that the reversal operation references the corresponding closing transfer, guarding against unexpected interleaving of close/unclose operations.
amount_must_not_be_zero

Deprecated: This error code is only returned to clients prior to release 0.16.0. Since 0.16.0, zero-amount transfers are permitted.
Client release < 0.16.0

ledger_must_not_be_zero

The transfer was not created. Transfer.ledger is zero, but must be nonzero.
code_must_not_be_zero

The transfer was not created. Transfer.code is zero, but must be nonzero.
debit_account_not_found

The transfer was not created. Transfer.debit_account_id must refer to an existing Account.

This is a transient error. The Transfer.id associated with this particular attempt will always fail upon retry, even if the underlying issue is resolved. To succeed, a new idempotency id must be submitted.
credit_account_not_found

The transfer was not created. Transfer.credit_account_id must refer to an existing Account.

This is a transient error. The Transfer.id associated with this particular attempt will always fail upon retry, even if the underlying issue is resolved. To succeed, a new idempotency id must be submitted.
accounts_must_have_the_same_ledger

The transfer was not created. The accounts referred to by Transfer.debit_account_id and Transfer.credit_account_id must have an identical ledger.

Currency exchange is implemented with multiple transfers.
transfer_must_have_the_same_ledger_as_accounts

The transfer was not created. The accounts referred to by Transfer.debit_account_id and Transfer.credit_account_id are equivalent, but differ from the Transfer.ledger.
pending_transfer_not_found

The transfer was not created. The transfer referenced by Transfer.pending_id does not exist.

This is a transient error. The Transfer.id associated with this particular attempt will always fail upon retry, even if the underlying issue is resolved. To succeed, a new idempotency id must be submitted.
pending_transfer_not_pending

The transfer was not created. The transfer referenced by Transfer.pending_id exists, but does not have flags.pending set.
pending_transfer_has_different_debit_account_id

The transfer was not created. The transfer referenced by Transfer.pending_id exists, but with a different debit_account_id.

The post/void transfer’s debit_account_id must either be 0 or identical to the pending transfer’s debit_account_id.
pending_transfer_has_different_credit_account_id

The transfer was not created. The transfer referenced by Transfer.pending_id exists, but with a different credit_account_id.

The post/void transfer’s credit_account_id must either be 0 or identical to the pending transfer’s credit_account_id.
pending_transfer_has_different_ledger

The transfer was not created. The transfer referenced by Transfer.pending_id exists, but with a different ledger.

The post/void transfer’s ledger must either be 0 or identical to the pending transfer’s ledger.
pending_transfer_has_different_code

The transfer was not created. The transfer referenced by Transfer.pending_id exists, but with a different code.

The post/void transfer’s code must either be 0 or identical to the pending transfer’s code.
exceeds_pending_transfer_amount

The transfer was not created. The transfer’s amount exceeds the amount of its pending transfer.
pending_transfer_has_different_amount

The transfer was not created. The transfer is attempting to void a pending transfer. The voiding transfer’s amount must be either 0 or exactly the amount of the pending transfer.

To partially void a transfer, create a posting transfer with an amount less than the pending transfer’s amount.
Client release < 0.16.0

pending_transfer_already_posted

The transfer was not created. The referenced pending transfer was already posted by a post_pending_transfer.
pending_transfer_already_voided

The transfer was not created. The referenced pending transfer was already voided by a void_pending_transfer.
pending_transfer_expired

The transfer was not created. The referenced pending transfer was already voided because its timeout has passed.
imported_event_timestamp_must_not_regress

This result only applies when Transfer.flags.imported is set.

The transfer was not created. The user-defined Transfer.timestamp regressed, but it must be greater than the last timestamp assigned to any Transfer in the cluster and cannot be equal to the timestamp of any existing Account.
imported_event_timestamp_must_postdate_debit_account

This result only applies when Transfer.flags.imported is set.

The transfer was not created. Transfer.debit_account_id must refer to an Account whose timestamp is less than the Transfer.timestamp.
imported_event_timestamp_must_postdate_credit_account

This result only applies when Transfer.flags.imported is set.

The transfer was not created. Transfer.credit_account_id must refer to an Account whose timestamp is less than the Transfer.timestamp.
imported_event_timeout_must_be_zero

This result only applies when Transfer.flags.imported is set.

The transfer was not created. The Transfer.timeout is nonzero, but must be zero.

It’s possible to import pending transfers with a user-defined timestamp, but since it’s not driven by the cluster clock, it cannot define a timeout for automatic expiration. In those cases, the two-phase post or rollback must be done manually.
debit_account_already_closed

The transfer was not created. Transfer.debit_account_id must refer to an Account whose Account.flags.closed is not already set.

This is a transient error. The Transfer.id associated with this particular attempt will always fail upon retry, even if the underlying issue is resolved. To succeed, a new idempotency id must be submitted.
credit_account_already_closed

The transfer was not created. Transfer.credit_account_id must refer to an Account whose Account.flags.closed is not already set.

This is a transient error. The Transfer.id associated with this particular attempt will always fail upon retry, even if the underlying issue is resolved. To succeed, a new idempotency id must be submitted.
overflows_debits_pending

The transfer was not created. debit_account.debits_pending + transfer.amount would overflow a 128-bit unsigned integer.
overflows_credits_pending

The transfer was not created. credit_account.credits_pending + transfer.amount would overflow a 128-bit unsigned integer.
overflows_debits_posted

The transfer was not created. debit_account.debits_posted + transfer.amount would overflow a 128-bit unsigned integer.
overflows_credits_posted

The transfer was not created. debit_account.credits_posted + transfer.amount would overflow a 128-bit unsigned integer.
overflows_debits

The transfer was not created. debit_account.debits_pending + debit_account.debits_posted + transfer.amount would overflow a 128-bit unsigned integer.
overflows_credits

The transfer was not created. credit_account.credits_pending + credit_account.credits_posted + transfer.amount would overflow a 128-bit unsigned integer.
overflows_timeout

The transfer was not created. transfer.timestamp + (transfer.timeout * 1_000_000_000) would exceed 2^63.

Transfer.timeout is converted to nanoseconds.

This computation uses the Transfer.timestamp value assigned by the replica, not the 0 value sent by the client.
exceeds_credits

The transfer was not created.

The debit account has flags.debits_must_not_exceed_credits set, but debit_account.debits_pending + debit_account.debits_posted + transfer.amount would exceed debit_account.credits_posted.

This is a transient error. The Transfer.id associated with this particular attempt will always fail upon retry, even if the underlying issue is resolved. To succeed, a new idempotency id must be submitted.
Client release < 0.16.0

exceeds_debits

The transfer was not created.

The credit account has flags.credits_must_not_exceed_debits set, but credit_account.credits_pending + credit_account.credits_posted + transfer.amount would exceed credit_account.debits_posted.

This is a transient error. The Transfer.id associated with this particular attempt will always fail upon retry, even if the underlying issue is resolved. To succeed, a new idempotency id must be submitted.
Client release < 0.16.0

Client libraries

For language-specific docs see:

    .NET library
    Java library
    Go library
    Node.js library
    Python library

Internals

If you’re curious and want to learn more, you can find the source code for creating a transfer in src/state_machine.zig. Search for fn create_transfer( and fn execute(.
Edit this page
lookup_accounts

Fetch one or more accounts by their ids.

⚠️ Note that you should not check an account’s balance using this request before creating a transfer. That would not be atomic and the balance could change in between the check and the transfer. Instead, set the debits_must_not_exceed_credits or credits_must_not_exceed_debits flag on the accounts to limit their account balances. More complex conditional transfers can be expressed using balance-conditional transfers.

⚠️ It is not possible currently to look up more than a full batch (8189) of accounts atomically. When issuing multiple lookup_accounts calls, it can happen that other operations will interleave between the calls leading to read skew. Consider using the history flag to enable atomic lookups.
Event

An id belonging to a Account.
Result

    If the account exists, return the Account.
    If the account does not exist, return nothing.

Client libraries

For language-specific docs see:

    .NET library
    Java library
    Go library
    Node.js library

Internals

If you’re curious and want to learn more, you can find the source code for looking up an account in src/state_machine.zig. Search for fn execute_lookup_accounts(.
Edit this page
lookup_transfers

Fetch one or more transfers by their ids.
Event

An id belonging to a Transfer.
Result

    If the transfer exists, return the Transfer.
    If the transfer does not exist, return nothing.

Client libraries

For language-specific docs see:

    .NET library
    Java library
    Go library
    Node.js library

Internals

If you’re curious and want to learn more, you can find the source code for looking up a transfer in src/state_machine.zig. Search for fn execute_lookup_transfers(.
Edit this page
get_account_transfers

Fetch Transfers involving a given Account.
Event

The account filter. See AccountFilter for constraints.
Result

    Return a (possibly empty) array of Transfers that match the filter.
    If any constraint is violated, return nothing.
    By default, Transfers are sorted chronologically by timestamp. You can use the reversed to change this.
    The result is always limited in size. If there are more results, you need to page through them using the AccountFilter’s timestamp_min and/or timestamp_max.

Client libraries

For language-specific docs see:

    .NET library
    Java library
    Go library
    Node.js library
    Python library

Edit this page
get_account_balances

Fetch the historical AccountBalances of a given Account.

Only accounts created with the history flag set retain historical balances. This is off by default.

    Each balance returned has a corresponding transfer with the same timestamp. See the get_account_transfers operation for more details.

    The amounts refer to the account balance recorded after the transfer execution.

    Pending balances automatically removed due to timeout expiration don’t change historical balances.

Event

The account filter. See AccountFilter for constraints.
Result

    If the account has the flag history set and any matching balances exist, return an array of AccountBalances.
    If the account does not have the flag history set, return nothing.
    If no matching balances exist, return nothing.
    If any constraint is violated, return nothing.

Client libraries

For language-specific docs see:

    .NET library
    Java library
    Go library
    Node.js library
    Python library

Edit this page
query_accounts

Query Accounts by the intersection of some fields and by timestamp range.

⚠️ It is not possible currently to query more than a full batch (8189) of accounts atomically. When issuing multiple query_accounts calls, it can happen that other operations will interleave between the calls leading to read skew. Consider using the history flag to enable atomic lookups.
Event

The query filter. See QueryFilter for constraints.
Result

    Return a (possibly empty) array of Accounts that match the filter.
    If any constraint is violated, return nothing.
    By default, Accounts are sorted chronologically by timestamp. You can use the reversed to change this.
    The result is always limited in size. If there are more results, you need to page through them using the QueryFilter’s timestamp_min and/or timestamp_max.

Client libraries

For language-specific docs see:

    .NET library
    Java library
    Go library
    Node.js library
    Python library

Edit this page
query_transfers

Query Transfers by the intersection of some fields and by timestamp range.
Event

The query filter. See QueryFilter for constraints.
Result

    Return a (possibly empty) array of Transfers that match the filter.
    If any constraint is violated, return nothing.
    By default, Transfers are sorted chronologically by timestamp. You can use the reversed to change this.
    The result is always limited in size. If there are more results, you need to page through them using the QueryFilter’s timestamp_min and/or timestamp_max.
