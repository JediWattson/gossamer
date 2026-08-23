package engineparity

import (
	"context"
	"net/url"
	"testing"

	"github.com/JediWattson/gossamer/internal/browser"
	"github.com/JediWattson/gossamer/internal/dom"
	"github.com/JediWattson/gossamer/internal/nativeengine"
)

func TestStrandArrayBuiltinParity(t *testing.T) {
	runArrayBuiltinParity(t, nativeengine.New(nativeengine.Config{}))
}

func runArrayBuiltinParity(t *testing.T, engine browser.Engine) {
	t.Helper()
	browserRuntime, err := browser.NewWithEngine(engine)
	if err != nil {
		t.Fatal(err)
	}
	location, _ := url.Parse("https://parity.gossamer.test/array-builtins.html")
	page, err := browserRuntime.NewPage(dom.NewDocument(), location)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := page.QueueScript(browser.ScriptSource{URL: location.String(), Source: `
let parityConcatenated = [1, 2].concat([3, 4], 5);
let parityShifted = [1, 2];
let parityUnshiftLength = parityShifted.unshift(0);
let parityFirst = parityShifted.shift();
let paritySpliced = [1, 2, 3, 4];
let parityRemoved = paritySpliced.splice(1, 2, 7, 8, 9);
let paritySorted = [10, 2, 1].sort();
let parityNumericSorted = [10, 2, 1].sort((left, right) => left - right);
let paritySparse = [, 3, undefined, 1, , 2].sort((left, right) => left - right);
let parityFromString = Array.from("A😃B");
let parityFromSet = Array.from(new Set([2, 4]), (value, index) => value + index);
let parityFromLike = Array.from({ length: 3, 0: "a", 2: "c" });
let parityFilled = new Array(5).fill("x", -3, 4);
let parityReversed = [1, 2, 3].reverse();
let paritySparseReversed = [, 2, 3].reverse();
let parityFoundIndex = [3, 7, 9].findIndex(value => value > 5);
let parityMissingIndex = [3, 7, 9].findIndex(value => value > 10);
let parityMapSource = [1, 2, 3];
let parityMappedMutation = parityMapSource.map((value, index) => {
  if (index === 0) { delete parityMapSource[1]; parityMapSource[2] = 4; }
  return value * 2;
});
let parityFilterSource = [1, 2, 3];
let parityFilteredMutation = parityFilterSource.filter((value, index) => {
  if (index === 0) { delete parityFilterSource[1]; parityFilterSource[2] = 4; }
  return true;
});
let parityForEachSource = [1, 2, 3];
let parityForEachMutation = "";
parityForEachSource.forEach((value, index) => {
  if (index === 0) { delete parityForEachSource[1]; parityForEachSource[2] = 4; }
  parityForEachMutation += value;
});
let parityReduceSource = [1, 2, 3];
let parityReducedMutation = parityReduceSource.reduce((total, value, index) => {
  if (index === 0) { delete parityReduceSource[1]; parityReduceSource[2] = 4; }
  return total + value;
}, 0);
let paritySomeSource = [1, 2, 3];
let paritySomeMutation = paritySomeSource.some((value, index) => {
  if (index === 0) { delete paritySomeSource[1]; paritySomeSource[2] = 4; }
  return value === 4;
});
let parityEverySource = [1, 2, 3];
let parityEveryMutation = parityEverySource.every((value, index) => {
  if (index === 0) { delete parityEverySource[1]; parityEverySource[2] = 4; }
  return value < 5;
});
let parityFlatMapSource = [1, 2, 3];
let parityFlatMappedMutation = parityFlatMapSource.flatMap((value, index) => {
  if (index === 0) { delete parityFlatMapSource[1]; parityFlatMapSource[2] = 4; }
  return [value, value];
});
if (parityConcatenated.join(",") !== "1,2,3,4,5" ||
    parityUnshiftLength !== 3 || parityFirst !== 0 || parityShifted.join(",") !== "1,2" ||
    parityRemoved.join(",") !== "2,3" || paritySpliced.join(",") !== "1,7,8,9,4" ||
    paritySorted.join(",") !== "1,10,2" || parityNumericSorted.join(",") !== "1,2,10" ||
    paritySparse.join("|") !== "1|2|3|||" || (4 in paritySparse) || (5 in paritySparse) ||
    parityFromString.join("|") !== "A|😃|B" || parityFromSet.join(",") !== "2,5" ||
    parityFromLike.join("|") !== "a||c" || parityFilled.join("|") !== "||x|x|" ||
    parityReversed.join(",") !== "3,2,1" || paritySparseReversed.join("|") !== "3|2|" ||
    (2 in paritySparseReversed) || parityFoundIndex !== 1 || parityMissingIndex !== -1 ||
    parityMappedMutation.join("|") !== "2||8" || (1 in parityMappedMutation) ||
    parityFilteredMutation.join(",") !== "1,4" || parityForEachMutation !== "14" ||
    parityReducedMutation !== 5 || paritySomeMutation !== true || parityEveryMutation !== true ||
    parityFlatMappedMutation.join(",") !== "1,1,4,4") {
  throw new Error("Array mutation builtin parity failed");
}
`}); err != nil {
		t.Fatal(err)
	}
	if err := page.Realm.RunOne(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := page.Close(); err != nil {
		t.Fatalf("close page: %v", err)
	}
	if stats := browserRuntime.Ledger().Stats(); stats.LiveObjects != 0 || stats.PersistentObjects != 0 {
		t.Fatalf("Array builtin teardown ownership = %#v", stats)
	}
	if err := browserRuntime.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
}
