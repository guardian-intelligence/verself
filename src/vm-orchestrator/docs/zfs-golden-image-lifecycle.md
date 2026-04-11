# ZFS Golden Image Lifecycle

1. Host clones ZFS base snapshot to a candidate repo-golden zvol.
2. Host boots a warm-golden Firecracker VM with that zvol as /dev/vda.
3. VM gets a narrow network profile: Forgejo and dependency mirrors only, preferably no general internet.
4. VM fetches from Forgejo, installs deps through Verdaccio or other mirrors, writes warm metadata like lockfile hash, and returns a typed supervisor
   manifest over vsock.
5. Host snapshots the same zvol as @ready after the guest exits cleanly and the manifest passes promotion gates. No “send it out” step is needed; the
   host already owns the zvol. The host must not mount or fsck the guest-mutated ext4 filesystem in the trusted host path.

    The key distinction: ZFS stays host-side at the block layer; filesystem parsing and untrusted repo execution stay guest-side.

Forgejo connectivity uses an explicit host-service plane rather than `route_localnet` or DNAT to `127.0.0.1`:

- Add a host-only service IP, e.g. 10.255.0.1/32 on a dummy interface like fm-host0.
- Put Caddy or a tiny internal reverse proxy on 10.255.0.1:18080.
- Pass the host-service origin to the VM as runtime config.
- Caddy reverse-proxies git.<domain> to Forgejo’s existing 127.0.0.1:3000.
- nftables allows Firecracker TAPs to 10.255.0.1:18080 and 10.255.0.1:4873, and drops other host-local/internal destinations.
- Warm/exec repo URLs must be HTTP(S); SSH-style clone URLs are rejected on this path.

That avoids the 127/8 routing footgun. Firecracker just attaches the VM to TAP devices; the host owns TAP routing and policy.

Define VM “pools” as profiles, not separate codebases:

- warm-golden: can reach Forgejo + package mirrors; can receive a short-lived repo-read credential; writes a candidate repo-golden zvol and supervisor
manifest.
- ci-exec: starts from repo-golden snapshot, fetches the target ref inside the VM, compares lockfile hash inside the VM, runs prepare only if needed, then
runs CI; no host filesystem parser.
- customer-sandbox: separate CIDR/profile, no operator Forgejo access by default, billing and policy attached from the start.

The exec path follows the same boundary: it starts from the repo-golden snapshot, fetches the requested ref inside the VM, compares lockfile hashes inside the
VM, and skips dependency install only after the guest supervisor returns that decision.

If private repo credentials matter, there are two sane options:

- Short-term: inject a short-lived, repo-scoped, read-only clone credential into the warm/exec VM, use it only for clone/fetch, don’t persist it in .git/
config, remove it before running repo scripts, and don’t allow general internet egress where it can be exfiltrated.
- Stronger later: a vsock fetch broker keeps credentials host-side and exposes only “give this VM this repo/ref pack/archive” semantics. More secure for
credentials, but more code and easier to accidentally reinvent a leaky HTTP proxy.

I would not use host-side clone/mount/fsck as the main path. It is operationally tempting, but it moves Git parsing, ext4 parsing, and sometimes workspace
mutation back into the privileged host boundary. The whole point of the Firecracker move is to make the malicious repo only affect a disposable VM and a
candidate zvol that gets promoted only after a clean guest outcome and typed supervisor manifest.
