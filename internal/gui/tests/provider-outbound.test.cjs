// Run with Node's test runner and Playwright on the module path; see README.md.
// A provider's Outbound protocol is the API its requests leave on. Automatic
// is today's choice. The control offers only the APIs that have a URL, opens
// as saved, and Save posts it. A signed-in account has no such row.
const assert = require("node:assert/strict");
const fs = require("node:fs/promises");
const path = require("node:path");
const { test } = require("node:test");
const { chromium, webkit } = require("playwright");

const assets = path.resolve(__dirname, "../assets");
const unfold = async (page) => {
  const adv = page.locator(".editor details.advanced");
  await adv.waitFor({ state: "attached" });
  if (!(await adv.evaluate((d) => d.open))) await adv.locator(":scope > summary").click();
};
const base = {
  icon: "generic", chat: "", responses: "", anthropic: "", gemini: "", catalog: "",
  models: [{ id: "m1", name: "M1", on: true }], agents: [], fallback: [], headers: {},
  key: { set: true, masked: "sk-…1234" }, keyList: [], balanceToken: { takes: false, set: false },
  proxy: "", searches: false, unredacted: false, passUserAgent: false,
};
const relay = { ...base, id: "relay", name: "Relay", chat: "https://relay.example/v1", anthropic: "https://relay.example" };
const pinned = { ...relay, id: "pinned", name: "Pinned", outbound: "anthropic" };
const chatOnly = { ...base, id: "chatty", name: "Chatty", chat: "https://chat.example/v1" };
const account = {
  id: "claude", name: "Claude", icon: "claude-color", chat: "", responses: "", anthropic: "https://api.anthropic.com", catalog: "",
  models: [{ id: "claude-sonnet", name: "Sonnet", on: true }], agents: [], fallback: [], headers: {}, keyList: [],
  account: { agent: "claude", agentName: "Claude", user: "me@example.com", plan: "pro", logins: [{ user: "me@example.com", plan: "pro", active: true, on: true }] },
  proxy: "", outbound: "anthropic",
};

function serve(lang, posts) {
  const providers = { providers: [relay, pinned, chatOnly, account], presets: [], excluded: [], gateway: { running: true, window: true } };
  const state = { agents: [], profiles: [], settings: { lang, theme: "light" } };
  return async (route) => {
    const url = new URL(route.request().url());
    const json = (data) => route.fulfill({ json: data });
    if (url.pathname === "/boot.js") return route.fulfill({ contentType: "text/javascript", body: `window.bootPrefs = {lang:"${lang}",theme:"light",web:true};` });
    if (url.pathname === "/wails/runtime.js") return route.fulfill({ contentType: "text/javascript", body: "export const Window = {};" });
    if (url.pathname === "/api/state") return json(state);
    if (url.pathname === "/api/providers") return json(providers);
    if (url.pathname === "/api/groups") return json({ groups: [] });
    if (url.pathname.startsWith("/api/provider/") && route.request().method() === "POST") {
      posts.push({ action: url.pathname.slice("/api/provider/".length), body: route.request().postDataJSON() });
      return json(providers);
    }
    if (url.pathname.startsWith("/api/")) return json({});
    const file = path.join(assets, url.pathname === "/" ? "index.html" : url.pathname);
    const contentType = { ".html": "text/html", ".js": "text/javascript", ".css": "text/css", ".svg": "image/svg+xml", ".png": "image/png" }[path.extname(file)];
    await route.fulfill({ body: await fs.readFile(file), contentType });
  };
}

const strings = [
  "Outbound protocol",
  "The API requests to this provider leave on. Automatic is today's choice. When it matches the agent's API, only the model name and key are replaced.",
];
const words = {
  en: { label: "Outbound protocol", auto: "Automatic", chat: "OpenAI compatible", anthropic: "Anthropic compatible", save: "Save" },
  zh: { label: "出站协议", auto: "自动", chat: "OpenAI 兼容", anthropic: "Anthropic 兼容", save: "保存" },
  "zh-TW": { label: "出站協定", auto: "自動", chat: "OpenAI 相容", anthropic: "Anthropic 相容", save: "儲存" },
  ja: { label: "送信プロトコル", auto: "自動", chat: "OpenAI 互換", anthropic: "Anthropic 互換", save: "保存" },
  de: { label: "Ausgehendes Protokoll", auto: "Automatisch", chat: "OpenAI-kompatibel", anthropic: "Anthropic-kompatibel", save: "Speichern" },
};

for (const engine of (process.env.BROWSER ? [process.env.BROWSER] : ["chromium", "webkit"])) {
  for (const lang of Object.keys(words)) {
    for (const width of [900, 440]) {
      const w = words[lang];
      const open = async (t, name) => {
        const browser = await (engine === "webkit" ? webkit.launch() : chromium.launch({ channel: "chromium" }));
        t.after(async () => {
          if (process.env.ARTIFACT_DIR) {
            await fs.mkdir(process.env.ARTIFACT_DIR, { recursive: true });
            await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-${name}-outbound.png`), fullPage: true });
          }
          await browser.close();
        });
        const page = await (await browser.newContext({ viewport: { width, height: 760 }, reducedMotion: "reduce" })).newPage();
        page.setDefaultTimeout(5000);
        const errors = [];
        page.on("pageerror", (e) => errors.push(e.message));
        const posts = [];
        await page.route("**/*", serve(lang, posts));
        await page.goto("http://magpie.test/?view=providers");
        await page.locator(".row.provider", { hasText: name }).click();
        await unfold(page);
        await page.locator(".editor .bar").waitFor();
        return { page, errors, posts };
      };
      const label = (page) => page.locator(".editor label", { hasText: w.label });
      const segs = (page) => label(page).locator("xpath=following-sibling::*[1]");
      const opt = (page, name) => segs(page).getByRole("button", { name, exact: true });
      const save = async (page, posts) => {
        const n = posts.length;
        await page.locator(".editor .bar").getByRole("button", { name: w.save, exact: true }).click();
        for (let i = 0; i < 50 && posts.length === n; i++) await page.waitForTimeout(50);
        return posts.at(-1);
      };

      test(`${engine} ${lang} ${width}: outbound offers the APIs that have a URL, and save posts the pick`, async (t) => {
        const { page, errors, posts } = await open(t, "Relay");
        assert.equal(await label(page).isVisible(), true);
        if (process.env.ARTIFACT_DIR) {
          await segs(page).scrollIntoViewIfNeeded();
          await page.screenshot({ path: path.join(process.env.ARTIFACT_DIR, `${engine}-${lang}-${width}-open-outbound.png`) });
        }
        assert.equal(await opt(page, w.auto).getAttribute("aria-pressed"), "true");
        assert.equal(await opt(page, w.chat).count(), 1);
        assert.equal(await opt(page, w.anthropic).count(), 1);
        const box = await segs(page).boundingBox();
        assert.ok(box && box.width > 60 && box.x + box.width <= width, `the row fits: ${JSON.stringify(box)}`);
        await opt(page, w.anthropic).click();
        const saved = await save(page, posts);
        assert.equal(saved.action, "save");
        assert.equal(saved.body.id, "relay");
        assert.equal(saved.body.outbound, "anthropic");
        if (lang !== "en") {
          const missing = await page.evaluate(([l, ks]) => ks.filter((k) => !I18N[l][k]), [lang, strings]);
          assert.deepEqual(missing, [], `every string has its ${lang}`);
        }
        assert.deepEqual(errors, []);
      });

      test(`${engine} ${lang} ${width}: a saved pin opens pressed, and a chat-only provider offers chat`, async (t) => {
        const pinnedPage = await open(t, "Pinned");
        assert.equal(await opt(pinnedPage.page, w.anthropic).getAttribute("aria-pressed"), "true");
        assert.deepEqual(pinnedPage.errors, []);
        const chatPage = await open(t, "Chatty");
        assert.equal(await opt(chatPage.page, w.chat).count(), 1);
        assert.equal(await opt(chatPage.page, w.anthropic).count(), 0);
        assert.deepEqual(chatPage.errors, []);
      });

      test(`${engine} ${lang} ${width}: a signed-in account has no outbound row`, async (t) => {
        const { page, errors } = await open(t, "Claude");
        assert.equal(await label(page).count(), 0);
        assert.deepEqual(errors, []);
      });
    }
  }
}
