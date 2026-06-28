package tool

import (
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// nodeCheck writes js to a temp file and runs `node --check` on it, which parses
// the source and fails on any syntax error without executing it (so missing
// browser globals like document/XPathResult don't matter). Skips if node is
// absent.
func nodeCheck(t *testing.T, name, js string) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; skipping JS syntax check")
	}
	// `node --check` wants a statement source; an IIFE expression is fine when
	// assigned, so wrap it to be unambiguous.
	f, err := os.CreateTemp(t.TempDir(), name+"-*.js")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("const _x = " + js + ";\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()
	out, err := exec.Command(node, "--check", f.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("node --check failed for %s: %v\n%s", name, err, out)
	}
}

// selectors that previously broke the string-interpolated JS: an XPath whose
// predicate contains single quotes produced "SyntaxError: missing ) after
// argument list".
var trickySelectors = []string{
	`//a[contains(text(),'決算')]`,
	`#progress_list`,
	`(//div[@class="x"])[1]`,
	`a[href*="schedule"]`,
	`button[type="submit"]`,
	`'); alert(1); //`, // injection attempt must stay inert (a string literal)
}

func TestVerifyClickJS_Syntax(t *testing.T) {
	for _, sel := range trickySelectors {
		nodeCheck(t, "verify", verifyClickJS(sel, "left"))
	}
}

func TestCleanElementJS_Syntax(t *testing.T) {
	for _, sel := range append([]string{""}, trickySelectors...) {
		nodeCheck(t, "clean", cleanElement(5, sel))
	}
}

func TestMultiStepJS_Syntax(t *testing.T) {
	steps := make([]jsStep, 0, len(trickySelectors))
	for _, sel := range trickySelectors {
		steps = append(steps, jsStep{Action: "select", Selector: sel, Value: `"quoted" & <value>`, XPath: isXPathSelector(sel)})
	}
	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	nodeCheck(t, "multistep", multiStepJS(string(stepsJSON)))
}

func TestMultiExtractJS_Syntax(t *testing.T) {
	specs := []jsExtract{{Name: "x", Selector: trickySelectors[0], XPath: true}}
	for i := range defaultExtractSpecs {
		specs = append(specs, defaultExtractSpecs[i])
	}
	b, err := json.Marshal(specs)
	if err != nil {
		t.Fatal(err)
	}
	nodeCheck(t, "multiextract", multiExtractJS(string(b), 50))
}

func TestPageStatsJS_Syntax(t *testing.T) {
	for _, cfg := range []statsConfig{
		{Top: 50},
		{Selector: trickySelectors[0], XPath: true, Top: 10},
		{Selector: "#main .list", XPath: false, Top: 25},
	} {
		b, err := json.Marshal(cfg)
		if err != nil {
			t.Fatal(err)
		}
		nodeCheck(t, "pagestats", pageStatsJS(string(b)))
	}
}

func TestIsXPathSelector(t *testing.T) {
	cases := map[string]bool{
		`//a[@id="x"]`:    true,
		`/html/body`:      true,
		`(//div)[1]`:      true,
		`.//span`:         true,
		`#id`:             false,
		`.cls`:            false,
		`a[href="/x"]`:    false,
		`button[type=x]`:  false,
	}
	for sel, want := range cases {
		if got := isXPathSelector(sel); got != want {
			t.Errorf("isXPathSelector(%q) = %v, want %v", sel, got, want)
		}
	}
	// sanity: the XPath branch should never be taken for a plain CSS id
	if strings.HasPrefix("#id", "/") {
		t.Fatal("unreachable")
	}
}
