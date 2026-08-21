package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandIteratorProtocolParity(t *testing.T) {
	runIteratorProtocolParity(t, nativeengine.New(nativeengine.Config{}))
}

func TestStrandArraySpreadUsesIteratorProtocol(t *testing.T) {
	runArraySpreadParity(t, nativeengine.New(nativeengine.Config{}))
}

func runArraySpreadParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/array-spread.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let iteratorCalls = 0;
let nextReads = 0;
let closes = 0;
function values() {
  let index = 0;
  let iterable = {};
  iterable[Symbol.iterator] = function() {
    iteratorCalls++;
    let iterator = {return: function() { closes++; return {done: true}; }};
    Object.defineProperty(iterator, "next", {
      get: function() {
        nextReads++;
        return function() {
          if (index === 2) return {done: true};
          return {value: index++ === 0 ? "a" : "b", done: false};
        };
      }
    });
    return iterator;
  };
  return iterable;
}
let spread = [0, ...values(), 3];
let overridden = [1, 2];
overridden[Symbol.iterator] = function() { return values()[Symbol.iterator](); };
let overriddenSpread = [...overridden];
let abruptCloses = 0;
let abrupt = {};
abrupt[Symbol.iterator] = function() {
  let result = {};
  Object.defineProperty(result, "done", {get: function() { return false; }});
  Object.defineProperty(result, "value", {get: function() { throw new Error("value failure"); }});
  return {
    next: function() { return result; },
    return: function() { abruptCloses++; return {done: true}; }
  };
};
let failed = false;
try { let ignored = [...abrupt]; } catch (error) { failed = error.message === "value failure"; }
if (spread.join(":") !== "0:a:b:3" || overriddenSpread.join(":") !== "a:b" ||
    iteratorCalls !== 2 || nextReads !== 2 || closes !== 0 || !failed || abruptCloses !== 0) {
  throw new Error("array spread iterator parity failed: " +
    [spread.join(":"), overriddenSpread.join(":"), iteratorCalls, nextReads, closes, failed, abruptCloses].join("|"));
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
		t.Fatalf("array spread teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatal(err)
	}
}

func runIteratorProtocolParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/iterator-protocol.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let iteratorCalls = 0;
let nextReads = 0;
let iteratorCloses = 0;

function iterableOf(values, closeBehavior) {
  let index = 0;
  let iterator = {};
  Object.defineProperty(iterator, "next", {
    configurable: true,
    get: function() {
      nextReads++;
      return function() {
        if (index >= values.length) return {value: undefined, done: true};
        return {value: values[index++], done: false};
      };
    }
  });
  iterator.return = function() {
    iteratorCloses++;
    if (closeBehavior === "throw") throw new Error("close failure");
    if (closeBehavior === "primitive") return 1;
    return {value: undefined, done: true};
  };
  let iterable = {};
  iterable[Symbol.iterator] = function() {
    iteratorCalls++;
    return iterator;
  };
  return iterable;
}

let normal = "";
for (let value of iterableOf(["a", "b"], "object")) normal = normal + value;

let broken = 0;
for (let value of iterableOf([1, 2], "object")) {
  broken = value;
  break;
}

let continued = 0;
for (let value of iterableOf([1, 2], "object")) {
  if (value === 1) continue;
  continued = continued + value;
}

function returnFromLoop() {
  for (let value of iterableOf([7, 8], "object")) return value;
  return 0;
}
let returned = returnFromLoop();

let closeOverridesReturn = false;
try {
  function returnWhileCloseThrows() {
    for (let value of iterableOf([1], "throw")) return value;
    return 0;
  }
  returnWhileCloseThrows();
} catch (error) {
  closeOverridesReturn = error.message === "close failure";
}

let originalThrowPreserved = false;
try {
  for (let value of iterableOf([1], "throw")) throw new Error("body failure");
} catch (error) {
  originalThrowPreserved = error.message === "body failure";
}

let primitiveCloseRejected = false;
try {
  for (let value of iterableOf([1], "primitive")) break;
} catch (error) {
  primitiveCloseRejected = error instanceof TypeError;
}

let overridden = [1, 2];
overridden[Symbol.iterator] = function() {
  return iterableOf([9], "object")[Symbol.iterator]();
};
let overriddenValue = 0;
for (let value of overridden) overriddenValue = value;

outer: for (let first of iterableOf([1, 2], "object")) {
  for (let second of iterableOf([3, 4], "object")) break outer;
}

let stepFailureCloses = 0;
function stepFailureIterable(part) {
  let iterable = {};
  iterable[Symbol.iterator] = function() {
    let iterator = {};
    iterator.return = function() {
      stepFailureCloses++;
      return {done: true};
    };
    if (part === "next") {
      iterator.next = function() { throw new Error("next failure"); };
    } else {
      iterator.next = function() {
        let result = {};
        Object.defineProperty(result, "done", {
          get: function() {
            if (part === "done") throw new Error("done failure");
            return false;
          }
        });
        Object.defineProperty(result, "value", {
          get: function() { throw new Error("value failure"); }
        });
        return result;
      };
    }
    return iterator;
  };
  return iterable;
}
for (let part of ["next", "done", "value"]) {
  try { for (let value of stepFailureIterable(part)) {} } catch (error) {}
}

let nonIterableRejected = false;
try { for (let value of {}) {} } catch (error) { nonIterableRejected = error instanceof TypeError; }
let invalidIteratorRejected = false;
let invalidIterator = {};
invalidIterator[Symbol.iterator] = function() { return 1; };
try { for (let value of invalidIterator) {} } catch (error) { invalidIteratorRejected = error instanceof TypeError; }
let invalidStepRejected = false;
let invalidStep = {};
invalidStep[Symbol.iterator] = function() { return {next: function() { return 1; }}; };
try { for (let value of invalidStep) {} } catch (error) { invalidStepRejected = error instanceof TypeError; }

let arrayText = "";
for (let value of ["x", "y"]) arrayText = arrayText + value;
let stringText = "";
for (let value of "A😀B") stringText = stringText + value;
let map = new Map();
map.set("a", 2); map.set("b", 3);
let mapTotal = 0;
for (let pair of map) mapTotal = mapTotal + pair[1];
let set = new Set();
set.add(4); set.add(5);
let setTotal = 0;
for (let value of set) setTotal = setTotal + value;
let iterator = [4][Symbol.iterator]();

if (normal !== "ab" || broken !== 1 || continued !== 2 || returned !== 7 ||
    !closeOverridesReturn || !originalThrowPreserved || !primitiveCloseRejected || overriddenValue !== 9 ||
    iteratorCalls !== 10 || nextReads !== 10 || iteratorCloses !== 7 || stepFailureCloses !== 0 ||
    !nonIterableRejected || !invalidIteratorRejected || !invalidStepRejected ||
    arrayText !== "xy" || stringText !== "A😀B" || mapTotal !== 5 || setTotal !== 9 ||
    iterator[Symbol.iterator]() !== iterator) {
  throw new Error("iterator protocol parity failed");
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
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("iterator teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
