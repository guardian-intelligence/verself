OBuilder (OCaml CI infrastructure) — Interesting Techniques                 
                                                                              
  The most surprising: seccomp to fake fsync.                                 
  OBuilder intercepts all sync syscalls (fsync, fdatasync, msync, sync,       
  syncfs, sync_file_range) via seccomp and returns success without actually   
  syncing. The rationale: if the machine crashes mid-build, the result is   
  thrown away anyway (no @snap tag exists yet), so writing to disk is pure    
  waste. This is a massive speedup, especially for workloads that call fsync
  heavily (like npm install). Directly applicable to forge-metal's sandbox.

  Content-addressable build steps via hash chains. Each build step is         
  identified by SHA256(parent_id + command + env + user + workdir). Since the
  parent ID is itself a hash, changing any earlier step cascades through all  
  subsequent ones — structurally identical to how Git commits work. Two     
  developers running the same build share cached snapshots automatically.

  @snap tag as a crash recovery marker. Build results are written directly to 
  their final ZFS dataset (not a temp location, because ZFS can't rename while
   files are open). The @snap snapshot is only created after the build        
  succeeds. On startup, any dataset without @snap is known to be a crashed  
  partial build and gets cleaned up. Simple, no external state needed.

  SQLite LRU with reference-counted GC. Each cached build tracks a rc         
  (reference count) of how many child builds depend on it. Eviction query:
  WHERE rc = 0 AND used < threshold ORDER BY used ASC. You can only evict     
  leaves of the dependency tree — parents are protected until all children are
   gone.

  The ZFS "promotion dance" for cache updates. ZFS can't delete a dataset that
   has clones. So when updating a shared cache (like an opam package cache),
  OBuilder: (1) renames the old dataset to a temp name, (2) zfs promote the   
  new clone (transfers all snapshot relationships), (3) destroys the old    
  dataset, (4) marks old snapshots for deferred deletion with zfs destroy -d.
  They documented this in a large comment because it's the hardest part of
  their ZFS integration.

  macOS builds create actual OS users. Since macOS has no containers, OBuilder
   creates real macOS users via dscl and runs builds as those users with sudo 
  su -l. ZFS mountpoint manipulation (zfs set mountpoint=) provides filesystem
   isolation. Builds are serialized on macOS (global mutex) because of this.

  A mysterious workaround they don't fully understand. When destroying a      
  temporary cache dataset fails, they run fuser -mv to debug, sleep 10
  seconds, and retry. The comment says "Don't know what's causing this." A    
  race condition with something holding a reference to the ZFS dataset.     

  ---
  Several of these are directly relevant to forge-metal's sandbox
  implementation — especially the seccomp fsync trick, the @snap crash        
  recovery marker, and the ZFS promotion dance for updating golden images
  while clones exist. I'll compile the other codebases as they come in.

  Velo (Git-like Postgres branching) — Interesting Techniques                 
                                                                              
  CHECKPOINT instead of pg_start_backup/pg_stop_backup. Before snapshotting,  
  they just run CHECKPOINT on the running Postgres (~100ms to flush dirty     
  buffers), then immediately zfs snapshot. This works because ZFS snapshots   
  are atomic — after CHECKPOINT, the on-disk state is consistent. Much simpler
   than the traditional backup API. Directly relevant if forge-metal ever
  snapshots a running ClickHouse.

  Clone-then-swap for branch reset. When resetting a branch:                  
  1. Clone parent's snapshot → temp dataset
  2. Mount temp to verify it works                                            
  3. Rename original → backup                                               
  4. Rename temp → original name
  5. Destroy backup                                                           
                   
  If cloning fails, the original is untouched. Only after success does the    
  swap happen. This is the safe pattern forge-metal should use for golden     
  image refresh — never destroy the old golden until the new one is verified.
                                                                              
  Atomic state persistence. All metadata lives in a single JSON file, but with
   surprising rigor: write to .tmp, fsync, rename (atomic on POSIX), then
  fsync the parent directory. Plus automatic backup before every save. This is
   the same pattern SQLite and etcd use. Overkill for a CLI tool — but it   
  means interrupted operations never corrupt state.

  Rollback-on-failure mini-transactions. Every multi-step operation uses a    
  Rollback class that collects cleanup functions in LIFO order. If the
  operation fails at step 3, steps 2 and 1 are unwound. On success,           
  rollback.clear() discards them. A pattern worth stealing for the Go sandbox
  lifecycle.

  No checkout, no merge. The biggest conceptual surprise. Every branch runs   
  its own Postgres container on its own port simultaneously. There's no
  "switching" — you just connect to a different port. This sidesteps the      
  hardest part of version control (merging) by simply not doing it. Branches
  are throwaway parallel universes.

  recordsize=8k to match Postgres page size. Hardcoded, no config. Postgres   
  reads/writes in 8KB pages. If ZFS uses a larger recordsize (default 128KB),
  a single-page write triggers a 128KB read-modify-write cycle. Matching      
  recordsize to page size eliminates this amplification. For forge-metal's  
  ClickHouse, the equivalent would be tuning recordsize to match ClickHouse's
  mark/granule size.

  Port allocation delegated entirely to Docker. Pass port: 0, Docker picks    
  one, read it back after startup. No port registry, no conflict management.
  Ports can change between restarts — state is updated after every start.     
                                                                            
  ---
  Still waiting on DBLab, go-zfs, impermanence, and Firecracker. DBLab should
  be the most interesting since it's the most mature tool in this space. 

  o-zfs is in. Key finding for forge-metal:                                  
                                                                              
  go-zfs (mistifyio) vs go-libzfs (bicomsystems)                              
                                                                              
  Use mistifyio/go-zfs v4. Don't touch go-libzfs.                             
                                                                              
  go-libzfs links against the C libzfs library via cgo. Sounds faster         
  (in-process calls vs subprocess per operation), but in practice:            
                                                                              
  - SIGSEGV panics from concurrent HTTP handlers freeing C memory out from  
  under Go pointers
  - Breaks on every ZFS version upgrade — issues track breakage for ZFS 0.6,
  0.7, 0.8, 2.0, 2.1, 2.2 separately                                          
  - Global mutex serializes all property reads, eliminating concurrency
  benefits                                                                    
  - A SendSize() function that redirects the process's actual fd 1 (stdout) —
  process-global, not thread-safe                                             
  - Requires libzfs-dev headers at compile time, killing cross-compilation  
                                                                              
  mistifyio/go-zfs just shells out to zfs/zpool via exec.Command. Subprocess  
  overhead is irrelevant for management-plane operations (you're              
  creating/destroying datasets, not doing thousands per second). It parses    
  ZFS's -H -p machine-readable output. Stable across ZFS versions because the
  CLI is a committed interface.

  One gap relevant to forge-metal: go-zfs doesn't expose zfs promote.         
  OBuilder's research showed that promote is critical for updating golden
  images while clones are active (the "promotion dance"). You'd need to add   
  that — but it's trivial, just one more exec.Command("zfs", "promote",     
  dataset) following their existing patterns.

  DBLab Engine (Postgres.AI) — Interesting Techniques

  Dual-pool rotation for live refresh. This is their biggest architectural
  insight. The problem: how do you update the golden snapshot while clones are
   serving traffic? Their answer: maintain multiple pools in an ordered linked
   list. When it's time to refresh:

  1. Find a pool with zero active clones (iterate from least-recently-active)
  2. Stream fresh data into that idle pool
  3. Take new snapshots
  4. Move that pool to the front of the list (make it active for new clones)

  Old clones on the old pool keep running until destroyed. New clones go to
  the fresh pool. No downtime, no promotion dance, no ZFS trickery.
  forge-metal's golden-refresh.yml does this at the fleet level (rolling 25%
  of workers), but DBLab solved it within a single host.

  Pre-snapshot + clone dance for consistent Postgres snapshots. They never
  snapshot a running database directly. Instead:
  1. zfs snapshot pool@pre (captures live state, may have dirty buffers)
  2. zfs clone pool@pre → clone_pre (new dataset from that snapshot)
  3. Start Postgres in a Docker container on the clone
  4. Promote from replica → primary
  5. CHECKPOINT (flush everything)
  6. Stop Postgres
  7. zfs snapshot clone_pre@clean (now the data is clean)

  This guarantees a crash-consistent snapshot without ever stopping the source
   database.

  Custom ZFS user properties as a metadata layer. ZFS doesn't expose
  clone→parent relationships easily. DBLab builds a virtual DAG using custom
  properties: dle:branch, dle:parent, dle:child, dle:root, dle:message. Commit
   messages are base64-encoded to avoid ZFS property value escaping issues.
  They reconstruct the full relationship graph on startup via
  VerifyBranchMetadata().

  Automatic password reset on clone. When cloning a production database, all
  Postgres user passwords are reset to random MD5 hashes. This prevents
  credential leakage from prod data into dev/test environments. Simple but
  important — nobody else does this.

  Idle clone detection from Postgres CSV logs. They parse Postgres's CSV log
  files to detect inactive clones, then fall back to checking
  pg_stat_activity. Clones idle beyond a threshold are automatically
  destroyed. This is how they manage space pressure — not quotas, but
  automatic reaping of abandoned clones.

  Async clone creation. CreateClone() returns immediately with StatusCreating,
   then provisions in a goroutine. The ZFS clone is O(1), but starting the
  Docker container + Postgres promotion takes seconds. The API returns
  instantly so callers don't block.

  Pre-snapshot size accounting workaround. The pre-snapshot clone that sits
  between the original and final snapshot accumulates size that ZFS doesn't
  attribute to the final snapshot. calculateEntrySize() traces the origin
  chain and sums sizes across the pre-clone lineage. Without this, the UI
  would underreport snapshot sizes.

  No ZFS library — 1300 lines of shell-out. Like OBuilder, like Velo, like
  go-zfs: everyone shells out. DBLab even has a TODO comment saying "go-libzfs
   might be useful" but never adopted it. The CLI is the stable interface.

  ---
  Three interesting patterns that converge across all codebases studied so
  far:

  1. Everyone shells out to zfs CLI. Nobody uses libzfs bindings in
  production. The CLI is the stable API.
  2. Everyone invents their own metadata layer on top of ZFS. OBuilder uses
  SQLite + @snap tags. DBLab uses custom ZFS properties. Velo uses a JSON
  file. ZFS itself has no built-in way to track relationships.
  3. The hardest problem is updating the golden image while clones exist.
  OBuilder does a promotion dance. DBLab does pool rotation. Velo does
  clone-then-swap. Three different solutions to the same fundamental
  constraint: you can't destroy a snapshot that has dependents.