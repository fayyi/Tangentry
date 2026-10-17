package layering

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func dependencyViolation(layer, importPath string) string {
	imports := func(path string) bool {
		return importPath == "tangentry/internal/"+path || strings.HasPrefix(importPath, "tangentry/internal/"+path+"/")
	}
	switch {
	case layer == "cmd" && !(imports("app") || imports("server") || imports("desktop") || imports("jobs")):
		return "command entry point imports a non-composition package"
	case layer == "adapters" && (imports("app") || imports("server") || imports("transport")):
		return "adapter imports an outer layer"
	case layer == "app" && imports("server"):
		return "composition root imports shell server"
	case layer == "transport" && (imports("app") || imports("server") || imports("adapters")):
		return "transport imports an outer or concrete layer"
	case layer == "server" && (imports("app") || imports("adapters") || imports("transport")):
		return "server imports an inner concrete or transport layer"
	case layer != "cmd" && layer != "app" && layer != "adapters" && layer != "server" && layer != "transport" &&
		(imports("app") || imports("server") || imports("transport") || imports("adapters") || imports("desktop")):
		return "domain package imports an outer or concrete layer"
	default:
		return ""
	}
}

func TestDependencyPolicy(t *testing.T) {
	tests := []struct {
		layer, importPath string
		wantViolation     bool
	}{
		{"transport", "tangentry/internal/app", true},
		{"transport", "tangentry/internal/server", true},
		{"transport", "tangentry/internal/adapters/sqlite", true},
		{"transport", "tangentry/internal/content", false},
		{"cmd", "tangentry/internal/app", false},
		{"cmd", "tangentry/internal/server", false},
		{"cmd", "tangentry/internal/desktop", false},
		{"cmd", "tangentry/internal/content", true},
		{"app", "tangentry/internal/server", true},
		{"app", "tangentry/internal/adapters/sqlite", false},
		{"server", "tangentry/internal/transport/http", true},
		{"content", "tangentry/internal/adapters/sqlite", true},
		{"content", "tangentry/internal/desktop", true},
	}
	for _, test := range tests {
		t.Run(test.layer+"_"+strings.TrimPrefix(test.importPath, "tangentry/internal/"), func(t *testing.T) {
			got := dependencyViolation(test.layer, test.importPath) != ""
			if got != test.wantViolation {
				t.Fatalf("dependencyViolation(%q, %q) = %v, want %v", test.layer, test.importPath, got, test.wantViolation)
			}
		})
	}
}

// TestProductionDependencyDirection keeps the package boundary executable
// while allowing app to assemble adapters and transport.
func TestProductionDependencyDirection(t *testing.T) {
	err := fs.WalkDir(os.DirFS("../.."), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || !strings.HasPrefix(filepath.ToSlash(path), "internal/") || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		relative := filepath.ToSlash(path)
		parts := strings.Split(relative, "/")
		if len(parts) < 2 {
			return nil
		}
		top := parts[1]
		if parts[0] == "cmd" {
			top = "cmd"
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join("../..", path), nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		for _, spec := range file.Imports {
			importPath := strings.Trim(spec.Path.Value, `"`)
			if violation := dependencyViolation(top, importPath); violation != "" {
				t.Errorf("%s: %s imports %s", violation, path, spec.Path.Value)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
