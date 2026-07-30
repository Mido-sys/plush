package plush

import "github.com/gobuffalo/plush/v5/ast"

func programHasContextWrites(program *ast.Program) bool {
	if program == nil {
		return false
	}
	return statementsHaveContextWrites(program.Statements)
}

func statementsHaveContextWrites(statements []ast.Statement) bool {
	for _, stmt := range statements {
		if statementHasContextWrites(stmt) {
			return true
		}
	}
	return false
}

func statementHasContextWrites(stmt ast.Statement) bool {
	switch stmt := stmt.(type) {
	case nil:
		return false
	case *ast.LetStatement:
		return true
	case *ast.ExpressionStatement:
		return expressionHasContextWrites(stmt.Expression)
	case *ast.ReturnStatement:
		return expressionHasContextWrites(stmt.ReturnValue)
	case *ast.BlockStatement:
		return statementsHaveContextWrites(stmt.Statements)
	case *ast.HoleStatement:
		return false
	default:
		return false
	}
}

func expressionHasContextWrites(expr ast.Expression) bool {
	switch expr := expr.(type) {
	case nil:
		return false
	case *ast.AssignExpression:
		return true
	case *ast.IfExpression:
		if expressionHasContextWrites(expr.Condition) {
			return true
		}
		if expr.Block != nil && statementsHaveContextWrites(expr.Block.Statements) {
			return true
		}
		for _, elseif := range expr.ElseIf {
			if elseif == nil {
				continue
			}
			if expressionHasContextWrites(elseif.Condition) {
				return true
			}
			if elseif.Block != nil && statementsHaveContextWrites(elseif.Block.Statements) {
				return true
			}
		}
		return expr.ElseBlock != nil && statementsHaveContextWrites(expr.ElseBlock.Statements)
	case *ast.ForExpression:
		return expressionHasContextWrites(expr.Iterable) || expr.Block != nil && statementsHaveContextWrites(expr.Block.Statements)
	case *ast.FunctionLiteral:
		return expr.Block != nil && statementsHaveContextWrites(expr.Block.Statements)
	case *ast.CallExpression:
		if expressionHasContextWrites(expr.Callee) ||
			expressionHasContextWrites(expr.ChainCallee) ||
			expressionHasContextWrites(expr.Function) {
			return true
		}
		for _, arg := range expr.Arguments {
			if expressionHasContextWrites(arg) {
				return true
			}
		}
		if expr.Block != nil && statementsHaveContextWrites(expr.Block.Statements) {
			return true
		}
		return expr.ElseBlock != nil && statementsHaveContextWrites(expr.ElseBlock.Statements)
	case *ast.InfixExpression:
		return expressionHasContextWrites(expr.Left) || expressionHasContextWrites(expr.Right)
	case *ast.PrefixExpression:
		return expressionHasContextWrites(expr.Right)
	case *ast.ArrayLiteral:
		for _, element := range expr.Elements {
			if expressionHasContextWrites(element) {
				return true
			}
		}
	case *ast.HashLiteral:
		for _, key := range expr.Order {
			if expressionHasContextWrites(key) || expressionHasContextWrites(expr.Pairs[key]) {
				return true
			}
		}
	case *ast.IndexExpression:
		return expr.Value != nil ||
			expressionHasContextWrites(expr.Left) ||
			expressionHasContextWrites(expr.Index) ||
			expressionHasContextWrites(expr.Callee)
	}
	return false
}
