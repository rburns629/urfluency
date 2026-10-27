package golang

import (
	"go/parser"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/rburns629/urfluency/analysis"
)

// webFrameworks maps module paths to the framework names reported for Go;
// versioned paths such as github.com/gofiber/fiber/v2 match by prefix.
var webFrameworks = map[string]string{
	"github.com/gofiber/fiber":    "fiber",
	"github.com/gin-gonic/gin":    "gin",
	"github.com/labstack/echo":    "echo",
	"github.com/go-chi/chi":       "chi",
	"github.com/gorilla/mux":      "gorilla",
	"google.golang.org/grpc":      "grpc",
	"github.com/valyala/fasthttp": "fasthttp",
}

// Frameworks implements analysis.FrameworkDetector from the file's imports.
func (Analyzer) Frameworks(f analysis.File) []string {
	file, err := parser.ParseFile(token.NewFileSet(), f.Path, f.Content, parser.ImportsOnly)
	if err != nil {
		return nil
	}
	found := map[string]bool{}
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		for module, name := range webFrameworks {
			if p == module || strings.HasPrefix(p, module+"/") {
				found[name] = true
			}
		}
	}
	names := slices.Collect(maps.Keys(found))
	slices.Sort(names)
	return names
}
