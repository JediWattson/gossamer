//go:build v8 && cgo && darwin && arm64

package v8engine

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/JediWattson/gossamer/internal/browser"
)

func TestStockV8FocusedWebPlatformSubsets(t *testing.T) {
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	page, err := browserRuntime.LoadPage(ctx, "https://wpt.gossamer.test/", staticDocumentLoader{
		document: `<!doctype html><html><body>
			<main id="root"><button id="target">target</button></main>
			<form id="form"><input id="required" name="query" required><button id="submitter" name="commit" value="yes">go</button></form>
		</body></html>`,
	})
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}

	cases := []struct {
		name   string
		source string
	}{
		{
			name: "dom-nodes",
			source: `
				(() => {
					const root = document.getElementById("root");
					const target = document.getElementById("target");
					const clone = root.cloneNode(true);
					if (!(root instanceof HTMLElement) || root.ownerDocument !== document ||
						document.querySelector("#target") !== target || !target.isConnected ||
						clone.isConnected || clone.firstChild === target || clone.textContent !== "target") {
						throw new Error("focused WPT DOM node subset failed");
					}
					const fragment = document.createDocumentFragment();
					fragment.append(clone);
					if (fragment.firstChild !== clone || clone.parentNode !== fragment) {
						throw new Error("focused WPT fragment subset failed");
					}
				})();
			`,
		},
		{
			name: "dom-events",
			source: `
				(() => {
					const root = document.getElementById("root");
					const target = document.getElementById("target");
					const order = [];
					root.addEventListener("wpt", event => order.push("capture:" + event.eventPhase), {capture: true});
					target.addEventListener("wpt", event => { order.push("target:" + event.eventPhase); event.preventDefault(); });
					root.addEventListener("wpt", event => order.push("bubble:" + event.eventPhase));
					const event = new Event("wpt", {bubbles: true, cancelable: true});
					if (target.dispatchEvent(event) || !event.defaultPrevented ||
						order.join(",") !== "capture:1,target:2,bubble:3") {
						throw new Error("focused WPT EventTarget subset failed: " + order.join(","));
					}
				})();
			`,
		},
		{
			name: "dom-collections",
			source: `
				(() => {
					const root = document.getElementById("root");
					const children = root.children;
					const node = document.createElement("span");
					node.id = "live";
					node.classList.add("one", "two");
					node.dataset.owner = "gossamer";
					root.append(node);
					if (root.children !== children || children.length !== 2 || children[1] !== node ||
						[...children].join("").indexOf("[object") === -1 ||
						node.className !== "one two" || node.dataset.owner !== "gossamer" ||
						node.getAttribute("data-owner") !== "gossamer") {
						throw new Error("focused WPT live collection subset failed");
					}
				})();
			`,
		},
		{
			name: "html-forms",
			source: `
				(() => {
					const form = document.getElementById("form");
					const input = document.getElementById("required");
					const submitter = document.getElementById("submitter");
					let invalid = 0;
					input.addEventListener("invalid", () => invalid++);
					if (form.checkValidity() || invalid !== 1) throw new Error("focused WPT validity subset failed");
					input.value = "regions";
					const data = new FormData(form, submitter);
					data.append("queue", "arc");
					if (!form.checkValidity() || data.get("query") !== "regions" || data.get("commit") !== "yes" ||
						JSON.stringify([...data]) !== JSON.stringify([["query", "regions"], ["commit", "yes"], ["queue", "arc"]])) {
						throw new Error("focused WPT FormData subset failed");
					}
				})();
			`,
		},
	}

	for _, testCase := range cases {
		if _, err := page.QueueScript(browser.ScriptSource{
			URL:    "https://wpt.gossamer.test/" + testCase.name + ".js",
			Source: testCase.source,
		}); err != nil {
			t.Fatalf("queue %s: %v", testCase.name, err)
		}
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("run %s: %v", testCase.name, err)
		}
	}
	for page.Realm.Tasks.Len() != 0 {
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("drain focused WPT subset tasks: %v", err)
		}
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("focused WPT teardown ownership = %#v", ledger)
	}
}

func TestStockV8ReplacementSubmissionLeakAndPerformanceGate(t *testing.T) {
	const (
		submissionCycles  = 25
		replacementCycles = 200
		wallBudget        = 20 * time.Second
		wrapperBudget     = 2500
	)

	started := time.Now()
	engine := newTestEngine(t)
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatalf("NewWithEngine: %v", err)
	}
	defer browserRuntime.Close()
	ctx, cancel := context.WithTimeout(context.Background(), wallBudget)
	defer cancel()
	document := `<!doctype html><html><body>
		<form id="gate" action="/gate-next">
			<input name="cycle" value="native">
			<button id="submit" name="commit" value="yes">go</button>
		</form>
		<p>replacement gate</p>
	</body></html>`
	client := staticDocumentLoader{document: document}
	page, err := browserRuntime.LoadPage(ctx, "https://gate.gossamer.test/start", client)
	if err != nil {
		t.Fatalf("LoadPage: %v", err)
	}
	page.SetFormNavigationLoader(client)

	for cycle := 0; cycle < submissionCycles; cycle++ {
		if cycle%5 == 0 {
			realm, ok := engine.LatestRealm()
			if !ok {
				t.Fatal("submission gate lost the live Realm")
			}
			if err := realm.CollectGarbage(page); err != nil {
				t.Fatalf("submission GC %d: %v", cycle, err)
			}
		}
		if _, err := page.QueueScript(browser.ScriptSource{
			URL: "https://gate.gossamer.test/submit.js",
			Source: `document.getElementById("gate").requestSubmit(
				document.getElementById("submit")
			);`,
		}); err != nil {
			t.Fatalf("queue submission %d: %v", cycle, err)
		}
		if err := page.Realm.RunOne(context.Background()); err != nil {
			t.Fatalf("run submission %d: %v", cycle, err)
		}
		navigation := page.Navigation().ID
		if err := page.WaitNavigation(ctx, navigation); err != nil {
			t.Fatalf("wait submission %d: %v", cycle, err)
		}
	}

	for cycle := 0; cycle < replacementCycles; cycle++ {
		if cycle%25 == 0 {
			realm, ok := engine.LatestRealm()
			if !ok {
				t.Fatal("replacement gate lost the live Realm")
			}
			if err := realm.CollectGarbage(page); err != nil {
				t.Fatalf("replacement GC %d: %v", cycle, err)
			}
		}
		navigation, err := page.Navigate(
			ctx,
			fmt.Sprintf("https://gate.gossamer.test/replacement/%d", cycle),
			client,
		)
		if err != nil {
			t.Fatalf("navigate replacement %d: %v", cycle, err)
		}
		if err := page.WaitNavigation(ctx, navigation); err != nil {
			t.Fatalf("wait replacement %d: %v", cycle, err)
		}
	}

	history, current := page.History()
	if current != len(history)-1 || len(history) < submissionCycles+replacementCycles+1 {
		t.Fatalf("replacement history length=%d current=%d", len(history), current)
	}
	latest, ok := engine.LatestRealm()
	if !ok {
		t.Fatal("gate has no final Realm")
	}
	if err := latest.CollectGarbage(page); err != nil {
		t.Fatalf("final forced GC: %v", err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("Close page: %v", err)
	}

	profile := engine.Profile()
	if profile.RealmsCreated != profile.RealmsClosed || profile.RealmsCreated < submissionCycles+replacementCycles+2 {
		t.Fatalf("Realm teardown profile = %#v", profile)
	}
	// ClosedRealms.LiveWrappers is the pre-delete diagnostic snapshot; native
	// ownership after deletion is asserted through the ledger below.
	if profile.ClosedRealms.LiveCallbacks != 0 || profile.ClosedRealms.EventListeners != 0 {
		t.Fatalf("closed Realm handles = %#v", profile.ClosedRealms)
	}
	if profile.ClosedRealms.WrappersCreated > wrapperBudget || profile.ClosedRealms.LiveWrappers > wrapperBudget {
		t.Fatalf("wrapper regression: created %d live-at-close %d, budget %d",
			profile.ClosedRealms.WrappersCreated, profile.ClosedRealms.LiveWrappers, wrapperBudget)
	}
	if profile.ClosedRealms.Evaluations < submissionCycles || profile.ClosedRealms.EventsDispatched < submissionCycles || profile.ClosedRealms.MajorGCs == 0 {
		t.Fatalf("gate did not exercise evaluation, events, and forced GC: %#v", profile.ClosedRealms)
	}
	if ledger := browserRuntime.Ledger().Stats(); ledger.LiveObjects != 0 || ledger.PersistentObjects != 0 {
		t.Fatalf("replacement gate ownership = %#v", ledger)
	}
	if elapsed := time.Since(started); elapsed > wallBudget {
		t.Fatalf("replacement gate took %s, budget %s", elapsed, wallBudget)
	} else {
		t.Logf("gate replacements=%d submissions=%d realms=%d wrappers=%d elapsed=%s",
			replacementCycles, submissionCycles, profile.RealmsCreated,
			profile.ClosedRealms.WrappersCreated, elapsed)
	}
}
