# Zig 0.15 Breaking API Changes

Reference for migrating production Zig code from 0.13/0.14 to 0.15.x (tested against 0.15.2).

Changes are grouped by severity of impact. Each entry notes whether the change was deprecated in 0.14
and removed in 0.15 ("dep-0.14, rm-0.15") or is entirely new to 0.15 ("new-0.15").

---

## 1. I/O Overhaul ("Writergate") [new-0.15]

The single most disruptive change. The entire `std.io` Reader/Writer generic interface was replaced
with concrete, non-generic types where **the buffer lives in the interface, not the implementation**.

**Source:** [0.15.1 Release Notes -- Writergate](https://ziglang.org/download/0.15.1/release-notes.html),
[Writergate PR #24329](https://github.com/ziglang/zig/pull/24329)

### 1a. Writing to stdout

```zig
// OLD (0.13/0.14)
const stdout = std.io.getStdOut().writer();
try stdout.print("Hello {s}\n", .{"world"});

// NEW (0.15)
var stdout_buffer: [4096]u8 = undefined;
var stdout_writer = std.fs.File.stdout().writer(&stdout_buffer);
const stdout = &stdout_writer.interface;
try stdout.print("Hello {s}\n", .{"world"});
try stdout.flush();  // REQUIRED -- without this, nothing appears
```

For unbuffered output, pass an empty slice: `std.fs.File.stdout().writer(&.{})`.

### 1b. Generic writer functions

```zig
// OLD (0.13/0.14) -- writer: anytype
fn writeGreeting(writer: anytype) !void {
    try writer.print("hello\n", .{});
}

// NEW (0.15) -- concrete pointer type
fn writeGreeting(writer: *std.Io.Writer) std.Io.Writer.Error!void {
    try writer.print("hello\n", .{});
}
```

### 1c. Buffered writers/readers

```zig
// OLD (0.13/0.14)
var bw = std.io.bufferedWriter(file.writer());
const writer = bw.writer();
// ... writes ...
try bw.flush();

// NEW (0.15) -- buffer is caller-provided
var buf: [4096]u8 = undefined;
var file_writer = file.writer(&buf);
const writer = &file_writer.interface;
// ... writes ...
try writer.flush();
```

`std.io.BufferedWriter` and `std.io.bufferedReader` are deleted.

### 1d. Reading lines

```zig
// OLD (0.13/0.14)
var reader = std.io.bufferedReader(file.reader());
while (try reader.reader().readUntilDelimiterOrEof(&buf, '\n')) |line| { ... }

// NEW (0.15)
var read_buf: [4096]u8 = undefined;
var file_reader = file.reader(&read_buf);
const reader: *std.Io.Reader = &file_reader.interface;
while (reader.takeDelimiterExclusive('\n')) |line| {
    reader.toss(1);  // consume the delimiter
    // use line
} else |err| switch (err) {
    error.EndOfStream => {},
    else => return err,
}
```

### 1e. Allocating writer (in-memory)

```zig
// OLD (0.13/0.14) -- ArrayList-based
var buf = std.ArrayList(u8).init(allocator);
defer buf.deinit();
try std.fmt.format(buf.writer(), "value={d}", .{42});
const result = buf.items;

// NEW (0.15) -- std.Io.Writer.Allocating
var string: std.Io.Writer.Allocating = .init(allocator);
defer string.deinit();
try string.writer.print("value={d}", .{42});
const result = string.written();
```

**Gotcha:** `std.Io.Writer.Allocating` can reallocate, invalidating any previously saved slice
references. Track offsets, not `[]const u8` pointers.
([Writergate migration experience -- Ziggit](https://ziggit.dev/t/writergate-migration-experience/12095))

### 1f. Fixed-buffer writer

```zig
// OLD (0.13/0.14) -- std.io.fixedBufferStream
var fbs = std.io.fixedBufferStream(&buf);
const writer = fbs.writer();

// NEW (0.15) -- still exists in 0.15, REMOVED in 0.16
// std.io.fixedBufferStream(&buf) still works in 0.15.x
// Future: std.Io.Writer.fixed(&buf)
```

`std.io.fixedBufferStream` survives in 0.15 but is removed in 0.16.

### 1g. Type renames

| Old (0.13/0.14)          | New (0.15)                     |
|--------------------------|--------------------------------|
| `std.io.GenericReader`   | `std.Io.Reader`                |
| `std.io.GenericWriter`   | `std.Io.Writer`                |
| `std.io.AnyReader`       | `std.Io.Reader`                |
| `std.io.AnyWriter`       | `std.Io.Writer`                |
| `std.io.CountingWriter`  | Deleted -- use `std.Io.Writer.Discarding` with `.count` |
| `std.io.BufferedWriter`  | Deleted -- buffer is now caller-provided |
| `std.io.BufferedReader`  | Deleted -- buffer is now caller-provided |
| `std.io.SeekableStream`  | Deleted                        |
| `std.io.BitReader`       | Deleted                        |
| `std.io.BitWriter`       | Deleted                        |
| `std.fifo.LinearFifo`    | Deleted                        |
| `std.RingBuffer`         | Deleted -- subsumed by `std.Io.Reader`/`Writer` |

### 1h. File Reader/Writer

```zig
// OLD (0.13/0.14)
const reader = file.reader();
const writer = file.writer();

// NEW (0.15) -- old methods renamed to deprecated*
const reader = file.deprecatedReader();  // old behavior, will be removed
const writer = file.deprecatedWriter();  // old behavior, will be removed

// Preferred: new concrete types with caller-provided buffer
var buf: [4096]u8 = undefined;
var reader = file.reader(&buf);       // returns std.fs.File.Reader
var writer = file.writer(&buf);       // returns std.fs.File.Writer
```

### 1i. Adapter for incremental migration

```zig
// Bridge old anytype-based code to the new API
fn legacyWrite(old_writer: anytype) !void {
    var adapter = old_writer.adaptToNewApi(&.{});
    const w: *std.Io.Writer = &adapter.new_interface;
    try w.print("{s}", .{"bridged"});
}
```

---

## 2. ArrayList: Unmanaged by Default [dep-0.14, rm-0.15]

Deprecated in 0.14, the old names are removed in 0.15. `std.ArrayList` is now the **unmanaged**
variant (no stored allocator). The managed variant moves to `std.array_list.Managed`.

**Source:** [0.15.1 Release Notes](https://ziglang.org/download/0.15.1/release-notes.html),
[0.14.0 Release Notes](https://ziglang.org/download/0.14.0/release-notes.html)

```zig
// OLD (0.13/0.14) -- allocator stored in the list
var list = std.ArrayList(u8).init(allocator);
defer list.deinit();
try list.append(42);
try list.appendSlice("hello");
const slice = try list.toOwnedSlice();

// NEW (0.15) -- allocator passed per call
var list: std.ArrayList(u8) = .{};  // no .init(), just empty literal
defer list.deinit(allocator);
try list.append(allocator, 42);
try list.appendSlice(allocator, "hello");
const slice = try list.toOwnedSlice(allocator);
```

If you need the old managed behavior:

```zig
var list = std.array_list.Managed(u8).init(allocator);
defer list.deinit();
try list.append(42);  // no allocator argument needed
```

Note: `std.ArrayListUnmanaged` still exists as a deprecated alias for `std.ArrayList`.
Both managed and unmanaged will "eventually be removed entirely" per the release notes.

---

## 3. JSON Serialization [new-0.15]

The top-level `std.json.stringify()` function no longer exists. Serialization now goes through
`std.json.Stringify` (a type) or `std.json.fmt` (for use in format strings).

**Source:** [std/json.zig source](https://github.com/ziglang/zig/blob/master/lib/std/json.zig),
[std.json.fmt update PR #24505](https://github.com/ziglang/zig/issues/24468)

### 3a. Serialize to allocated string

```zig
// OLD (0.13/0.14)
const json = try std.json.stringify(value, .{}, allocator);
// or
var buf: [1024]u8 = undefined;
var fbs = std.io.fixedBufferStream(&buf);
try std.json.stringify(value, .{}, fbs.writer());

// NEW (0.15) -- Stringify.valueAlloc
const json = try std.json.Stringify.valueAlloc(allocator, value, .{});
defer allocator.free(json);
```

### 3b. Serialize to a writer

```zig
// OLD (0.13/0.14)
try std.json.stringify(value, .{}, writer);

// NEW (0.15) -- Stringify.value takes *std.Io.Writer
try std.json.Stringify.value(my_value, .{}, writer);  // writer is *std.Io.Writer
```

### 3c. Serialize via format strings

```zig
// NEW (0.15) -- std.json.fmt returns a Formatter for use with print
var string: std.Io.Writer.Allocating = .init(allocator);
defer string.deinit();
try string.writer.print("{f}", .{std.json.fmt(value, .{})});
const json = string.written();
```

### 3d. Custom JSON serialization

Types can implement a `jsonStringify` method:

```zig
pub fn jsonStringify(self: *const @This(), jws: *std.json.Stringify) !void {
    // custom serialization logic
}
```

### 3e. Parsing (mostly unchanged)

```zig
// Same in 0.14 and 0.15
const parsed = try std.json.parseFromSlice(MyStruct, allocator, json_bytes, .{});
defer parsed.deinit();
const value = parsed.value;
```

---

## 4. Build System Changes

### 4a. root_module field required [dep-0.14, rm-0.15]

The old `root_source_file`, `target`, and `optimize` fields passed directly to `addExecutable` etc.
were deprecated in 0.14 and **removed in 0.15**.

**Source:** [0.15.1 Release Notes](https://ziglang.org/download/0.15.1/release-notes.html),
[0.14.0 Release Notes](https://ziglang.org/download/0.14.0/release-notes.html)

```zig
// OLD (0.13/0.14)
const exe = b.addExecutable(.{
    .name = "myapp",
    .root_source_file = b.path("src/main.zig"),
    .target = target,
    .optimize = optimize,
});

// NEW (0.15)
const exe = b.addExecutable(.{
    .name = "myapp",
    .root_module = b.createModule(.{
        .root_source_file = b.path("src/main.zig"),
        .target = target,
        .optimize = optimize,
    }),
});
```

### 4b. addStaticLibrary / addSharedLibrary removed [dep-0.14, rm-0.15]

**Source:** [Ziggit discussion](https://ziggit.dev/t/how-to-convert-addstaticlibrary-to-addlibrary/12753),
[addLibrary PR #22554](https://github.com/ziglang/zig/pull/22554)

```zig
// OLD (0.13/0.14)
const lib = b.addStaticLibrary(.{
    .name = "mylib",
    .root_source_file = b.path("src/lib.zig"),
    .target = target,
    .optimize = optimize,
});

// NEW (0.15) -- unified addLibrary with linkage parameter
const lib = b.addLibrary(.{
    .name = "mylib",
    .linkage = .static,  // or .dynamic
    .root_module = b.createModule(.{
        .root_source_file = b.path("src/lib.zig"),
        .target = target,
        .optimize = optimize,
    }),
});
```

Linkage can be made configurable:

```zig
const linkage = b.option(std.builtin.LinkMode, "linkage", "Linkage mode") orelse .static;
const lib = b.addLibrary(.{ .linkage = linkage, .name = "mylib", .root_module = mod });
```

### 4c. UBSan configuration type change [new-0.15]

```zig
// OLD (0.14)
exe.root_module.sanitize_c = true;

// NEW (0.15) -- enum instead of bool
exe.root_module.sanitize_c = .full;   // was true
exe.root_module.sanitize_c = .off;    // was false
exe.root_module.sanitize_c = .trap;   // new option
```

---

## 5. Format String and Custom Formatter Changes [new-0.15]

### 5a. Custom format method signature

The `format` method signature changed to match the new Writer API.

**Source:** [0.15.1 Release Notes](https://ziglang.org/download/0.15.1/release-notes.html)

```zig
// OLD (0.13/0.14)
pub fn format(
    self: @This(),
    comptime fmt_string: []const u8,
    options: std.fmt.FormatOptions,
    writer: anytype,
) !void {
    try writer.print("value={d}", .{self.value});
}
// Usage: std.debug.print("{}", .{instance});

// NEW (0.15)
pub fn format(
    self: @This(),
    writer: *std.Io.Writer,
) std.Io.Writer.Error!void {
    try writer.print("value={d}", .{self.value});
}
// Usage: std.debug.print("{f}", .{instance});  // NOTE: {f} not {}
```

### 5b. {f} specifier required for format methods

Types with a `format` method now require `{f}` in the format string. Using `{}` is ambiguous.
Use `{any}` to explicitly skip the format method.

```zig
// OLD (0.14) -- {} would call format method
std.debug.print("{}", .{my_formattable_thing});

// NEW (0.15) -- {f} explicitly invokes format method
std.debug.print("{f}", .{my_formattable_thing});
// OR skip the format method:
std.debug.print("{any}", .{my_formattable_thing});
```

### 5c. New format specifiers

| Specifier | Purpose |
|-----------|---------|
| `{f}`     | Call the type's `format` method |
| `{t}`     | `@tagName()` / `@errorName()` shorthand |
| `{b64}`   | Standard base64 output |
| `{B}`     | Replaces `std.fmt.fmtIntSizeDec` |
| `{Bi}`    | Replaces `std.fmt.fmtIntSizeBin` |
| `{D}`     | Replaces `std.fmt.fmtDuration` / `fmtDurationSigned` |

### 5d. Formatter helper renames

| Old                           | New                          |
|-------------------------------|------------------------------|
| `std.fmt.Formatter`           | `std.fmt.Alt`                |
| `std.fmt.fmtSliceEscapeLower` | `std.ascii.hexEscape`        |
| `std.fmt.fmtSliceEscapeUpper` | `std.ascii.hexEscape`        |
| `std.zig.fmtEscapes`          | `std.zig.fmtString`          |
| `std.fmt.fmtSliceHexLower`    | Use `{x}` specifier          |
| `std.fmt.fmtSliceHexUpper`    | Use `{X}` specifier          |
| `std.fmt.fmtIntSizeDec`       | Use `{B}` specifier          |
| `std.fmt.fmtIntSizeBin`       | Use `{Bi}` specifier         |
| `std.fmt.fmtDuration`         | Use `{D}` specifier          |
| `std.fmt.format`              | `std.Io.Writer.print`        |

---

## 6. Language Changes

### 6a. usingnamespace removed [new-0.15]

**Source:** [0.15.1 Release Notes](https://ziglang.org/download/0.15.1/release-notes.html)

```zig
// OLD (0.14) -- pull in namespace
pub usingnamespace if (have_foo) struct {
    pub const foo = 123;
} else struct {};

// NEW (0.15) -- explicit declarations only
pub const foo = if (have_foo)
    123
else
    @compileError("foo not supported on this target");
```

### 6b. async/await keywords removed [new-0.15]

The `async`, `await`, `suspend`, `resume` keywords and `@frameSize` builtin are removed. Async
functionality will return via `std.Io` coroutine primitives in the standard library.

### 6c. Arithmetic on undefined [new-0.15]

Only operators that can never trigger Illegal Behavior allow `undefined` as an operand. Other
operations on `undefined` are now compile errors.

```zig
// OLD (0.14) -- allowed
const x: u32 = undefined;
const y = x +% 1;  // wrapping add on undefined was allowed

// NEW (0.15) -- compile error for most operations on undefined
```

### 6d. Lossy integer-to-float coercion [new-0.15]

```zig
// OLD (0.14) -- silent precision loss
const val: f32 = 123_456_789;

// NEW (0.15) -- compile error
// Fix: use a float literal
const val: f32 = 123_456_789.0;
// Or explicitly cast
const val: f32 = @floatFromInt(some_int);
```

### 6e. Variable shadowing [exists since early Zig, enforced more strictly]

Local variables that shadow struct method names, outer scope declarations, or function parameters
are compile errors. This is not new to 0.15 but enforcement expanded.

```zig
fn foo() void {}

pub fn main() void {
    var foo = true;  // ERROR: local shadows declaration of 'foo'
    _ = foo;
}
```

### 6f. Inline assembly: typed clobbers [new-0.15]

```zig
// OLD (0.14) -- string clobbers
: "rcx", "r11"

// NEW (0.15) -- struct clobbers (zig fmt auto-upgrades)
: .{ .rcx = true, .r11 = true }
```

### 6g. @export requires address-of [dep-0.14, rm-0.15]

```zig
// OLD (0.14)
@export(myFunction, .{ .name = "exported" });

// NEW (0.15)
@export(&myFunction, .{ .name = "exported" });
```

### 6h. Packed union alignment specifiers removed [new-0.15]

Packed union fields can no longer specify an `align` attribute. Delete any alignment specifiers on
packed union fields -- they previously had no effect anyway.

---

## 7. Container Type Changes

### 7a. DoublyLinkedList de-genericified [new-0.15]

**Source:** [0.15.1 Release Notes](https://ziglang.org/download/0.15.1/release-notes.html)

```zig
// OLD (0.13/0.14)
const List = std.DoublyLinkedList(MyData);
var list: List = .{};
var node = try allocator.create(List.Node);
node.data = MyData{ .value = 42 };
list.append(node);

// NEW (0.15) -- embed Node in your struct, use @fieldParentPtr
const MyData = struct {
    node: std.DoublyLinkedList.Node = .{},
    value: i32,
};
var list: std.DoublyLinkedList = .{};
var item = MyData{ .value = 42 };
list.append(&item.node);

// To get data from a node:
const data = @fieldParentPtr("node", some_node_ptr);
```

### 7b. BoundedArray removed [new-0.15]

**Source:** [0.15.1 Release Notes](https://ziglang.org/download/0.15.1/release-notes.html)

```zig
// OLD (0.14)
var stack = try std.BoundedArray(i32, 8).fromSlice(initial);

// NEW (0.15) -- use ArrayListUnmanaged with a fixed buffer
var buffer: [8]i32 = undefined;
var stack = std.ArrayListUnmanaged(i32).initBuffer(&buffer);
try stack.appendSliceBounded(initial);
```

### 7c. Ring buffer types removed [new-0.15]

`std.fifo.LinearFifo`, `std.RingBuffer`, and `std.compress.flate.CircularBuffer` are deleted.
Use `std.Io.Reader` / `std.Io.Writer` instead.

---

## 8. Memory and Allocator Changes

### 8a. Page size is now a runtime value [dep-0.14, rm-0.15]

```zig
// OLD (0.14)
const page_size: comptime_int = std.heap.page_size;

// NEW (0.15)
const page_size = std.heap.pageSize();      // runtime function call
const min = std.heap.page_size_min;          // compile-time lower bound
const max = std.heap.page_size_max;          // compile-time upper bound
```

### 8b. Alignment type [dep-0.14, rm-0.15]

Allocator vtable functions now use `std.mem.Alignment` (a type-safe enum with `log2(alignment)` as
the tag) instead of raw `u8` or `u29`.

```zig
// OLD (0.14) -- raw integer
const alignment: u29 = 16;

// NEW (0.15) -- typed enum
const alignment: std.mem.Alignment = .@"16";
```

### 8c. String tokenization [dep-0.14, rm-0.15]

```zig
// OLD (0.14)
var iter = std.mem.tokenize(u8, input, " ");

// NEW (0.15) -- specialized variants
var iter = std.mem.tokenizeScalar(u8, input, ' ');          // single char
var iter2 = std.mem.tokenizeSequence(u8, input, "\r\n");    // multi-char
```

---

## 9. Calling Convention and POSIX Changes [dep-0.14, rm-0.15]

### 9a. Calling convention lowercase

```zig
// OLD (0.14)
fn handler(sig: c_int) callconv(.C) void { ... }

// NEW (0.15) -- lowercase
fn handler(sig: c_int) callconv(.c) void { ... }
```

### 9b. empty_sigset removed

```zig
// OLD (0.14)
const mask = std.posix.empty_sigset;

// NEW (0.15)
const mask = std.mem.zeroes(std.posix.sigset_t);
```

### 9c. sigaction return type

```zig
// OLD (0.14) -- returns error union
std.posix.sigaction(...) catch {};

// NEW (0.15) -- returns void
std.posix.sigaction(...);
```

---

## 10. std.builtin.Type Tags Lowercase [introduced in 0.14]

This landed in 0.14 but is listed here because code written for 0.13 will hit it when upgrading to
0.15.

**Source:** [0.14.0 Release Notes](https://ziglang.org/download/0.14.0/release-notes.html)

```zig
// OLD (0.13)
switch (@typeInfo(T)) {
    .Int => {},
    .Struct => {},
    .Enum => {},
    .Pointer => |ptr| switch (ptr.size) {
        .One => {},
        .Slice => {},
    },
}

// NEW (0.14+, required in 0.15)
switch (@typeInfo(T)) {
    .int => {},
    .@"struct" => {},
    .@"enum" => {},
    .pointer => |ptr| switch (ptr.size) {
        .one => {},
        .slice => {},
    },
}
```

Keywords (`struct`, `enum`, `union`, `opaque`) require `@""` quoting because they are reserved words.

---

## 11. @branchHint replaces @setCold [introduced in 0.14]

Also landed in 0.14, listed for 0.13 upgraders.

**Source:** [0.14.0 Release Notes](https://ziglang.org/download/0.14.0/release-notes.html)

```zig
// OLD (0.13)
@setCold(true);

// NEW (0.14+)
@branchHint(.cold);
@branchHint(.likely);
@branchHint(.unlikely);
@branchHint(.unpredictable);
```

---

## 12. HTTP Client/Server Overhaul [new-0.15]

The HTTP client and server no longer depend on `std.net` directly -- they operate on `std.Io`
streams.

**Source:** [0.15.1 Release Notes](https://ziglang.org/download/0.15.1/release-notes.html)

```zig
// OLD (0.14) -- HTTP client
var server_header_buffer: [1024]u8 = undefined;
var req = try client.open(.GET, uri, .{
    .server_header_buffer = &server_header_buffer,
});
defer req.deinit();
try req.send();
try req.wait();
const body_reader = try req.reader();
var it = req.response.iterateHeaders();
while (it.next()) |header| { ... }

// NEW (0.15) -- HTTP client
var req = try client.request(.GET, uri, .{});
defer req.deinit();
try req.sendBodiless();
var response = try req.receiveHead(&.{});

// Read headers BEFORE calling reader() -- strings invalidated after
var it = response.head.iterateHeaders();
while (it.next()) |header| { ... }

var reader_buffer: [4096]u8 = undefined;
const body_reader = response.reader(&reader_buffer);
```

---

## 13. Namespace Renames

| Old (0.13/0.14)          | New (0.15)                |
|--------------------------|---------------------------|
| `std.rand`               | `std.Random`              |
| `std.TailQueue`          | `std.DoublyLinkedList`    |
| `std.zig.CrossTarget`    | `std.Target.Query`        |
| `std.fs.MAX_PATH_BYTES`  | `std.fs.max_path_bytes`   |
| `std.fmt.format`         | `std.Io.Writer.print`     |

---

## 14. Filesystem Changes [new-0.15]

```zig
// Deleted functions
fs.File.WriteFileOptions     // deleted
fs.File.writeFileAll         // deleted
fs.File.writeFileAllUnseekable  // deleted

// Renames
posix.sendfile               // -> fs.File.Reader.sendFile

// Changed behavior
fs.Dir.copyFile              // no longer fails with error.OutOfMemory
fs.Dir.atomicFile            // now requires write_buffer in options
fs.AtomicFile                // now has File.Writer field instead of File field
```

---

## 15. Compression Changes [new-0.15]

`std.compress.flate` was restructured. Compression functionality was removed (decompression only).

```zig
// NEW (0.15) -- decompression
var decompress_buffer: [std.compress.flate.max_window_len]u8 = undefined;
var decompress: std.compress.flate.Decompress = .init(reader, .zlib, &decompress_buffer);
const decompress_reader: *std.Io.Reader = &decompress.reader;

// For piped streams with no intermediate buffer:
var decompress: std.compress.flate.Decompress = .init(reader, .zlib, &.{});
const n = try decompress.streamRemaining(writer);
```

---

## Quick Checklist for Migration

- [ ] Replace all `std.io.getStdOut().writer()` with buffered `std.fs.File.stdout().writer(&buf)`
- [ ] Add `try stdout.flush()` before program exit and after meaningful output
- [ ] Replace `std.io.bufferedWriter` / `std.io.bufferedReader` with caller-provided buffers
- [ ] Update `ArrayList` usage: pass allocator to every mutating method
- [ ] Replace `std.json.stringify()` with `std.json.Stringify.valueAlloc()` or `.value()`
- [ ] Replace `anytype` writer params with `*std.Io.Writer` in public APIs
- [ ] Update `format` method signatures (remove `fmt_string` and `options` params)
- [ ] Use `{f}` instead of `{}` for types with custom format methods
- [ ] Move `root_source_file` / `target` / `optimize` into `b.createModule(...)` in build.zig
- [ ] Replace `addStaticLibrary` with `addLibrary(.{ .linkage = .static, ... })`
- [ ] Remove any `usingnamespace` usage
- [ ] Remove any `async` / `await` / `suspend` / `resume` usage
- [ ] Use `@export(&fn, ...)` instead of `@export(fn, ...)`
- [ ] Replace `callconv(.C)` with `callconv(.c)`
- [ ] Replace `std.builtin.Type` uppercase tags with lowercase (`.Int` -> `.int`)
- [ ] Replace `std.BoundedArray` with `ArrayListUnmanaged.initBuffer`
- [ ] Update `DoublyLinkedList` usage to embed `Node` in data structs
- [ ] Replace `std.heap.page_size` with `std.heap.pageSize()`
- [ ] Replace `sanitize_c = true/false` with `.full/.off` in build options

---

## Sources

- [Zig 0.15.1 Release Notes](https://ziglang.org/download/0.15.1/release-notes.html) -- primary authoritative source
- [Zig 0.14.0 Release Notes](https://ziglang.org/download/0.14.0/release-notes.html) -- for changes deprecated in 0.14
- [Writergate PR #24329](https://github.com/ziglang/zig/pull/24329) -- the I/O overhaul
- [std.json update PR #24505](https://github.com/ziglang/zig/issues/24468) -- JSON module updated for writergate
- [std.Io migration tracking](https://codeberg.org/ziglang/zig/issues/30150) -- comprehensive I/O migration
- [Zig 0.15.x Migration Guide (community gist)](https://gist.github.com/svenyurgensson/e016010fd544d3301ee1f4aad3c5f64f)
- [Migration roadblocks blog post](https://sngeth.com/zig/systems-programming/breaking-changes/2025/10/24/zig-0-15-migration-roadblocks/)
- [Writergate migration experience (Ziggit)](https://ziggit.dev/t/writergate-migration-experience/12095)
- [Ghostty Zig 0.15 migration tracking](https://github.com/ghostty-org/ghostty/issues/8361)
- [addLibrary PR #22554](https://github.com/ziglang/zig/pull/22554) -- addStaticLibrary replacement
- [addStaticLibrary to addLibrary conversion (Ziggit)](https://ziggit.dev/t/how-to-convert-addstaticlibrary-to-addlibrary/12753)
- [Zig 0.15.1 I/O Overhaul article](https://dev.to/bkataru/zig-0151-io-overhaul-understanding-the-new-readerwriter-interfaces-30oe)
- [std.json.Stringify intrusive interface issue](https://github.com/ziglang/zig/issues/25233)
