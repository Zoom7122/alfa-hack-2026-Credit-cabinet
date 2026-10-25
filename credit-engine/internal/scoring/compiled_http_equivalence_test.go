package scoring

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// A temporary adapter runs the compiled model through the existing concrete
// Evaluator seam. Production assembly is changed only in plan step 13.
// The original HTTP tests, expected payloads, optimizer, bank and assertions
// are used unchanged; no score/route/financial result is stubbed.
func TestCompiledV2AcrossExistingHTTPAcceptance(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "internal", "scoring", "scoring.go")
	raw, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	source, err := parser.ParseFile(fset, file, raw, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	bridge, err := parser.ParseFile(fset, "bridge.go", `package scoring;func bridge(){return compiledCompatibilityEvaluate(e,ctx,r)}`, 0)
	if err != nil {
		t.Fatal(err)
	}
	body := bridge.Decls[0].(*ast.FuncDecl).Body
	replaced := 0
	for _, decl := range source.Decls {
		f, ok := decl.(*ast.FuncDecl)
		if !ok || f.Recv == nil || f.Name.Name != "Evaluate" {
			continue
		}
		receiver, ok := f.Recv.List[0].Type.(*ast.StarExpr)
		if !ok {
			continue
		}
		name, ok := receiver.X.(*ast.Ident)
		if !ok || name.Name != "Evaluator" {
			continue
		}
		f.Body = body
		replaced++
	}
	if replaced != 1 {
		t.Fatal("evaluator seam changed", replaced)
	}
	var rewritten bytes.Buffer
	if err = format.Node(&rewritten, fset, source); err != nil {
		t.Fatal(err)
	}
	adapter := `package scoring
import("context";"errors";"sync";"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/contracts";"github.com/Zoom7122/alfa-hack-2026-Credit-cabinet/credit-engine/internal/modelbindings")
var compiledCompatibilityOnce sync.Once
var compiledCompatibilityCatalog *modelbindings.Catalog
var compiledCompatibilityError error
func compiledCompatibilityEvaluate(e *Evaluator,ctx context.Context,r contracts.DecisionRequest)(Assessment,error){
 p,err:=PrepareApplication(r,dateOnly(e.clock.Now()));if err!=nil{return Assessment{},err}
 compiledCompatibilityOnce.Do(func(){compiledCompatibilityCatalog,compiledCompatibilityError=modelbindings.NewDefault()});if compiledCompatibilityError!=nil{return Assessment{},compiledCompatibilityError}
 version,err:=modelbindings.LegacyVersion(r.Borrower.Type);if err!=nil{return Assessment{},err};entry,err:=compiledCompatibilityCatalog.Lookup(version,r.Borrower.Type);if err!=nil{return Assessment{},err}
 stops,err:=entry.Engine().Stops(p.Input);if err!=nil{return Assessment{},err}
 if len(stops)==0&&r.Borrower.Type==contracts.BorrowerTypeLegalEntity {
  if e.source==nil{return Assessment{},&Error{Status:502,Code:"STATEMENTS_UNAVAILABLE"}}
  response,sourceErr:=e.source.Statements(ctx,r.Borrower.ID,[]int{p.Date.Year()-2,p.Date.Year()-1})
  if errors.Is(sourceErr,ErrStatementsNotFound){}else if sourceErr!=nil{var failure *Error;if errors.As(sourceErr,&failure){return Assessment{},failure};status:=502;var timeout interface{Timeout()bool};if errors.Is(sourceErr,context.DeadlineExceeded)||errors.As(sourceErr,&timeout)&&timeout.Timeout(){status=504};return Assessment{},&Error{Status:status,Code:"STATEMENTS_UNAVAILABLE"}}else{p,err=WithStatements(p,response);if err!=nil{return Assessment{},err}}
 }
 result,err:=EvaluatePrepared(entry.Engine(),p);if err!=nil{return Assessment{},err};return ToAssessment(result)
}
`
	temp := t.TempDir()
	replacement := filepath.Join(temp, "scoring.go")
	if err = os.WriteFile(replacement, rewritten.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	adapterFile := filepath.Join(temp, "compatibility.go")
	if err = os.WriteFile(adapterFile, []byte(adapter), 0600); err != nil {
		t.Fatal(err)
	}
	overlay := struct{ Replace map[string]string }{map[string]string{file: replacement, filepath.Join(root, "internal", "scoring", "compiled_compatibility_overlay.go"): adapterFile}}
	data, err := json.Marshal(overlay)
	if err != nil {
		t.Fatal(err)
	}
	overlayFile := filepath.Join(temp, "overlay.json")
	if err = os.WriteFile(overlayFile, data, 0600); err != nil {
		t.Fatal(err)
	}
	goBinary := "go"
	if runtime.GOOS == "windows" {
		goBinary += ".exe"
	}
	testNames := map[string]bool{}
	for _, pkg := range []string{"scoring", "app"} {
		files, err := filepath.Glob(filepath.Join(root, "internal", pkg, "*_test.go"))
		if err != nil {
			t.Fatal(err)
		}
		for _, file := range files {
			parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			for _, decl := range parsed.Decls {
				f, ok := decl.(*ast.FuncDecl)
				if ok && f.Recv == nil && strings.HasPrefix(f.Name.Name, "Test") && f.Name.Name != "TestCompiledV2AcrossExistingHTTPAcceptance" {
					testNames[f.Name.Name] = true
				}
			}
		}
	}
	names := []string{}
	for name := range testNames {
		names = append(names, regexp.QuoteMeta(name))
	}
	sort.Strings(names)
	pattern := "^(" + strings.Join(names, "|") + ")$"
	command := exec.Command(filepath.Join(runtime.GOROOT(), "bin", goBinary), "test", "-overlay="+overlayFile, "-count=1", "-shuffle=on", "-json", "-run="+pattern, "./internal/scoring", "./internal/app")
	command.Dir = root
	output, runErr := command.CombinedOutput()
	events := bytes.Split(output, []byte("\n"))
	passed, failed := 0, 0
	for _, line := range events {
		var event struct{ Action, Test, Output string }
		if json.Unmarshal(line, &event) != nil {
			continue
		}
		if event.Action == "pass" && event.Test != "" {
			passed++
		}
		if event.Action == "fail" {
			failed++
		}
		if event.Action == "output" && event.Output != "" && runErr != nil {
			t.Log(event.Output)
		}
	}
	if runErr != nil || failed != 0 || passed == 0 {
		t.Fatalf("compiled v2 HTTP acceptance failed: %v (%d pass/%d fail)\n%s", runErr, passed, failed, output)
	}
	t.Logf("compiled v2 scoring + HTTP acceptance: %d tests/subtests passed, original tests/fixtures unchanged", passed)
}
