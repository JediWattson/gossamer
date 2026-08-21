package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandLazyForOfGeneratorParity(t *testing.T) {
	runLazyForOfGeneratorParity(t, nativeengine.New(nativeengine.Config{}))
}

func runLazyForOfGeneratorParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/generator.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let visits = 0;
function* matching(values, minimum) {
  for (let value of values) (++visits, value > minimum) && (yield value);
}
let iterator = matching([1, 2, 3], 1);
if (visits !== 0 || iterator[Symbol.iterator]() !== iterator) {
  throw new Error("generator was not lazy or self-iterable");
}
let first = iterator.next();
let second = iterator.next();
let done = iterator.next();
let spread = [...matching([1, 2, 3], 1)];

let closes = 0;
let source = {};
source[Symbol.iterator] = function() {
  let current = 0;
  return {
    next: function() { return {value: ++current, done: false}; },
    return: function() { closes++; return {done: true}; }
  };
};
function* closing(values) { for (let value of values) yield value; }
let closingIterator = closing(source);
let closingFirst = closingIterator.next();
let closingResult = closingIterator.return("closed");
let afterClose = closingIterator.next();

if (first.value !== 2 || first.done !== false || visits !== 6 ||
    second.value !== 3 || second.done !== false || done.done !== true ||
    spread.join(":") !== "2:3" || closingFirst.value !== 1 || closes !== 1 ||
    closingResult.value !== "closed" || closingResult.done !== true || afterClose.done !== true) {
  throw new Error("generator result parity failed");
}
`}); err != nil {
		t.Fatal(err)
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatal(err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("generator teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}
