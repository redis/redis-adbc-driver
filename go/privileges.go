// Copyright (c) 2026 ADBC Drivers Contributors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may obtain a copy at http://www.apache.org/licenses/LICENSE-2.0
package redis

import "github.com/apache/arrow-adbc/go/adbc"

// PrivilegeStmt maps one table SELECT grant to a provisioned Redis role.
type PrivilegeStmt struct {
	Revoke bool
	Table  TableName
	Role   string
}

func (*PrivilegeStmt) stmtNode() {}

func (p *parser) parsePrivilege() (Stmt, error) {
	st := &PrivilegeStmt{Revoke: p.acceptKeyword("REVOKE")}
	if !st.Revoke {
		p.next()
	}
	unsupported := func() (Stmt, error) {
		return nil, errorf(adbc.StatusNotImplemented, "only GRANT SELECT ON [TABLE] table TO role and REVOKE SELECT ON [TABLE] table FROM role are supported")
	}
	if !p.acceptKeyword("SELECT") {
		return unsupported()
	}
	if p.isOp("(") || p.isOp(",") {
		return unsupported()
	}
	if err := p.expectKeyword("ON"); err != nil {
		return nil, err
	}
	p.acceptKeyword("TABLE")
	if p.isKeyword("ALL") || p.isKeyword("SCHEMA") || p.isKeyword("SEQUENCE") || p.isKeyword("DATABASE") {
		return unsupported()
	}
	var err error
	if st.Table, err = p.parseTableName(); err != nil {
		return nil, err
	}
	if p.isOp(",") {
		return unsupported()
	}
	keyword := "TO"
	if st.Revoke {
		keyword = "FROM"
	}
	if err := p.expectKeyword(keyword); err != nil {
		return nil, err
	}
	if p.isKeyword("PUBLIC") || p.isKeyword("CURRENT_USER") || p.isKeyword("CURRENT_ROLE") || p.isKeyword("SESSION_USER") || p.isKeyword("GROUP") {
		return unsupported()
	}
	if st.Role, err = p.parseIdent(); err != nil {
		return nil, err
	}
	if !p.isOp(";") && p.peek().kind != tokEOF {
		return unsupported()
	}
	return st, nil
}
