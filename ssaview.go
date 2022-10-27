// ssaview is a small utlity that renders SSA code alongside input Go code
//
// Runs via HTTP on :8080 or the PORT environment variable
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"io"
	"net/http"
	"os"
	"sort"

	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/ssa/ssautil"
)

const indexPage = "index.html"

type members []ssa.Member

func (m members) Len() int           { return len(m) }
func (m members) Swap(i, j int)      { m[i], m[j] = m[j], m[i] }
func (m members) Less(i, j int) bool { return m[i].Pos() < m[j].Pos() }

// toSSA converts go source to SSA
func toSSA(source io.Reader, fileName string, debug bool) ([]byte, error) {
	// Parse the source files.
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, fileName, source, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	files := []*ast.File{f}

	// Create the type-checker's package.
	pkg := types.NewPackage(f.Name.String(), "")

	// Type-check the package, load dependencies.
	// Create and build the SSA program.
	ssap, _, err := ssautil.BuildPackage(
		&types.Config{Importer: importer.Default()}, fset, pkg, files, ssa.NaiveForm|ssa.BuildSerially)
	if err != nil {
		return nil, err
	}

	ssap.SetDebugMode(debug)

	out := new(bytes.Buffer)

	ssap.WriteTo(out)

	var funcs = members([]ssa.Member{})

	var visit func(*ssa.Function)
	visit = func(f *ssa.Function) {
		for _, anon := range f.AnonFuncs {
			visit(anon) // anon is done building before f.
		}

		funcs = append(funcs, f)
	}

	for _, mem := range ssap.Members {
		if fn, ok := mem.(*ssa.Function); ok {
			visit(fn)
		}
	}
	// sort by Pos()
	sort.Sort(funcs)
	for _, f := range funcs {
		if fn, ok := f.(*ssa.Function); ok {
			fn.WriteTo(out)
		}
	}
	return out.Bytes(), nil
}

// writeJSON attempts to serialize data and write it to w
// On error it will write an HTTP status of 400
func writeJSON(w http.ResponseWriter, data interface{}) error {
	if err, ok := data.(error); ok {
		data = struct{ Error string }{err.Error()}
		w.WriteHeader(400)
	}
	o, err := json.MarshalIndent(data, "", "   ")
	if err != nil {
		return err
	}
	_, err = w.Write(o)
	return err
}

func main() {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		f, err := os.Open(indexPage)
		if err != nil {
			writeJSON(w, err)
		}
		io.Copy(w, f)
	})
	http.HandleFunc("/ssa", func(w http.ResponseWriter, r *http.Request) {
		ssa, err := toSSA(r.Body, "main.go", false)
		if err != nil {
			writeJSON(w, err)
			return
		}
		defer r.Body.Close()
		writeJSON(w, struct{ All string }{string(ssa)})
	})
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	fmt.Println(http.ListenAndServe(":"+port, nil))
}
