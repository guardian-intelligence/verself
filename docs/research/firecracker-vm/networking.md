# Firecracker Networking for Concurrent CI VMs

> Patterns for managing networking across hundreds of ephemeral Firecracker VMs per hour.
> TAP devices, NAT at scale, CNI integration, rate limiting, and DNS.
>
> Repo: [firecracker-microvm/firecracker](https://github.com/firecracker-microvm/firecracker)
> Researched 2026-03-29.

## TAP device management at scale

Every Firecracker VM requires one TAP device per network interface. TAP creation is a
single `ioctl(TUNSETIFF)` call against `/dev/net/tun` with `IFF_TAP | IFF_NO_PI | IFF_VNET_HDR`
flags. No published isolated benchmarks exist, but the [firecracker-demo](https://github.com/firecracker-microvm/firecracker-demo)
creates 4,000 TAP devices (named `fc-{ID}-tap0`) and starts 4,000 microVMs in ~60 seconds.
Per-TAP overhead is sub-millisecond -- it is a metadata operation, not I/O.

Teardown: `ip link del <tapN>`. For namespace-based setups, deleting the namespace
destroys all devices within it automatically.

Source: [Cloudflare: Virtual networking 101 -- understanding TAP](https://blog.cloudflare.com/virtual-networking-101-understanding-tap/)

## Three networking patterns

### Pattern A: Individual TAP + per-VM NAT (simplest)

```bash
ip tuntap add tap0 mode tap
ip addr add 172.16.0.1/30 dev tap0
ip link set tap0 up
iptables -t nat -A POSTROUTING -o eth0 -s 172.16.0.2 -j MASQUERADE
iptables -A FORWARD -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT
iptables -A FORWARD -i tap0 -o eth0 -j ACCEPT
```

Guest boot args: `ip=172.16.0.2::172.16.0.1:255.255.255.252::eth0:off`

Each VM gets its own `/30` subnet and NAT rule. **Problem at scale:** iptables rules
grow linearly. With 100+ VMs, linear rule scan per packet becomes measurable.

Source: [`docs/network-setup.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/network-setup.md)

### Pattern B: Shared bridge

```bash
ip link add name br0 type bridge
ip addr add 172.20.0.1/24 dev br0
ip link set dev br0 up
brctl addif br0 tap0
brctl addif br0 tap1
# Single NAT rule for the entire bridge subnet
iptables -t nat -A POSTROUTING -o eth0 -s 172.20.0.0/24 -j MASQUERADE
```

Single NAT rule, but introduces a shared L2 domain -- VMs can see each other's
broadcast traffic. **Gotcha:** `net.bridge.bridge-nf-call-iptables` must be `0`,
otherwise iptables processes bridged traffic and silently drops unicast while
passing ARP broadcasts.

Source: [DevOpsChops: VM-to-VM via bridges](https://devopschops.com/blog/communicating-between-firecracker-microvms-using-bridges/)

### Pattern C: Network namespace + veth + TAP (recommended for clones)

From Firecracker's [`docs/snapshotting/network-for-clones.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/network-for-clones.md):

```bash
# Per-clone setup
ip netns add fc0
ip netns exec fc0 ip tuntap add name vmtap0 mode tap
ip netns exec fc0 ip addr add 192.168.241.1/29 dev vmtap0
ip netns exec fc0 ip link set vmtap0 up
# veth pair connecting namespace to host
ip link add name veth1 type veth peer name veth0 netns fc0
ip netns exec fc0 ip addr add 10.0.0.2/24 dev veth0
ip netns exec fc0 ip link set dev veth0 up
ip addr add 10.0.0.1/24 dev veth1
ip link set dev veth1 up
ip netns exec fc0 ip route add default via 10.0.0.1
# NAT inside namespace
ip netns exec fc0 iptables -t nat -A POSTROUTING -s 192.168.241.1/29 -o veth0 -j MASQUERADE
# NAT on host for veth traffic
iptables -t nat -A POSTROUTING -s 10.0.0.0/30 -o $UPSTREAM -j MASQUERADE
```

Multiple VMs can reuse the same TAP name and guest IP because each lives in a
separate namespace. firecracker-containerd reports TC filter setups use **10-20%
fewer CPU cycles** than bridge setups.

## Jailer `--netns` integration

The jailer's `--netns <path>` calls `setns(fd, CLONE_NEWNET)` to join a pre-existing
namespace before constructing the chroot (step 12 of jailer execution).

Workflow for CI:
1. Orchestrator creates netns: `ip netns add ci-job-<id>`
2. Orchestrator configures networking inside the netns
3. Jailer invoked with `--netns /var/run/netns/ci-job-<id>`
4. Jailer joins namespace, builds jail, execs Firecracker
5. Firecracker creates TAP device inside the namespace (via `/dev/net/tun` in jail)
6. On job completion: `ip netns del ci-job-<id>` destroys everything

Source: [`docs/jailer.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/jailer.md)

## iptables vs nftables at scale

For 100+ concurrent VMs with individual NAT rules, nftables is dramatically better:

**NAT throughput (SKUDONET benchmarks):**

| Operation | iptables | nftables | Improvement |
|-----------|----------|----------|-------------|
| DNAT | ~256k req/s/core | ~561k req/s/core | +118% |
| SNAT | ~262k req/s/core | ~609k req/s/core | +132% |

**CPU overhead at 100k pps (Red Hat benchmarks):**

| Tool | Extra CPU cost |
|------|---------------|
| iptables | ~40.77% |
| nftables | ~17.27% |

**Rule scaling:** iptables checks rules linearly -- O(N) per packet. nftables uses
set-based matching (hash lookups) -- O(1) regardless of VM count. A single nft rule
referencing a set of VM addresses replaces N individual iptables rules.

Firecracker's `network-setup.md` already uses `iptables-nft` (the nftables-compatible
iptables binary), suggesting awareness of this.

Source: [SKUDONET benchmarks](https://www.skudonet.com/knowledge-base/nftlb/nftlb-benchmarks-and-performance-keys/),
[Red Hat: Benchmarking nftables](https://developers.redhat.com/blog/2017/04/11/benchmarking-nftables)

## CNI plugin: tc-redirect-tap

[`awslabs/tc-redirect-tap`](https://github.com/awslabs/tc-redirect-tap) is a CNI plugin
from AWS designed specifically for Firecracker. It chains with standard CNI plugins and
converts their output into a TAP device:

1. A prior CNI plugin (e.g., `ptp`) creates a veth pair and assigns an IP via IPAM
2. `tc-redirect-tap` creates a TAP device in the same namespace
3. Linux TC U32 filters redirect packets between the veth and TAP
4. Guest gets the same MAC and IP as the veth device

```json
{
  "cniVersion": "0.3.1",
  "name": "fcnet",
  "plugins": [
    {
      "type": "ptp",
      "ipMasq": true,
      "ipam": { "type": "host-local", "subnet": "192.168.1.0/24" }
    },
    { "type": "tc-redirect-tap" }
  ]
}
```

**Undocumented environment variables** (required for jailer integration):
```bash
TC_REDIRECT_TAP_UID=$uid
TC_REDIRECT_TAP_GID=$gid
TC_REDIRECT_TAP_NAME=tap1
```

The Firecracker Go SDK handles the full CNI lifecycle: creates namespace, invokes CNI,
starts VMM, handles cleanup on shutdown.

Source: [awslabs/tc-redirect-tap](https://github.com/awslabs/tc-redirect-tap),
[0x74696d: Networking for a Firecracker Lab](https://blog.0x74696d.com/posts/networking-firecracker-lab/)

## IP assignment: static, not DHCP

Every production Firecracker deployment uses **static IP via kernel boot params**:

```
ip=172.16.0.2::172.16.0.1:255.255.255.252::eth0:off
```

The orchestrator assigns the IP and passes it as a kernel boot arg. No DHCP negotiation
(which adds 1-5s). For snapshot clones with identical guest IPs, external uniqueness is
achieved via per-namespace SNAT/DNAT rules.

With `host-local` IPAM (via CNI), IPs are allocated from a subnet range and stored in
files under `/var/lib/cni/networks/<name>/`. The Go SDK handles this automatically.

Source: [`docs/network-setup.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/network-setup.md)

## DNS resolution

Four approaches, ordered by suitability for CI:

| Approach | Complexity | Notes |
|----------|------------|-------|
| Bake `/etc/resolv.conf` into golden image | Simplest | Write `nameserver 8.8.8.8` during image build |
| Kernel boot params + `/proc/net/pnp` | Low | Symlink `/etc/resolv.conf` -> `/proc/net/pnp`; max 2 nameservers |
| CNI `resolvConf` inheritance | Medium | `host-local` IPAM reads host's `/etc/resolv.conf` |
| MMDS metadata injection | Medium | Guest-side init must parse and apply |

For forge-metal, baking DNS into the golden image is simplest.

## MAC address generation

Firecracker requires a `guest_mac` field in the network interface config (no auto-generation).

**IP-derived pattern** (Firecracker's documented convention):
```
06:00:AC:10:00:02  =  06:00 (locally-administered unicast) + 172.16.0.2 in hex
```

Formula: `06:00:<ip_byte1>:<ip_byte2>:<ip_byte3>:<ip_byte4>`. Collision-free as long
as IPs are unique. The first octet must have bit 1 set (locally administered) and
bit 0 clear (unicast).

Source: [`docs/network-setup.md`](https://github.com/firecracker-microvm/firecracker/blob/main/docs/network-setup.md)

## Built-in rate limiting

Firecracker implements a **dual token bucket** rate limiter per network interface:

```json
{
  "rx_rate_limiter": {
    "bandwidth": { "size": 10485760, "refill_time": 1000 },
    "ops": { "size": 1000, "refill_time": 1000 }
  },
  "tx_rate_limiter": {
    "bandwidth": { "size": 10485760, "one_time_burst": 5242880, "refill_time": 1000 }
  }
}
```

Two independent buckets: bandwidth (bytes/sec) and operations (packets/sec),
independently configurable for RX and TX. `one_time_burst` provides initial credit
(useful for `git clone` burst at job start). Over-consumption is allowed but forces
proportional wait. Hot-reconfigurable via PATCH API.

| Aspect | Firecracker built-in | tc (tbf, htb) | iptables (hashlimit) |
|--------|---------------------|---------------|---------------------|
| Granularity | Per-VM, per-interface | Per-device or class | Per-flow or IP |
| Hot reconfig | Yes (PATCH API) | Yes (`tc qdisc change`) | Yes (rule replace) |
| Overhead | Minimal (userspace) | Kernel qdisc | Kernel netfilter |
| CI suitability | Best -- per-job, API-driven | Host-level limits | Aggregate limits |

Source: [Firecracker Rate Limiting](https://codecatalog.org/articles/firecracker-rate-limiting/)

## Performance at scale

Firecracker's own testing at N=100 and N=1000 active VMs:

| Metric | N=100 (basic) | N=100 (namespace) | N=1000 (basic) | N=1000 (namespace) |
|--------|--------------|-------------------|----------------|-------------------|
| Ping latency avg | 0.315ms | +10-20us | 0.305ms | 0.318ms |
| iperf throughput avg | 2.25 Gbps | Same | ~440 Mbps | ~430 Mbps |

Ping latency stays flat at 1000 VMs. The throughput drop reflects aggregate bandwidth
saturation, not per-VM overhead.

Source: Firecracker network-for-clones testing data

## IPv6 support

Firecracker's `network-setup.md` states IPv4 is assumed, adapt for IPv6. The virtio-net
device operates at L2 (Ethernet frames) so IPv6 works. firecracker-containerd reports
"IPv6 verified in prototypes." However, MMDS uses IPv4 only, and kernel boot param IP
format for IPv6 is poorly documented. For CI jobs needing only outbound internet, IPv4
NAT is simpler.

## Recommended pattern for forge-metal

1. **Per-job network namespace** -- isolates networking, simplifies cleanup
2. **tc-redirect-tap via CNI** with `host-local` IPAM -- automated IP allocation
3. **nftables** with set-based NAT -- O(1) lookup at any VM count
4. **Static IP via kernel boot params** -- zero DHCP overhead
5. **DNS baked into golden image** -- no per-job config needed
6. **IP-derived MAC addresses** -- deterministic, collision-free
7. **Firecracker built-in rate limiter** -- per-job bandwidth via API
8. **Jailer `--netns`** -- joins pre-created namespace

## Sources

- [network-setup.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/network-setup.md)
- [network-for-clones.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/snapshotting/network-for-clones.md)
- [jailer.md](https://github.com/firecracker-microvm/firecracker/blob/main/docs/jailer.md)
- [awslabs/tc-redirect-tap](https://github.com/awslabs/tc-redirect-tap)
- [firecracker-containerd networking.md](https://github.com/firecracker-microvm/firecracker-containerd/blob/main/docs/networking.md)
- [Firecracker Go SDK](https://pkg.go.dev/github.com/firecracker-microvm/firecracker-go-sdk)
- [Firecracker Rate Limiting](https://codecatalog.org/articles/firecracker-rate-limiting/)
- [firecracker-demo](https://github.com/firecracker-microvm/firecracker-demo)
- [Cloudflare TAP internals](https://blog.cloudflare.com/virtual-networking-101-understanding-tap/)
- [SKUDONET benchmarks](https://www.skudonet.com/knowledge-base/nftlb/nftlb-benchmarks-and-performance-keys/)
- [Red Hat: Benchmarking nftables](https://developers.redhat.com/blog/2017/04/11/benchmarking-nftables)
- [0x74696d: Networking for a Firecracker Lab](https://blog.0x74696d.com/posts/networking-firecracker-lab/)
- [DevOpsChops: VM-to-VM via bridges](https://devopschops.com/blog/communicating-between-firecracker-microvms-using-bridges/)
