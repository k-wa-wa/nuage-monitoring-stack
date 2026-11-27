package internal_test

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const modulePrefix = "github.com/k-wa-wa/nuage-monitoring-stack/monitoring-pwa/backend/"

type listedPackage struct {
	ImportPath string
	Dir        string
	GoFiles    []string
	Imports    []string
}

func listModulePackages(t *testing.T) []listedPackage {
	t.Helper()
	cmd := exec.Command("go", "list", "-json", "./...")
	cmd.Dir = ".."
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list -json ./... failed: %v", err)
	}

	var pkgs []listedPackage
	dec := json.NewDecoder(bytes.NewReader(out))
	for dec.More() {
		var p listedPackage
		if err := dec.Decode(&p); err != nil {
			t.Fatalf("decode go list json failed: %v", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs
}

func TestArchitectureDependencies(t *testing.T) {
	pkgs := listModulePackages(t)

	forbiddenInUsecase := map[string]bool{
		"database/sql":                     true,
		"modernc.org/sqlite":               true,
		"github.com/gorilla/websocket":     true,
		"github.com/SherClockHolmes/webpush-go": true,
	}

	for _, p := range pkgs {
		rel := strings.TrimPrefix(p.ImportPath, modulePrefix)
		isSubpkg := func(prefix string) bool {
			return rel == prefix || strings.HasPrefix(rel, prefix+"/")
		}

		switch {
		case isSubpkg("internal/domain"):
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, modulePrefix+"internal/") {
					t.Errorf("domain (%s) must not import internal packages, found %s", rel, imp)
				}
				if strings.HasPrefix(imp, "github.com/labstack/echo") || forbiddenInUsecase[imp] {
					t.Errorf("domain (%s) must not import framework/driver %s", rel, imp)
				}
			}
			// domain の型には JSON タグを付けない
			fset := token.NewFileSet()
			for _, gf := range p.GoFiles {
				fileAst, err := parser.ParseFile(fset, filepath.Join(p.Dir, gf), nil, 0)
				if err != nil {
					t.Fatalf("parse %s failed: %v", gf, err)
				}
				ast.Inspect(fileAst, func(n ast.Node) bool {
					st, ok := n.(*ast.StructType)
					if !ok || st.Fields == nil {
						return true
					}
					for _, f := range st.Fields.List {
						if f.Tag == nil {
							continue
						}
						tagVal := strings.Trim(f.Tag.Value, "`")
						if _, hasJSON := reflect.StructTag(tagVal).Lookup("json"); hasJSON {
							t.Errorf("domain struct in %s must not have json tags: %s", gf, f.Tag.Value)
						}
					}
					return true
				})
			}

		case isSubpkg("internal/usecase"):
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, modulePrefix+"internal/adapter") ||
					strings.HasPrefix(imp, modulePrefix+"internal/infra") ||
					strings.HasPrefix(imp, modulePrefix+"internal/config") {
					t.Errorf("usecase (%s) must not import %s", rel, imp)
				}
				if strings.HasPrefix(imp, "github.com/labstack/echo") || forbiddenInUsecase[imp] {
					t.Errorf("usecase (%s) must not import framework/driver %s", rel, imp)
				}
			}

		case isSubpkg("internal/adapter"):
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, modulePrefix+"internal/infra") {
					t.Errorf("adapter (%s) must not import infra (%s)", rel, imp)
				}
				if imp == "database/sql" || imp == "modernc.org/sqlite" || imp == "github.com/SherClockHolmes/webpush-go" {
					t.Errorf("adapter (%s) must not import infra driver (%s)", rel, imp)
				}
			}

		case isSubpkg("internal/infra"):
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, modulePrefix+"internal/adapter") {
					t.Errorf("infra (%s) must not import adapter (%s)", rel, imp)
				}
				if strings.HasPrefix(imp, "github.com/labstack/echo") {
					t.Errorf("infra (%s) must not import web framework (%s)", rel, imp)
				}
			}

		case isSubpkg("internal/config"):
			for _, imp := range p.Imports {
				if strings.HasPrefix(imp, modulePrefix+"internal/") {
					t.Errorf("config (%s) must not import internal packages, found %s", rel, imp)
				}
			}

		case rel == "internal":
			if len(p.GoFiles) > 0 {
				t.Errorf("internal root package must not contain non-test Go files: %v", p.GoFiles)
			}

		case isSubpkg("cmd"):
			// cmd 配下は各層を組み立てる配線層のため制限しない

		default:
			t.Errorf("unexpected package outside standard layers: %s", rel)
		}
	}
}
