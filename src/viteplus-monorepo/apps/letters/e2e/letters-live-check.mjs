// Static-blog smoke check: SSR HTML for /, a known post, and a 404 path.
// No browser, no auth — Letters is now a code-authored static blog.

const baseURL = (process.env.TEST_BASE_URL ?? "http://127.0.0.1:4247").replace(/\/$/, "");
const knownSlug = process.env.TEST_KNOWN_SLUG ?? "hello-world";
const knownTitleFragment = process.env.TEST_KNOWN_TITLE ?? "Hello, world";

async function fetchExpect(path, expectStatus) {
  const url = `${baseURL}${path}`;
  const res = await fetch(url, { redirect: "manual" });
  if (res.status !== expectStatus) {
    throw new Error(`GET ${url}: expected ${expectStatus}, got ${res.status}`);
  }
  const body = await res.text();
  return { url, status: res.status, body };
}

function assertContains(body, fragment, label) {
  if (!body.includes(fragment)) {
    throw new Error(`${label}: response did not contain ${JSON.stringify(fragment)}`);
  }
}

const checks = [];

const home = await fetchExpect("/", 200);
assertContains(home.body, "<title>Letters", "GET /");
assertContains(home.body, '<meta name="description"', "GET / description meta");
assertContains(home.body, knownSlug, "GET / known slug link");
checks.push({ path: "/", status: home.status });

const post = await fetchExpect(`/${knownSlug}`, 200);
assertContains(post.body, knownTitleFragment, `GET /${knownSlug}`);
assertContains(post.body, 'property="og:type" content="article"', `GET /${knownSlug} og:type`);
assertContains(post.body, "application/ld+json", `GET /${knownSlug} JSON-LD`);
assertContains(post.body, '<link rel="canonical"', `GET /${knownSlug} canonical`);
checks.push({ path: `/${knownSlug}`, status: post.status });

const missing = await fetchExpect("/this-post-does-not-exist", 404);
checks.push({ path: "/this-post-does-not-exist", status: missing.status });

console.log(JSON.stringify({ ok: true, baseURL, checks }, null, 2));
