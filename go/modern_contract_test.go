// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0.
package redis

import (
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Catch accidental reintroduction of old wire commands in runtime code.
// SQL grammar keywords (for example JSON OBJECT WITH UNIQUE KEYS) are unrelated.
func TestModernRedisCommandContract(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	forbidden := map[string]bool{"EVAL": true, "EVALSHA": true, "EVAL_RO": true, "EVALSHA_RO": true, "SCRIPT": true, "KEYS": true, "SMEMBERS": true}
	fs := gotoken.NewFileSet()
	check := func(e ast.Expr) {
		literal, ok := e.(*ast.BasicLit)
		if !ok || literal.Kind != gotoken.STRING {
			return
		}
		value, err := strconv.Unquote(literal.Value)
		if err != nil {
			t.Fatal(err)
		}
		if forbidden[strings.ToUpper(value)] {
			t.Errorf("%s: legacy wire command %s", fs.Position(e.Pos()), value)
		}
	}
	for _, file := range files {
		if strings.HasSuffix(file, "_test.go") {
			continue
		}
		parsed, err := goparser.ParseFile(fs, file, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			switch n := node.(type) {
			case *ast.CompositeLit:
				if len(n.Elts) > 0 {
					check(n.Elts[0])
				}
			case *ast.CallExpr:
				if fn, ok := n.Fun.(*ast.Ident); ok && fn.Name == "once" && len(n.Args) > 2 {
					check(n.Args[2])
				}
				if fn, ok := n.Fun.(*ast.SelectorExpr); ok {
					switch fn.Sel.Name {
					case "Eval", "EvalSha", "SMembers", "NewScript":
						t.Errorf("%s: legacy Redis helper %s", fs.Position(n.Pos()), fn.Sel.Name)
					case "Do", "NewCmd", "NewStringCmd", "NewIntCmd":
						if len(n.Args) > 1 {
							check(n.Args[1])
						}
					}
				}
			}
			return true
		})
	}
}
