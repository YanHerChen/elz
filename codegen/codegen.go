package codegen

import (
	"fmt"
	"strings"

	"github.com/elz-lang/elz/ast"
	"github.com/elz-lang/elz/types"

	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/constant"
	llvmtypes "github.com/llir/llvm/ir/types"
	"github.com/llir/llvm/ir/value"
)

type Generator struct {
	mod     *ir.Module
	bindMap map[string]*ast.Binding

	implsOfBinding       map[string]*ir.Func
	typeOfBuiltInBinding map[string]types.Type
	typeOfBinding        map[string]types.Type
}

func New(bindMap map[string]*ast.Binding) *Generator {
	typMap := make(map[string]types.Type)
	typMap["+(int,int)"] = &types.Int{}
	return &Generator{
		mod:                  ir.NewModule(),
		bindMap:              bindMap,
		implsOfBinding:       make(map[string]*ir.Func),
		typeOfBuiltInBinding: typMap,
		typeOfBinding:        typMap,
	}
}

func (g *Generator) String() string {
	return g.mod.String()
}

func (g *Generator) Call(bind *ast.Binding, exprList ...*ast.Arg) {
	g.mustGetImpl(bind, exprList...)
}

func (g *Generator) mustGetImpl(bind *ast.Binding, argList ...*ast.Arg) *ir.Func {
	bindName := bind.Name
	key := genKeyByArg(bindName, argList...)
	impl, getImpl := g.implsOfBinding[key]
	if getImpl {
		return impl
	}
	if len(argList) != len(bind.ParamList) {
		panic(`do not have enough arguments to call function`)
	}
	binds := make(map[string]ast.Expr)
	typeList := make([]types.Type, 0)
	for _, e := range argList {
		typeList = append(typeList, types.TypeOf(e))
	}
	params := make([]*ir.Param, 0)
	for i, e := range argList {
		argNameMustBe := bind.ParamList[i]
		argName := e.Ident
		// allow ignore argument name like: `add(1, 2)`
		if argName == "" {
			argName = argNameMustBe
		}
		if argNameMustBe != argName {
			panic(`argument name must be parameter name(or empty), for example:
  assert that should_be = ...
  assert(that: 1+2, should_be: 3)
`)
		}
		binds[argName] = e.Expr
		params = append(params, ir.NewParam(e.Ident, typeList[i].LLVMType()))
	}
	typeMap := make(map[string]types.Type)
	for i, t := range typeList {
		argName := bind.ParamList[i]
		typeMap[argName] = t
	}
	inferT := g.inferReturnType(bind.Expr, binds, typeMap)
	g.typeOfBinding[key] = inferT
	f := g.mod.NewFunc(bindName, inferT.LLVMType(), params...)

	b := f.NewBlock("")
	g.funcBody(b, bind.Expr, binds, typeMap)

	g.implsOfBinding[key] = f
	return f
}

// inference the return type by the expression we going to execute and input types
func (g *Generator) inferReturnType(expr ast.Expr, binds map[string]ast.Expr, typeMap map[string]types.Type) types.Type {
	switch expr := expr.(type) {
	case *ast.FuncCall:
		bind, hasBind := g.bindMap[expr.FuncName]
		if hasBind {
			argList := make([]*ast.Arg, 0)
			for _, arg := range expr.ExprList {
				argument := arg
				if e, isIdent := arg.Expr.(*ast.Ident); isIdent {
					argument.Expr = binds[e.Literal]
				}
				argList = append(argList, argument)
			}
			g.mustGetImpl(bind, argList...)
			t, exist := g.typeOfBind(genKeyByArg(expr.FuncName, expr.ExprList...))
			if exist {
				return t
			}
		}
		panic(fmt.Sprintf("can't find any binding call: %s", expr.FuncName))
	case *ast.BinaryExpr:
		lt := g.inferReturnType(expr.LExpr, binds, typeMap)
		rt := g.inferReturnType(expr.RExpr, binds, typeMap)
		op := expr.Op
		key := genKeyByTypes(op, lt, rt)
		t, ok := g.typeOfBind(key)
		if !ok {
			panic(fmt.Sprintf("can't infer return type by %s", key))
		}
		return t
	case *ast.Ident:
		t, ok := typeMap[expr.Literal]
		if !ok {
			panic(fmt.Sprintf("can't get type of identifier: %s", expr.Literal))
		}
		return t
	default:
		panic(fmt.Sprintf("unsupported type inference for expression: %#v yet", expr))
	}
}

func (g *Generator) isBuiltIn(key string) bool {
	_, isBuiltIn := g.typeOfBuiltInBinding[key]
	return isBuiltIn
}

func (g *Generator) typeOfBind(key string) (types.Type, bool) {
	t, existed := g.typeOfBuiltInBinding[key]
	if existed {
		return t, true
	}
	t, existed = g.typeOfBinding[key]
	if existed {
		return t, true
	}
	return nil, false
}

func (g *Generator) funcBody(b *ir.Block, expr ast.Expr, binds map[string]ast.Expr, typeMap map[string]types.Type) {
	v := g.genExpr(b, expr, binds, typeMap)
	b.NewRet(v)
}

func (g *Generator) genExpr(b *ir.Block, expr ast.Expr, binds map[string]ast.Expr, typeMap map[string]types.Type) value.Value {
	switch expr := expr.(type) {
	case *ast.FuncCall:
		bind := g.bindMap[expr.FuncName]
		f := g.mustGetImpl(bind, expr.ExprList...)
		valueList := make([]value.Value, 0)
		for _, arg := range expr.ExprList {
			e := g.genExpr(b, arg.Expr, binds, typeMap)
			valueList = append(valueList, e)
		}
		return b.NewCall(f, valueList...)
	case *ast.BinaryExpr:
		x := g.genExpr(b, expr.LExpr, binds, typeMap)
		y := g.genExpr(b, expr.RExpr, binds, typeMap)
		lt := getType(expr.LExpr, typeMap)
		rt := getType(expr.RExpr, typeMap)
		key := genKeyByTypes(expr.Op, lt, rt)
		if g.isBuiltIn(key) {
			if lt.String() == "int" && rt.String() == "int" {
				switch expr.Op {
				case "+":
					return b.NewAdd(x, y)
				case "-":
					return b.NewSub(x, y)
				case "*":
					return b.NewMul(x, y)
				case "/":
					return b.NewSDiv(x, y)
				}
			}
		}
		panic(fmt.Sprintf("unsupported operator: %s", expr.Op))
	case *ast.Ident:
		e := binds[expr.Literal]
		return g.genExpr(b, e, binds, typeMap)
	case *ast.Int:
		v, err := constant.NewIntFromString(llvmtypes.I64, expr.Literal)
		if err != nil {
		}
		return v
	default:
		panic(fmt.Sprintf("failed at generate expression: %#v", expr))
	}
}

func getType(e ast.Expr, typeMap map[string]types.Type) types.Type {
	if e, isIdentifier := e.(*ast.Ident); isIdentifier {
		return typeMap[e.Literal]
	}
	return types.TypeOf(e)
}

func genKeyByTypes(bindName string, typeList ...types.Type) string {
	var b strings.Builder
	b.WriteString(bindName)
	if len(typeList) > 0 {
		b.WriteRune('(')
		for _, t := range typeList[:len(typeList)-1] {
			b.WriteString(t.String())
			b.WriteRune(',')
		}
		b.WriteString(typeList[len(typeList)-1].String())
		b.WriteRune(')')
	}
	return b.String()
}

func genKey(bindName string, exprList ...ast.Expr) string {
	typeList := make([]types.Type, 0)
	for _, e := range exprList {
		typeList = append(typeList, types.TypeOf(e))
	}
	return genKeyByTypes(bindName, typeList...)
}

func genKeyByArg(bindName string, argList ...*ast.Arg) string {
	exprList := make([]ast.Expr, len(argList))
	for i, arg := range argList {
		exprList[i] = arg
	}
	return genKey(bindName, exprList...)
}
