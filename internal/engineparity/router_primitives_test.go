package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandSolidRouterPrimitiveParity(t *testing.T) {
	browserRuntime, err := browser.NewWithEngine(nativeengine.New(nativeengine.Config{}))
	if err != nil {
		t.Fatal(err)
	}
	defer browserRuntime.Close()
	location, _ := url.Parse("https://parity.gossamer.test/global")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	defer page.Close()

	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
const trimPathRegex = /^\/+|(\/)\/+$/g;
function normalizePath(path, omitSlash = false) {
  const s = path.replace(trimPathRegex, "$1");
  return s ? omitSlash || /^[?#]/.test(s) ? s : "/" + s : "";
}
function joinPaths(from, to) {
  return normalizePath(from).replace(/\/*(\*.*)?$/g, "") + normalizePath(to);
}
function asArray(value) { return Array.isArray(value) ? value : [value]; }
function createMatcher(path) {
  const [pattern, splat] = path.split("/*", 2);
  const segments = pattern.split("/").filter(Boolean);
  const len = segments.length;
  return location => {
    const locSegments = location.split("/").filter(Boolean);
    const lenDiff = locSegments.length - len;
    if (lenDiff < 0 || (lenDiff > 0 && splat === undefined)) return null;
    for (let i = 0; i < len; i++) {
      const segment = segments[i];
      const dynamic = segment[0] === ":";
      const locSegment = dynamic ? locSegments[i] : locSegments[i].toLowerCase();
      const key = dynamic ? segment.slice(1) : segment.toLowerCase();
      if (!dynamic && locSegment !== key) return null;
    }
    return { path: location };
  };
}
function scoreRoute(route) {
  const [pattern, splat] = route.pattern.split("/*", 2);
  const segments = pattern.split("/").filter(Boolean);
  return segments.reduce((score, segment) => score + (segment.startsWith(":" ) ? 2 : 3), segments.length - (splat === undefined ? 0 : 1));
}
const definitions = [{ path: ["/", "/:placeID"], name: "chat" }, { path: "/404", name: "404" }, { path: "*", name: "wildcard" }];
const branches = [];
for (const definition of definitions) {
  for (const originalPath of asArray(definition.path)) {
    const pattern = joinPaths("", originalPath);
    const route = { pattern, name: definition.name };
    branches.push({ route, score: scoreRoute(route), matcher: createMatcher(pattern) });
  }
}
branches.sort((left, right) => right.score - left.score);
const selected = branches.find(branch => branch.matcher(location.pathname));
if (!selected || selected.route.name !== "chat" || selected.route.pattern !== "/:placeID") {
  throw new Error("Solid Router primitive match failed: " + branches.map(branch => branch.route.name + "=" + branch.route.pattern + "@" + branch.score).join(",") + ":" + location.pathname + ":selected=" + (selected && selected.route.name));
}
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
}
